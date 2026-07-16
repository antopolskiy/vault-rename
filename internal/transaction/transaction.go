package transaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/antopolskiy/vault-rename/internal/apperr"
	"github.com/antopolskiy/vault-rename/internal/audit"
	"github.com/antopolskiy/vault-rename/internal/model"
	"github.com/antopolskiy/vault-rename/internal/patch"
	"github.com/antopolskiy/vault-rename/internal/state"
)

type Failpoint func(string) error

type journalFile struct {
	Path       string `json:"path"`
	Backup     string `json:"backup"`
	Mode       uint32 `json:"mode"`
	BeforeHash string `json:"before_hash"`
	AfterHash  string `json:"after_hash"`
}

type manifest struct {
	Version     int           `json:"version"`
	OperationID string        `json:"operation_id"`
	Root        string        `json:"root"`
	Source      string        `json:"source"`
	Destination string        `json:"destination"`
	CaseTemp    string        `json:"case_temp,omitempty"`
	Status      string        `json:"status"`
	Files       []journalFile `json:"files"`
}

func Pending(paths state.Paths) (bool, error) {
	entries, err := os.ReadDir(paths.Recovery)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, apperr.Wrap(apperr.CodeIOError, "cannot inspect recovery directory", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return true, nil
		}
	}
	return false, nil
}

func RecoverPending(ctx context.Context, paths state.Paths, store *audit.Store) error {
	entries, err := os.ReadDir(paths.Recovery)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot inspect recovery directory", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(paths.Recovery, entry.Name())
		item, loadErr := loadManifest(dir)
		if loadErr != nil {
			return loadErr
		}
		status, statusErr := store.Status(ctx, item.OperationID)
		if statusErr != nil {
			return statusErr
		}
		if status == "committed" {
			if err := os.RemoveAll(dir); err != nil {
				return apperr.Wrap(apperr.CodeIOError, "cannot remove committed recovery journal", err)
			}
			continue
		}
		if status == "" && item.Status == "prepared" {
			if err := os.RemoveAll(dir); err != nil {
				return apperr.Wrap(apperr.CodeIOError, "cannot remove unused recovery journal", err)
			}
			continue
		}
		if err := rollback(ctx, dir, &item, store, "automatic crash recovery"); err != nil {
			return err
		}
	}
	return nil
}

func Execute(
	ctx context.Context,
	paths state.Paths,
	store *audit.Store,
	vaultID string,
	plan model.Plan,
	auditContext model.AuditContext,
	fail Failpoint,
) (model.Result, error) {
	dir := filepath.Join(paths.Recovery, auditContext.OperationID)
	item, err := createJournal(dir, plan, auditContext.OperationID, fail)
	if err != nil {
		_ = os.RemoveAll(dir)
		return model.Result{}, err
	}
	removeJournal := true
	defer func() {
		if removeJournal {
			_ = os.RemoveAll(dir)
		}
	}()

	if err := store.Begin(ctx, auditContext, vaultID, plan); err != nil {
		return model.Result{}, err
	}
	if err := hit(fail, "after-audit-operation"); err != nil {
		if rollbackErr := rollback(ctx, dir, &item, store, err.Error()); rollbackErr != nil {
			removeJournal = false
			return model.Result{}, rollbackErr
		}
		return model.Result{}, apperr.Wrap(apperr.CodeIOError, "injected failure", err)
	}

	if err := applyChanges(dir, &item, plan, fail); err != nil {
		if rollbackErr := rollback(ctx, dir, &item, store, err.Error()); rollbackErr != nil {
			removeJournal = false
			return model.Result{}, rollbackErr
		}
		return model.Result{}, err
	}
	if err := postValidate(plan); err != nil {
		if rollbackErr := rollback(ctx, dir, &item, store, err.Error()); rollbackErr != nil {
			removeJournal = false
			return model.Result{}, rollbackErr
		}
		return model.Result{}, err
	}
	if err := hit(fail, "after-post-validation"); err != nil {
		if rollbackErr := rollback(ctx, dir, &item, store, err.Error()); rollbackErr != nil {
			removeJournal = false
			return model.Result{}, rollbackErr
		}
		return model.Result{}, apperr.Wrap(apperr.CodeIOError, "injected failure", err)
	}
	if err := hit(fail, "before-database-commit"); err != nil {
		if rollbackErr := rollback(ctx, dir, &item, store, err.Error()); rollbackErr != nil {
			removeJournal = false
			return model.Result{}, rollbackErr
		}
		return model.Result{}, apperr.Wrap(apperr.CodeIOError, "injected failure", err)
	}
	if err := store.SetStatus(ctx, auditContext.OperationID, "committed", ""); err != nil {
		if rollbackErr := rollback(ctx, dir, &item, store, err.Error()); rollbackErr != nil {
			removeJournal = false
			return model.Result{}, rollbackErr
		}
		return model.Result{}, err
	}
	postCommitErr := hit(fail, "after-database-commit")
	item.Status = "committed"
	warnings := append([]model.Warning(nil), plan.Warnings...)
	if postCommitErr != nil {
		warnings = append(warnings, model.Warning{
			Code:    "POST_COMMIT_WARNING",
			Message: postCommitErr.Error(),
		})
	}
	if err := writeManifest(dir, item); err != nil {
		removeJournal = false
		warnings = append(warnings, model.Warning{
			Code:    "RECOVERY_CLEANUP_PENDING",
			Message: "rename committed, but the recovery journal will be cleaned up on the next operation",
		})
	} else {
		removeJournal = false
		if err := os.RemoveAll(dir); err != nil {
			warnings = append(warnings, model.Warning{
				Code:    "RECOVERY_CLEANUP_PENDING",
				Message: "rename committed, but the recovery journal could not be removed",
			})
		}
	}

	return model.Result{
		OperationID:  auditContext.OperationID,
		Status:       "committed",
		Source:       plan.Source,
		Destination:  plan.Destination,
		FilesChanged: affectedCount(plan),
		LinksUpdated: plan.LinksUpdated,
		Warnings:     warnings,
		LogPath:      store.Path(),
	}, nil
}

