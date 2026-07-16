package naming

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/antopolskiy/vault-rename/internal/apperr"
)

func TestValidateNames(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		target  string
		wantErr bool
	}{
		{"markdown title", "old.md", "Portfolio diversification principles.md", false},
		{"dated markdown title", "old.md", "2026-07-04 Project review — Delivery risks.md", false},
		{"one word markdown", "old.md", "Mercury.md", false},
		{"markdown kebab", "old.md", "portfolio-diversification.md", true},
		{"markdown lowercase phrase", "old.md", "portfolio diversification.md", true},
		{"non markdown slug", "old.pdf", "20260704-reference-document.pdf", false},
		{"non markdown spaces", "old.pdf", "Reference document.pdf", true},
		{"generic", "old.md", "Untitled.md", true},
		{"uuid", "old.md", "550e8400-e29b-41d4-a716-446655440000.md", true},
		{"extension change", "old.pdf", "document.md", true},
		{"path", "old.md", "folder/Good title.md", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Validate(test.source, test.target)
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateCaseOnly(t *testing.T) {
	result, err := Validate("INBOX title.md", "Inbox title.md")
	if err != nil {
		t.Fatal(err)
	}
	if !result.CaseOnly {
		t.Fatal("expected case-only rename")
	}
}

func TestCheckCollisionUsesCanonicalNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Résumé.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := CheckCollision(dir, "source.md", "Re\u0301sume\u0301.md")
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeTargetExists {
		t.Fatalf("error = %v", err)
	}
}
