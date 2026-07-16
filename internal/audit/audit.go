package audit

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/antopolskiy/vault-rename/internal/apperr"
	"github.com/antopolskiy/vault-rename/internal/model"
)

type Store struct {
	db   *sql.DB
	path string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, apperr.Wrap(apperr.CodeIOError, "cannot create audit directory", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeIOError, "cannot open rename audit database", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, path: path}
	if err := store.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, apperr.Wrap(apperr.CodeIOError, "cannot secure rename audit database", err)
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Begin(ctx context.Context, audit model.AuditContext, vaultID string, plan model.Plan) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot begin audit transaction", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO operations (
			operation_id, started_at, updated_at, vault_id, vault_path,
			source_path, destination_path, actor, reason, batch_id,
			backlinks, unsupported_references, frontmatter_title,
			tool_version, config_version, files_changed, links_updated, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'applying')`,
		audit.OperationID, audit.StartedAt.UTC().Format(time.RFC3339Nano), audit.StartedAt.UTC().Format(time.RFC3339Nano),
		vaultID, plan.Root, plan.Source, plan.Destination, audit.Actor, audit.Reason, nullString(audit.BatchID),
		string(plan.Backlinks), string(plan.UnsupportedMode), string(plan.FrontmatterTitle),
		audit.ToolVersion, audit.ConfigVersion, affectedCount(plan), plan.LinksUpdated,
	)
	if err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot record applying operation", err)
	}
	for _, change := range plan.FileChanges {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO file_changes (
				operation_id, path, role, before_hash, after_hash, mode, patch_count
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			audit.OperationID, change.Path, change.Role, change.BeforeHash, change.AfterHash,
			uint32(change.Mode.Perm()), len(change.Patches),
		); err != nil {
			return apperr.Wrap(apperr.CodeIOError, "cannot record planned file change", err)
		}
		for _, item := range change.Patches {
			if !item.ReferenceEdit {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO link_changes (
					operation_id, referrer_path, kind, old_target, new_target, byte_offset
				) VALUES (?, ?, ?, ?, ?, ?)`,
				audit.OperationID, change.Path, item.Kind, item.OldTarget, item.NewTarget, item.Start,
			); err != nil {
				return apperr.Wrap(apperr.CodeIOError, "cannot record planned link change", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot commit applying audit operation", err)
	}
	return nil
}

func (s *Store) SetStatus(ctx context.Context, operationID, status, message string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		UPDATE operations
		SET status = ?, error_message = ?, updated_at = ?, completed_at =
			CASE WHEN ? IN ('committed', 'rolled_back', 'recovery_required') THEN ? ELSE completed_at END
		WHERE operation_id = ?`,
		status, nullString(message), now, status, now, operationID,
	)
	if err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot update audit status", err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return apperr.New(apperr.CodeIOError, "audit operation does not exist")
	}
	return nil
}

func (s *Store) Status(ctx context.Context, operationID string) (string, error) {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM operations WHERE operation_id = ?`, operationID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", apperr.Wrap(apperr.CodeIOError, "cannot read audit status", err)
	}
	return status, nil
}

func (s *Store) initialize(ctx context.Context) error {
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = FULL`,
		`PRAGMA busy_timeout = 5000`,
		`CREATE TABLE IF NOT EXISTS operations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			operation_id TEXT NOT NULL UNIQUE,
			started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			completed_at TEXT,
			vault_id TEXT NOT NULL,
			vault_path TEXT NOT NULL,
			source_path TEXT NOT NULL,
			destination_path TEXT NOT NULL,
			actor TEXT NOT NULL,
			reason TEXT NOT NULL,
			batch_id TEXT,
			backlinks TEXT NOT NULL,
			unsupported_references TEXT NOT NULL,
			frontmatter_title TEXT NOT NULL,
			tool_version TEXT NOT NULL,
			config_version INTEGER NOT NULL,
			files_changed INTEGER NOT NULL,
			links_updated INTEGER NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS file_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			operation_id TEXT NOT NULL REFERENCES operations(operation_id) ON DELETE CASCADE,
			path TEXT NOT NULL,
			role TEXT NOT NULL,
			before_hash TEXT NOT NULL,
			after_hash TEXT NOT NULL,
			mode INTEGER NOT NULL,
			patch_count INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS link_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			operation_id TEXT NOT NULL REFERENCES operations(operation_id) ON DELETE CASCADE,
			referrer_path TEXT NOT NULL,
			kind TEXT NOT NULL,
			old_target TEXT NOT NULL,
			new_target TEXT NOT NULL,
			byte_offset INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operations_started_at ON operations(started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_operations_status ON operations(status)`,
		`CREATE INDEX IF NOT EXISTS idx_file_changes_operation ON file_changes(operation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_link_changes_operation ON link_changes(operation_id)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return apperr.Wrap(apperr.CodeIOError, "cannot initialize audit database", err)
		}
	}
	return nil
}

func affectedCount(plan model.Plan) int {
	for _, change := range plan.FileChanges {
		if change.Path == plan.Source {
			return len(plan.FileChanges)
		}
	}
	return len(plan.FileChanges) + 1
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
