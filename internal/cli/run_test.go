package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDryRunDoesNotMutate(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "inbox", "Old note.md")
	if err := os.MkdirAll(filepath.Dir(source), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("# Old note\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(
		[]string{"--root", root, "--dry-run", "inbox/Old note.md", "Descriptive note title.md"},
		&stdout,
		&stderr,
	)

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Would rename: inbox/Old note.md -> inbox/Descriptive note title.md") {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source was mutated: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "inbox", "Descriptive note title.md")); !os.IsNotExist(err) {
		t.Fatal("dry run created destination")
	}
}

func TestRunJSONError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run(
		[]string{"--root", t.TempDir(), "--json", "--dry-run", "missing.md", "Good title.md"},
		&stdout,
		&stderr,
	)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if output["code"] != "SOURCE_NOT_FOUND" {
		t.Fatalf("code = %v", output["code"])
	}
}