func createJournal(dir string, plan model.Plan, operationID string, fail Failpoint) (manifest, error) {
	if err := os.MkdirAll(filepath.Join(dir, "backups"), 0o700); err != nil {
		return manifest{}, apperr.Wrap(apperr.CodeIOError, "cannot create recovery journal", err)
	}
	item := manifest{
		Version: 1, OperationID: operationID, Root: plan.Root,
		Source: plan.Source, Destination: plan.Destination, Status: "prepared",
	}
	if plan.CaseOnly {
		item.CaseTemp = plan.Source + ".vault-rename-" + operationID + ".tmp"
	}
	changes := make(map[string]model.FileChange)
	for _, change := range plan.FileChanges {
		changes[change.Path] = change
	}
	paths := make([]string, 0, len(changes)+1)
	for path := range changes {
		paths = append(paths, path)
	}
	if _, ok := changes[plan.Source]; !ok {
		paths = append(paths, plan.Source)
	}
	sort.Strings(paths)
	for index, rel := range paths {
		absolute := filepath.Join(plan.Root, filepath.FromSlash(rel))
		info, err := os.Lstat(absolute)
		if err != nil {
			return manifest{}, apperr.Wrap(apperr.CodeIOError, "cannot inspect file before backup", err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || hasMultipleLinks(info) {
			return manifest{}, apperr.New(apperr.CodeIOError, "touched files must be regular, non-symlink, non-hard-linked files")
		}
		data, err := os.ReadFile(absolute) //nolint:gosec // absolute is derived from a validated plan path under the vault root.
		if err != nil {
			return manifest{}, apperr.Wrap(apperr.CodeIOError, "cannot read file before backup", err)
		}
		beforeHash := patch.Hash(data)
		afterHash := beforeHash
		if change, ok := changes[rel]; ok {
			if beforeHash != change.BeforeHash {
				return manifest{}, apperr.New(apperr.CodeSourceChanged, "planned file changed before backup")
			}
			afterHash = change.AfterHash
		} else if beforeHash != plan.SourceHash {
			return manifest{}, apperr.New(apperr.CodeSourceChanged, "source changed before backup")
		}
		backup := filepath.ToSlash(filepath.Join("backups", fmt.Sprintf("%04d.bin", index)))
		if err := writeDurable(filepath.Join(dir, filepath.FromSlash(backup)), data, 0o600); err != nil {
			return manifest{}, err
		}
		item.Files = append(item.Files, journalFile{
			Path: rel, Backup: backup, Mode: uint32(info.Mode().Perm()),
			BeforeHash: beforeHash, AfterHash: afterHash,
		})
		if err := hit(fail, "after-backup"); err != nil {
			return manifest{}, apperr.Wrap(apperr.CodeIOError, "injected failure", err)
		}
	}
	if err := writeManifest(dir, item); err != nil {
		return manifest{}, err
	}
	if err := syncDirectory(dir); err != nil {
		return manifest{}, apperr.Wrap(apperr.CodeIOError, "cannot sync recovery journal", err)
	}
	if err := hit(fail, "after-recovery-manifest"); err != nil {
		return manifest{}, apperr.Wrap(apperr.CodeIOError, "injected failure", err)
	}
	return item, nil
}

func applyChanges(dir string, item *manifest, plan model.Plan, fail Failpoint) error {
	changes := append([]model.FileChange(nil), plan.FileChanges...)
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Role == changes[j].Role {
			return changes[i].Path < changes[j].Path
		}
		return changes[i].Role != "source"
	})
	for _, change := range changes {
		absolute := filepath.Join(plan.Root, filepath.FromSlash(change.Path))
		data, err := os.ReadFile(absolute) //nolint:gosec // absolute is derived from a validated plan path under the vault root.
		if err != nil {
			return apperr.Wrap(apperr.CodeIOError, "cannot read file before applying patch", err)
		}
		if patch.Hash(data) != change.BeforeHash {
			return apperr.New(apperr.CodeSourceChanged, "planned file changed before replacement")
		}
		rendered, err := patch.Apply(data, change.Patches)
		if err != nil || patch.Hash(rendered) != change.AfterHash {
			return apperr.Wrap(apperr.CodeSourceChanged, "planned patch no longer applies", err)
		}
		if err := atomicWrite(absolute, rendered, change.Mode.Perm(), item.OperationID, fail); err != nil {
			return err
		}
		item.Status = "applying"
		if err := writeManifest(dir, *item); err != nil {
			return err
		}
		if err := hit(fail, "after-file-replacement"); err != nil {
			return apperr.Wrap(apperr.CodeIOError, "injected failure", err)
		}
	}

	source := filepath.Join(plan.Root, filepath.FromSlash(plan.Source))
	destination := filepath.Join(plan.Root, filepath.FromSlash(plan.Destination))
	if plan.CaseOnly {
		temp := filepath.Join(plan.Root, filepath.FromSlash(item.CaseTemp))
		if err := os.Rename(source, temp); err != nil {
			return apperr.Wrap(apperr.CodeIOError, "cannot perform temporary case-only rename", err)
		}
		if err := hit(fail, "after-case-temporary-rename"); err != nil {
			return apperr.Wrap(apperr.CodeIOError, "injected failure", err)
		}
		if err := os.Rename(temp, destination); err != nil {
			return apperr.Wrap(apperr.CodeIOError, "cannot complete case-only rename", err)
		}
	} else if err := os.Rename(source, destination); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot rename source file", err)
	}
	item.Status = "renamed"
	if err := writeManifest(dir, *item); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot sync renamed file directory", err)
	}
	if err := hit(fail, "after-final-rename"); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "injected failure", err)
	}
	return nil
}

