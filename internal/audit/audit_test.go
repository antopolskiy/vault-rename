package audit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antopolskiy/vault-rename/internal/model"
)

func TestStoreLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "renames.sqlite3")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.Path() != path {
		t.Fatalf("path = %q", store.Path())
	}
	plan := model.Plan{
		Root: "/vault", Source: "Old.md", Destination: "New title.md",
		Backlinks: model.BacklinksRepair, UnsupportedMode: model.UnsupportedError,
		FrontmatterTitle: model.FrontmatterTitleExact, LinksUpdated: 1,
		FileChanges: []model.FileChange{{
			Path: "Map.md", Role: "backlink", BeforeHash: "a", AfterHash: "b",
			Mode: 0o644, Patches: []model.Patch{{
				Kind: "wikilink", OldTarget: "Old", NewTarget: "New title", ReferenceEdit: true,
			}},
		}},
	}
	ctx := context.Background()
	if err := store.Begin(ctx, model.AuditContext{
		OperationID: "operation-1", Actor: "test", Reason: "test",
		ToolVersion: "test", ConfigVersion: 1, StartedAt: time.Now(),
	}, "vault-id", plan); err != nil {
		t.Fatal(err)
	}
	status, err := store.Status(ctx, "operation-1")
	if err != nil || status != "applying" {
		t.Fatalf("status = %q, err = %v", status, err)
	}
	if err := store.SetStatus(ctx, "operation-1", "committed", ""); err != nil {
		t.Fatal(err)
	}
	status, err = store.Status(ctx, "operation-1")
	if err != nil || status != "committed" {
		t.Fatalf("status = %q, err = %v", status, err)
	}
	if status, err = store.Status(ctx, "missing"); err != nil || status != "" {
		t.Fatalf("missing status = %q, err = %v", status, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("database permissions = %o", info.Mode().Perm())
	}
}
