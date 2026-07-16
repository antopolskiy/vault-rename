package patch

import (
	"bytes"
	"testing"

	"github.com/antopolskiy/vault-rename/internal/model"
)

func TestApplyChangesOnlyDeclaredRanges(t *testing.T) {
	input := []byte("before [[Old note|alias]] middle Old note prose after")
	start := bytes.Index(input, []byte("Old note"))
	patches := []model.Patch{{
		Start: start, End: start + len("Old note"),
		Before: []byte("Old note"), After: []byte("New title"),
	}}
	output, err := Apply(input, patches)
	if err != nil {
		t.Fatal(err)
	}
	want := "before [[New title|alias]] middle Old note prose after"
	if string(output) != want {
		t.Fatalf("output = %q", output)
	}
}

func TestApplyRejectsOverlapAndChangedBytes(t *testing.T) {
	input := []byte("abcdef")
	for name, patches := range map[string][]model.Patch{
		"overlap": {
			{Start: 1, End: 3, Before: []byte("bc"), After: []byte("x")},
			{Start: 2, End: 4, Before: []byte("cd"), After: []byte("y")},
		},
		"changed": {{Start: 1, End: 3, Before: []byte("xx"), After: []byte("y")}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Apply(input, patches); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
