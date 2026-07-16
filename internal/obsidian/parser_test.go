package obsidian

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestParseRealWorldShapes(t *testing.T) {
	data := []byte(`---
title: "Old note"
aliases:
  - "[[Old note]]"
source: ../inbox/old-source.pdf
---
[[Old note|Alias]] ![[folder/Old note.md#Heading|300]]
[label](<../inbox/Old note.md>) ![image](../inbox/Old%20note.md)
Source PDF: ../inbox/old-source.pdf
` + "`[[Old note]]`" + `
<!-- [[Old note]] -->
~~~md
[[Old note]]
~~~
`)
	refs := Parse(data)
	var targets []string
	for _, ref := range refs {
		targets = append(targets, ref.Target)
	}
	sort.Strings(targets)
	joined := strings.Join(targets, "\n")
	for _, want := range []string{
		"Old note", "folder/Old note.md", "../inbox/Old note.md",
		"../inbox/Old%20note.md", "../inbox/old-source.pdf",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %#v", want, targets)
		}
	}
	if strings.Count(joined, "Old note") != 3 {
		t.Fatalf("code/comment references should be ignored: %#v", targets)
	}
	title, ok := FrontmatterTitle(data)
	if !ok || title.Target != "Old note" {
		t.Fatalf("title = %#v, %v", title, ok)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("[[Note]]"))
	f.Add([]byte("![x](<folder/File name.pdf>)"))
	f.Add([]byte("```\n[[ignored]]\n```"))
	f.Fuzz(func(t *testing.T, data []byte) {
		refs := Parse(data)
		lastStart := -1
		for _, ref := range refs {
			if ref.Start < 0 || ref.End < ref.Start || ref.End > len(data) {
				t.Fatalf("invalid span: %#v for %d bytes", ref, len(data))
			}
			if ref.Start < lastStart {
				t.Fatalf("references not deterministic: %#v", refs)
			}
			lastStart = ref.Start
		}
	})
}

func TestParsePreservesByteOrientedEdgeCases(t *testing.T) {
	data := append([]byte{0xef, 0xbb, 0xbf}, []byte("---\r\ntitle: Old note\r\n---\r\n")...)
	data = append(data, []byte(strings.Repeat("x", 128*1024)+" [[Old note]]")...)
	data = append(data, 0xff)
	refs := Parse(data)
	if len(refs) != 1 || refs[0].Target != "Old note" {
		t.Fatalf("refs = %#v", refs)
	}
	title, ok := FrontmatterTitle(data)
	if !ok || title.Target != "Old note" {
		t.Fatalf("title = %#v, %v", title, ok)
	}
}

func TestLiveVaultReadOnly(t *testing.T) {
	root := os.Getenv("VAULT_RENAME_LIVE_VAULT")
	if root == "" {
		t.Skip("VAULT_RENAME_LIVE_VAULT is not set")
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error { //nolint:gosec // opt-in read-only corpus root.
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root && strings.HasPrefix(entry.Name(), ".") {
			return filepath.SkipDir
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // path comes from the read-only WalkDir traversal.
		if err != nil {
			return err
		}
		for _, ref := range Parse(data) {
			if ref.Start < 0 || ref.End < ref.Start || ref.End > len(data) {
				t.Fatalf("%s: invalid span %#v", path, ref)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func BenchmarkParseLargeImportedNote(b *testing.B) {
	line := []byte("- [[folder/Old note.md#Heading|Alias]] [source](../folder/Old%20note.md)\n")
	data := bytes.Repeat(line, 10_000)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_ = Parse(data)
	}
}