func rollback(ctx context.Context, dir string, item *manifest, store *audit.Store, reason string) error {
	source := filepath.Join(item.Root, filepath.FromSlash(item.Source))
	destination := filepath.Join(item.Root, filepath.FromSlash(item.Destination))
	temp := ""
	if item.CaseTemp != "" {
		temp = filepath.Join(item.Root, filepath.FromSlash(item.CaseTemp))
	}

	current := source
	if exactExists(destination) {
		current = destination
	} else if temp != "" {
		if exactExists(temp) {
			current = temp
		}
	}
	if current != source {
		expected := sourceAfterHash(*item)
		hash, err := hashFile(current)
		if err != nil {
			return recoveryFailure(ctx, item, store, "cannot inspect renamed source during rollback", err)
		}
		if hash != expected {
			return recoveryConflict(ctx, item, store, current)
		}
		if exactExists(source) {
			return recoveryConflict(ctx, item, store, source)
		}
		if err := os.Rename(current, source); err != nil {
			return recoveryFailure(ctx, item, store, "cannot restore source path", err)
		}
	}

	for index := len(item.Files) - 1; index >= 0; index-- {
		entry := item.Files[index]
		path := filepath.Join(item.Root, filepath.FromSlash(entry.Path))
		currentHash, err := hashFile(path)
		if err != nil {
			return recoveryFailure(ctx, item, store, "cannot inspect file during rollback", err)
		}
		if currentHash == entry.BeforeHash {
			continue
		}
		if currentHash != entry.AfterHash {
			return recoveryConflict(ctx, item, store, path)
		}
		backup, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(entry.Backup))) //nolint:gosec // backup path comes from the validated internal manifest.
		if err != nil || patch.Hash(backup) != entry.BeforeHash {
			return recoveryFailure(ctx, item, store, "recovery backup is missing or corrupted", err)
		}
		if err := atomicWrite(path, backup, os.FileMode(entry.Mode), item.OperationID, nil); err != nil {
			return recoveryFailure(ctx, item, store, "cannot restore file from recovery backup", err)
		}
	}
	if err := store.SetStatus(ctx, item.OperationID, "rolled_back", reason); err != nil {
		return err
	}
	item.Status = "rolled_back"
	if err := writeManifest(dir, *item); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot remove completed recovery journal", err)
	}
	return nil
}

