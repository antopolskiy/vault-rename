package fixtures

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var privatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:/Users/|/home/)[^/\s]+/`),
	regexp.MustCompile(`(?i)[A-Z]:\\Users\\[^\\\r\n]+\\`),
	regexp.MustCompile(`(?i)[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}`),
	regexp.MustCompile(`(?:\+\d{1,3}[\s-]?)?(?:\(\d{2,4}\)|\d{2,4})[\s-]\d{3,4}[\s-]\d{3,4}`),
	regexp.MustCompile(`(?i)(?:gh[pousr]_[a-z0-9]{20,}|github_pat_[a-z0-9_]{20,})`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

var externalURL = regexp.MustCompile(`(?i)https?://[^\s)>"]+`)

func TestRepositoryContainsNoPrivateLookingData(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		switch filepath.ToSlash(rel) {
		case "coverage.out", "coverage.html", "internal/fixtures/audit_test.go":
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // path is produced by walking the repository root.
		if err != nil {
			return err
		}
		assertNoPrivatePatterns(t, path, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRepresentativeFixturesContainNoPrivateData(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "representative-vault")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // path is produced by walking the committed fixture root.
		if err != nil {
			return err
		}
		assertNoPrivatePatterns(t, path, data)
		for _, value := range externalURL.FindAllString(string(data), -1) {
			if !strings.HasPrefix(value, "https://example.invalid") && !strings.HasPrefix(value, "http://example.invalid") {
				t.Errorf("%s contains non-example URL %s", path, value)
			}
		}
		if strings.Contains(string(data), "example.com") {
			t.Errorf("%s uses a non-reserved example domain", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertNoPrivatePatterns(t *testing.T, path string, data []byte) {
	t.Helper()
	for _, pattern := range privatePatterns {
		if pattern.Match(data) {
			t.Errorf("%s contains forbidden private-looking content matching %s", path, pattern)
		}
	}
}
