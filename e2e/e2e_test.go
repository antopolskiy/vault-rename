package e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

var (
	binPath     string
	testBinPath string
	projectRoot string
	coverDir    string
)

type result struct {
	stdout   string
	stderr   string
	exitCode int
}

type output struct {
	OperationID  string `json:"operation_id"`
	Status       string `json:"status"`
	Source       string `json:"source"`
	Destination  string `json:"destination"`
	FilesChanged int    `json:"files_changed"`
	LinksUpdated int    `json:"links_updated"`
	LogPath      string `json:"log_path"`
}

func TestMain(m *testing.M) {
	var err error
	projectRoot, err = filepath.Abs("..")
	if err != nil {
		panic(err)
	}
	buildDir, err := os.MkdirTemp("", "vault-rename-e2e-*")
	if err != nil {
		panic(err)
	}
	name := "vault-rename"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binPath = filepath.Join(buildDir, name)
	testBinPath = filepath.Join(buildDir, "test-"+name)
	coverDir = os.Getenv("VAULT_RENAME_E2E_COVERDIR")
	build(binPath)
	build(testBinPath, "-tags", "testhooks")
	code := m.Run()
	_ = os.RemoveAll(buildDir)
	os.Exit(code)
}

func TestComplexRepresentativeFixture(t *testing.T) {
	vault := fixtureVault(t, "complex")
	home := t.TempDir()
	run := runBinary(t, binPath, home, nil,
		"--root", vault, "--json", "--reason", "fixture test", "--actor", "test-agent",
		"inbox/Old note.md", "Descriptive note title.md")
	if run.exitCode != 0 {
		t.Fatalf("rename failed: %s\n%s", run.stdout, run.stderr)
	}
	var response output
	if err := json.Unmarshal([]byte(run.stdout), &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != "committed" || response.LinksUpdated != 7 || response.FilesChanged != 2 {
		t.Fatalf("response = %#v", response)
	}
	assertTreeEquals(t, vault, filepath.Join(projectRoot, "testdata", "representative-vault", "complex", "after"))

	db, err := sql.Open("sqlite", response.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status, actor string
	var linkCount int
	if err := db.QueryRowContext(context.Background(), `SELECT status, actor, links_updated FROM operations WHERE operation_id = ?`, response.OperationID).
		Scan(&status, &actor, &linkCount); err != nil {
		t.Fatal(err)
	}
	if status != "committed" || actor != "test-agent" || linkCount != 7 {
		t.Fatalf("audit = %s %s %d", status, actor, linkCount)
	}
}

func TestAttachmentFixture(t *testing.T) {
	vault := fixtureVault(t, "attachment")
	run := runBinary(t, binPath, t.TempDir(), nil,
		"--root", vault, "--reason", "fixture test",
		"inbox/IMG_1234.pdf", "20260704-reference-document.pdf")
	if run.exitCode != 0 {
		t.Fatalf("rename failed: %s", run.stderr)
	}
	assertTreeEquals(t, vault, filepath.Join(projectRoot, "testdata", "representative-vault", "attachment", "after"))
}

func TestUnicodeExcalidrawRepresentativeFixture(t *testing.T) {
	vault := fixtureVault(t, "unicode-excalidraw")
	run := runBinary(t, binPath, t.TempDir(), nil,
		"--root", vault, "--reason", "Unicode fixture",
		"inbox/ძველი ჩანაწერი.md", "ქართული კვლევის ჩანაწერი.md")
	if run.exitCode != 0 {
		t.Fatalf("rename failed: %s", run.stderr)
	}
	assertTreeEquals(t, vault, filepath.Join(projectRoot, "testdata", "representative-vault", "unicode-excalidraw", "after"))
}

func TestDryRunIsCompletelyNonMutating(t *testing.T) {
	vault := fixtureVault(t, "complex")
	before := treeSnapshot(t, vault)
	home := t.TempDir()
	run := runBinary(t, binPath, home, nil,
		"--root", vault, "--dry-run", "--json",
		"inbox/Old note.md", "Descriptive note title.md")
	if run.exitCode != 0 {
		t.Fatalf("dry run failed: %s", run.stderr)
	}
	after := treeSnapshot(t, vault)
	if !equalSnapshots(before, after) {
		t.Fatal("dry run changed vault files")
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("dry run created state in HOME: %#v", entries)
	}
}

func TestInjectedFailureRollsBackExactly(t *testing.T) {
	failpoints := []string{
		"after-backup",
		"after-recovery-manifest",
		"after-audit-operation",
		"after-temporary-file",
		"after-file-replacement",
		"after-final-rename",
		"after-post-validation",
		"before-database-commit",
	}
	for _, failpoint := range failpoints {
		t.Run(failpoint, func(t *testing.T) {
			vault := fixtureVault(t, "complex")
			before := treeSnapshot(t, vault)
			run := runBinary(t, testBinPath, t.TempDir(), []string{
				"VAULT_RENAME_TESTING=1",
				"VAULT_RENAME_TEST_FAILPOINT=" + failpoint,
			}, "--root", vault, "--json", "--reason", "failure test",
				"inbox/Old note.md", "Descriptive note title.md")
			if run.exitCode == 0 {
				t.Fatal("expected injected failure")
			}
			after := treeSnapshot(t, vault)
			if !equalSnapshots(before, after) {
				t.Fatalf("rollback did not restore exact tree\nstdout: %s\nstderr: %s", run.stdout, run.stderr)
			}
		})
	}
}

func TestCrashIsRecoveredBeforeNextRename(t *testing.T) {
	vault := fixtureVault(t, "complex")
	home := t.TempDir()
	crashed := runBinary(t, testBinPath, home, []string{
		"VAULT_RENAME_TESTING=1",
		"VAULT_RENAME_TEST_CRASHPOINT=after-file-replacement",
	}, "--root", vault, "--json", "--reason", "crash test",
		"inbox/Old note.md", "Descriptive note title.md")
	if crashed.exitCode != 97 {
		t.Fatalf("crash exit = %d, want 97; stderr: %s", crashed.exitCode, crashed.stderr)
	}

	recovered := runBinary(t, binPath, home, nil,
		"--root", vault, "--json", "--reason", "retry after recovery",
		"inbox/Old note.md", "Descriptive note title.md")
	if recovered.exitCode != 0 {
		t.Fatalf("recovery retry failed: %s\n%s", recovered.stdout, recovered.stderr)
	}
	assertTreeEquals(t, vault, filepath.Join(projectRoot, "testdata", "representative-vault", "complex", "after"))
}

func TestPreparedAndCommittedCrashWindows(t *testing.T) {
	t.Run("prepared journal without audit is discarded", func(t *testing.T) {
		vault := fixtureVault(t, "complex")
		home := t.TempDir()
		crashed := runBinary(t, testBinPath, home, []string{
			"VAULT_RENAME_TESTING=1",
			"VAULT_RENAME_TEST_CRASHPOINT=after-recovery-manifest",
		}, "--root", vault, "--reason", "prepared crash",
			"inbox/Old note.md", "Descriptive note title.md")
		if crashed.exitCode != 97 {
			t.Fatalf("crash exit = %d, want 97", crashed.exitCode)
		}
		retry := runBinary(t, binPath, home, nil,
			"--root", vault, "--reason", "retry",
			"inbox/Old note.md", "Descriptive note title.md")
		if retry.exitCode != 0 {
			t.Fatalf("retry failed: %#v", retry)
		}
	})

	t.Run("committed journal is cleaned without rollback", func(t *testing.T) {
		vault := fixtureVault(t, "complex")
		second := filepath.Join(vault, "inbox", "Second note.md")
		if err := os.WriteFile(second, []byte("# Second\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		home := t.TempDir()
		crashed := runBinary(t, testBinPath, home, []string{
			"VAULT_RENAME_TESTING=1",
			"VAULT_RENAME_TEST_CRASHPOINT=after-database-commit",
		}, "--root", vault, "--reason", "committed crash",
			"inbox/Old note.md", "Descriptive note title.md")
		if crashed.exitCode != 97 {
			t.Fatalf("crash exit = %d, want 97", crashed.exitCode)
		}
		next := runBinary(t, binPath, home, nil,
			"--root", vault, "--reason", "next operation",
			"inbox/Second note.md", "Second descriptive note.md")
		if next.exitCode != 0 {
			t.Fatalf("next operation failed: %#v", next)
		}
		for _, path := range []string{
			"inbox/Descriptive note title.md",
			"inbox/Second descriptive note.md",
		} {
			if _, err := os.Stat(filepath.Join(vault, filepath.FromSlash(path))); err != nil {
				t.Fatalf("%s missing: %v", path, err)
			}
		}
	})
}

func TestRecoveryConflictPreservesExternalEdit(t *testing.T) {
	vault := fixtureVault(t, "complex")
	home := t.TempDir()
	crashed := runBinary(t, testBinPath, home, []string{
		"VAULT_RENAME_TESTING=1",
		"VAULT_RENAME_TEST_CRASHPOINT=after-file-replacement",
	}, "--root", vault, "--json", "--reason", "crash test",
		"inbox/Old note.md", "Descriptive note title.md")
	if crashed.exitCode != 97 {
		t.Fatalf("crash exit = %d, want 97", crashed.exitCode)
	}
	referrer := filepath.Join(vault, "3_Resources", "Reference map.md")
	if err := os.WriteFile(referrer, []byte("external edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	recovery := runBinary(t, binPath, home, nil,
		"--root", vault, "--json", "--reason", "retry",
		"inbox/Old note.md", "Descriptive note title.md")
	if recovery.exitCode == 0 {
		t.Fatal("expected recovery conflict")
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(recovery.stdout), &response); err != nil {
		t.Fatal(err)
	}
	if response["code"] != "RECOVERY_CONFLICT" {
		t.Fatalf("response = %#v", response)
	}
	data, err := os.ReadFile(referrer) //nolint:gosec // path belongs to the temporary fixture vault.
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "external edit\n" {
		t.Fatal("recovery overwrote the external edit")
	}
}

func TestBacklinkModesAndUnsupportedWarnings(t *testing.T) {
	t.Run("check", func(t *testing.T) {
		vault := fixtureVault(t, "complex")
		run := runBinary(t, binPath, t.TempDir(), nil,
			"--root", vault, "--json", "--backlinks", "check", "--reason", "check",
			"inbox/Old note.md", "Descriptive note title.md")
		if run.exitCode == 0 || !bytes.Contains([]byte(run.stdout), []byte("REFERENCES_PRESENT")) {
			t.Fatalf("unexpected check result: %#v", run)
		}
	})

	t.Run("off", func(t *testing.T) {
		vault := fixtureVault(t, "complex")
		run := runBinary(t, binPath, t.TempDir(), nil,
			"--root", vault, "--json", "--backlinks", "off", "--reason", "off",
			"inbox/Old note.md", "Descriptive note title.md")
		if run.exitCode != 0 {
			t.Fatalf("off failed: %#v", run)
		}
		data, err := os.ReadFile(filepath.Join(vault, "3_Resources", "Reference map.md")) //nolint:gosec // temporary fixture path.
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("[[inbox/Old note")) {
			t.Fatal("off mode unexpectedly repaired backlinks")
		}
	})

	t.Run("unsupported warn", func(t *testing.T) {
		vault := fixtureVault(t, "attachment")
		if err := os.WriteFile(filepath.Join(vault, "view.canvas"), []byte(`{"file":"inbox/IMG_1234.pdf"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vault, ".vault-rename.toml"), []byte(
			"version = 1\nunsupported_references = \"warn\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		run := runBinary(t, binPath, t.TempDir(), nil,
			"--root", vault, "--json", "--reason", "warning",
			"inbox/IMG_1234.pdf", "20260704-reference-document.pdf")
		if run.exitCode != 0 || !bytes.Contains([]byte(run.stdout), []byte("UNSUPPORTED_REFERENCE")) {
			t.Fatalf("unexpected warning result: %#v", run)
		}
	})
}

func TestCaseOnlyRenameAndUnsafeSources(t *testing.T) {
	t.Run("case only", func(t *testing.T) {
		vault := t.TempDir()
		path := filepath.Join(vault, "inbox", "CASE title.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# Case title\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		run := runBinary(t, binPath, t.TempDir(), nil,
			"--root", vault, "--reason", "case normalization",
			"inbox/CASE title.md", "Case title.md")
		if run.exitCode != 0 {
			t.Fatalf("case-only rename failed: %#v", run)
		}
		if _, err := os.Stat(filepath.Join(vault, "inbox", "Case title.md")); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("case-only rollback", func(t *testing.T) {
		vault := t.TempDir()
		path := filepath.Join(vault, "inbox", "CASE title.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# Case title\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		run := runBinary(t, testBinPath, t.TempDir(), []string{
			"VAULT_RENAME_TESTING=1",
			"VAULT_RENAME_TEST_FAILPOINT=after-case-temporary-rename",
		}, "--root", vault, "--reason", "case rollback",
			"inbox/CASE title.md", "Case title.md")
		if run.exitCode == 0 {
			t.Fatal("expected injected failure")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("original case was not restored: %v", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires privileges on Windows")
		}
		vault := t.TempDir()
		if err := os.MkdirAll(filepath.Join(vault, "inbox"), 0o750); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(vault, "real.md")
		if err := os.WriteFile(target, []byte("real"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(vault, "inbox", "Link.md")); err != nil {
			t.Fatal(err)
		}
		run := runBinary(t, binPath, t.TempDir(), nil,
			"--root", vault, "--json", "--reason", "unsafe source",
			"inbox/Link.md", "Safe link title.md")
		if run.exitCode == 0 {
			t.Fatal("symlink source was accepted")
		}
	})

	t.Run("hard link", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("hard-link semantics are platform-specific")
		}
		vault := t.TempDir()
		if err := os.MkdirAll(filepath.Join(vault, "inbox"), 0o750); err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(vault, "inbox", "Hard link.md")
		if err := os.WriteFile(source, []byte("hard"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(source, filepath.Join(vault, "duplicate.md")); err != nil {
			t.Fatal(err)
		}
		run := runBinary(t, binPath, t.TempDir(), nil,
			"--root", vault, "--json", "--reason", "unsafe source",
			"inbox/Hard link.md", "Safe hard link title.md")
		if run.exitCode == 0 {
			t.Fatal("hard-linked source was accepted")
		}
	})
}

func build(path string, extra ...string) {
	args := []string{"build"}
	args = append(args, extra...)
	if coverDir != "" {
		args = append(args, "-cover", "-coverpkg=github.com/antopolskiy/vault-rename/...")
	}
	args = append(args, "-o", path, "./cmd/vault-rename")
	command := exec.CommandContext(context.Background(), "go", args...)
	command.Dir = projectRoot
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		panic(err)
	}
}

func runBinary(t *testing.T, binary, home string, env []string, args ...string) result {
	t.Helper()
	command := exec.CommandContext(context.Background(), binary, args...) //nolint:gosec // binary is the test-built executable.
	baseEnv := []string{"HOME=" + home}
	if coverDir != "" {
		baseEnv = append(baseEnv, "GOCOVERDIR="+coverDir)
	}
	command.Env = append(os.Environ(), append(baseEnv, env...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	response := result{stdout: stdout.String(), stderr: stderr.String()}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			response.exitCode = exitErr.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return response
}

func fixtureVault(t *testing.T, name string) string {
	t.Helper()
	source := filepath.Join(projectRoot, "testdata", "representative-vault", name, "before")
	destination := t.TempDir()
	copyTree(t, source, destination)
	return destination
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		data, err := os.ReadFile(path) //nolint:gosec // path comes from the committed fixture tree.
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertTreeEquals(t *testing.T, actual, expected string) {
	t.Helper()
	got := treeSnapshot(t, actual)
	want := treeSnapshot(t, expected)
	if !equalSnapshots(got, want) {
		t.Fatalf("tree mismatch\nactual: %#v\nexpected: %#v", got, want)
	}
}

func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // path comes from walking the temporary or committed fixture tree.
		if err != nil {
			return err
		}
		result[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func equalSnapshots(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	keys := make([]string, 0, len(left))
	for key := range left {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if right[key] != left[key] {
			return false
		}
	}
	return true
}