func postValidate(plan model.Plan) error {
	source := filepath.Join(plan.Root, filepath.FromSlash(plan.Source))
	destination := filepath.Join(plan.Root, filepath.FromSlash(plan.Destination))
	if plan.CaseOnly {
		if exactExists(source) || !exactExists(destination) {
			return apperr.New(apperr.CodeIOError, "case-only rename did not produce the exact destination name")
		}
	} else if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		return apperr.New(apperr.CodeIOError, "source path still exists after rename")
	}
	destinationHash, err := hashFile(destination)
	if err != nil {
		return apperr.Wrap(apperr.CodeIOError, "renamed destination is missing", err)
	}
	expectedSourceHash := plan.SourceHash
	for _, change := range plan.FileChanges {
		path := filepath.Join(plan.Root, filepath.FromSlash(change.Path))
		if change.Path == plan.Source {
			expectedSourceHash = change.AfterHash
			path = destination
		}
		hash, hashErr := hashFile(path)
		if hashErr != nil || hash != change.AfterHash {
			return apperr.Wrap(apperr.CodeIOError, "post-rename file validation failed", hashErr)
		}
	}
	if destinationHash != expectedSourceHash {
		return apperr.New(apperr.CodeIOError, "renamed source hash does not match the plan")
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode, operationID string, fail Failpoint) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".vault-rename-"+operationID+"-*")
	if err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot create temporary replacement file", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		_ = temp.Close()
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(mode); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot preserve file permissions", err)
	}
	if err := copyExtendedMetadata(path, tempPath); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot preserve extended file metadata", err)
	}
	if _, err := temp.Write(data); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot write temporary replacement file", err)
	}
	if err := temp.Sync(); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot sync temporary replacement file", err)
	}
	if err := temp.Close(); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot close temporary replacement file", err)
	}
	if err := hit(fail, "after-temporary-file"); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "injected failure", err)
	}
	if err := replaceFile(tempPath, path); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot atomically replace file", err)
	}
	cleanup = false
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot sync replacement directory", err)
	}
	return nil
}

func writeManifest(dir string, item manifest) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot encode recovery manifest", err)
	}
	return writeDurable(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0o600)
}

func loadManifest(dir string) (manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json")) //nolint:gosec // dir is an internal recovery directory.
	if err != nil {
		return manifest{}, apperr.Wrap(apperr.CodeRollbackFailed, "cannot read recovery manifest", err)
	}
	var item manifest
	if err := json.Unmarshal(data, &item); err != nil || item.Version != 1 || item.OperationID == "" {
		return manifest{}, apperr.Wrap(apperr.CodeRollbackFailed, "invalid recovery manifest", err)
	}
	return item, nil
}

func writeDurable(path string, data []byte, mode os.FileMode) error {
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode) //nolint:gosec // temp is an internal state path.
	if err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot create durable state file", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return apperr.Wrap(apperr.CodeIOError, "cannot write durable state file", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return apperr.Wrap(apperr.CodeIOError, "cannot sync durable state file", err)
	}
	if err := file.Close(); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot close durable state file", err)
	}
	if err := replaceFile(temp, path); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot replace durable state file", err)
	}
	return nil
}

func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a validated transaction path.
	if err != nil {
		return "", err
	}
	return patch.Hash(data), nil
}

func exactExists(path string) bool {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return false
	}
	name := filepath.Base(path)
	for _, entry := range entries {
		if entry.Name() == name {
			return true
		}
	}
	return false
}

func sourceAfterHash(item manifest) string {
	for _, entry := range item.Files {
		if entry.Path == item.Source {
			return entry.AfterHash
		}
	}
	return ""
}

func recoveryConflict(ctx context.Context, item *manifest, store *audit.Store, path string) error {
	message := "rollback would overwrite an external edit"
	_ = store.SetStatus(ctx, item.OperationID, "recovery_required", message)
	item.Status = "recovery_required"
	return apperr.WithDetails(
		apperr.New(apperr.CodeRecoveryConflict, message),
		map[string]any{"path": path, "operation_id": item.OperationID},
	)
}

func recoveryFailure(ctx context.Context, item *manifest, store *audit.Store, message string, err error) error {
	_ = store.SetStatus(ctx, item.OperationID, "recovery_required", message)
	item.Status = "recovery_required"
	if err == nil {
		err = errors.New(message)
	}
	return apperr.Wrap(apperr.CodeRollbackFailed, message, err)
}

func affectedCount(plan model.Plan) int {
	for _, change := range plan.FileChanges {
		if change.Path == plan.Source {
			return len(plan.FileChanges)
		}
	}
	return len(plan.FileChanges) + 1
}

func hit(fail Failpoint, name string) error {
	if fail == nil {
		return nil
	}
	return fail(name)
}
