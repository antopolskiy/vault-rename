package planner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antopolskiy/vault-rename/internal/apperr"
	"github.com/antopolskiy/vault-rename/internal/config"
	"github.com/antopolskiy/vault-rename/internal/model"
	"github.com/antopolskiy/vault-rename/internal/patch"
)

func TestBuildRepairsProvenReferencesOnly(t *testing.T) {
	root := t.TempDir()
	write(t, root, "inbox/Old note.md", "---\ntitle: Old note\n---\nSelf: [[Old note]]\n")
	write(t, root, "3_Resources/Map.md", "Alias [[inbox/Old note|shown]].\nProse Old note stays.\n")

	plan, err := Build(root, "inbox/Old note.md", "Descriptive note title.md", config.Defaults(), model.BacklinksRepair)
	if err != nil {
		t.Fatal(err)
	}
	if plan.LinksUpdated != 2 {
		t.Fatalf("links = %d", plan.LinksUpdated)
	}
	for _, change := range plan.FileChanges {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(change.Path))) //nolint:gosec // path comes from the planner under the temporary root.
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := patch.Apply(data, change.Patches)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(rendered), "Prose Descriptive note title") {
			t.Fatal("ordinary prose was rewritten")
		}
	}
}

func TestBuildFailsOnAmbiguousBareReference(t *testing.T) {
	root := t.TempDir()
	write(t, root, "inbox/Old note.md", "target")
	write(t, root, "other/Old note.md", "duplicate")
	write(t, root, "Map.md", "[[Old note]]")
	_, err := Build(root, "inbox/Old note.md", "Descriptive note title.md", config.Defaults(), model.BacklinksRepair)
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeAmbiguousReference {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildBacklinksCheckAndUnsupported(t *testing.T) {
	root := t.TempDir()
	write(t, root, "inbox/Old note.md", "target")
	write(t, root, "Map.md", "[[inbox/Old note]]")
	_, err := Build(root, "inbox/Old note.md", "Descriptive note title.md", config.Defaults(), model.BacklinksCheck)
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeReferencesPresent {
		t.Fatalf("check error = %v", err)
	}

	if err := os.Remove(filepath.Join(root, "Map.md")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "view.canvas", `{"file":"inbox/Old note.md"}`)
	_, err = Build(root, "inbox/Old note.md", "Descriptive note title.md", config.Defaults(), model.BacklinksRepair)
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeUnsupportedReference {
		t.Fatalf("unsupported error = %v", err)
	}
}

func TestBuildEncodedUnicodeAndModes(t *testing.T) {
	root := t.TempDir()
	write(t, root, "inbox/Old note.md", "---\ntitle: Old note\n---\n")
	write(t, root, "Map.md", "[encoded](inbox/Old%20note.md)\n")
	plan, err := Build(root, "inbox/Old note.md", "ქართული კვლევის ჩანაწერი.md", config.Defaults(), model.BacklinksRepair)
	if err != nil {
		t.Fatal(err)
	}
	if plan.LinksUpdated != 1 {
		t.Fatalf("links = %d", plan.LinksUpdated)
	}
	var foundEncoded bool
	for _, change := range plan.FileChanges {
		for _, item := range change.Patches {
			if item.ReferenceEdit && strings.Contains(string(item.After), "%") {
				foundEncoded = true
			}
		}
	}
	if !foundEncoded {
		t.Fatal("Unicode Markdown destination was not URL encoded")
	}

	offPlan, err := Build(root, "inbox/Old note.md", "ქართული კვლევის ჩანაწერი.md", config.Defaults(), model.BacklinksOff)
	if err != nil {
		t.Fatal(err)
	}
	if offPlan.LinksUpdated != 0 || len(offPlan.Warnings) == 0 {
		t.Fatalf("off plan = %#v", offPlan)
	}
}

func TestBuildRejectsUnresolvedSourceLikeReferenceAndCollision(t *testing.T) {
	root := t.TempDir()
	write(t, root, "inbox/Old note.md", "target")
	write(t, root, "Map.md", "[[missing/Old note]]")
	_, err := Build(root, "inbox/Old note.md", "Descriptive note title.md", config.Defaults(), model.BacklinksRepair)
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeAmbiguousReference {
		t.Fatalf("unresolved error = %v", err)
	}

	if err := os.Remove(filepath.Join(root, "Map.md")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "inbox/Descriptive note title.md", "existing")
	_, err = Build(root, "inbox/Old note.md", "Descriptive note title.md", config.Defaults(), model.BacklinksRepair)
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeTargetExists {
		t.Fatalf("collision error = %v", err)
	}
}

func write(t *testing.T, root, rel, value string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
