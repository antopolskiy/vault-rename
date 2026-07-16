package planner

import (
	"bytes"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/antopolskiy/vault-rename/internal/apperr"
	"github.com/antopolskiy/vault-rename/internal/config"
	"github.com/antopolskiy/vault-rename/internal/model"
	"github.com/antopolskiy/vault-rename/internal/naming"
	"github.com/antopolskiy/vault-rename/internal/obsidian"
	"github.com/antopolskiy/vault-rename/internal/patch"
)

type fileRecord struct {
	rel  string
	abs  string
	mode fs.FileMode
}

type fileIndex struct {
	records []fileRecord
	byRel   map[string][]string
	byNoExt map[string][]string
	byName  map[string][]string
	byStem  map[string][]string
	byAbs   map[string]string
}

func Build(root, sourceArg, newName string, cfg config.Config, backlinks model.BacklinkMode, skipPaths ...string) (model.Plan, error) {
	root, err := canonicalRoot(root)
	if err != nil {
		return model.Plan{}, err
	}
	sourceAbs, sourceRel, sourceInfo, err := source(root, sourceArg)
	if err != nil {
		return model.Plan{}, err
	}
	validation, err := naming.Validate(sourceAbs, newName)
	if err != nil {
		return model.Plan{}, err
	}
	if err := naming.CheckCollision(filepath.Dir(sourceAbs), filepath.Base(sourceAbs), newName); err != nil {
		return model.Plan{}, err
	}
	destinationRel := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(sourceRel)), newName))
	destinationAbs := filepath.Join(root, filepath.FromSlash(destinationRel))
	if destinationAbs != sourceAbs && !validation.CaseOnly {
		if _, err := os.Lstat(destinationAbs); err == nil {
			return model.Plan{}, apperr.New(apperr.CodeTargetExists, "target already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return model.Plan{}, apperr.Wrap(apperr.CodeIOError, "cannot inspect target path", err)
		}
	}

	index, err := scan(root, skipPaths...)
	if err != nil {
		return model.Plan{}, err
	}
	sourceBytes, err := os.ReadFile(sourceAbs) //nolint:gosec // sourceAbs is validated to be inside the canonical vault root.
	if err != nil {
		return model.Plan{}, apperr.Wrap(apperr.CodeIOError, "cannot read source file", err)
	}

	plan := model.Plan{
		Root:             root,
		Source:           sourceRel,
		Destination:      destinationRel,
		SourceHash:       patch.Hash(sourceBytes),
		SourceMode:       sourceInfo.Mode(),
		CaseOnly:         validation.CaseOnly,
		Backlinks:        backlinks,
		UnsupportedMode:  cfg.UnsupportedReferences,
		FrontmatterTitle: cfg.FrontmatterTitle,
	}

	changes := make(map[string][]model.Patch)
	roles := make(map[string]string)
	oldName := filepath.Base(sourceAbs)
	oldStem := strings.TrimSuffix(oldName, filepath.Ext(oldName))
	newStem := strings.TrimSuffix(newName, filepath.Ext(newName))
	referencesFound := 0

	for _, record := range index.records {
		ext := strings.ToLower(filepath.Ext(record.rel))
		switch ext {
		case ".md":
			data, readErr := os.ReadFile(record.abs)
			if readErr != nil {
				return model.Plan{}, apperr.Wrap(apperr.CodeIOError, "cannot read Markdown file", readErr)
			}
			for _, ref := range obsidian.Parse(data) {
				candidates := resolve(ref.Target, ref.Kind, record.rel, root, index)
				if contains(candidates, sourceRel) {
					if len(candidates) != 1 {
						return model.Plan{}, apperr.WithDetails(
							apperr.New(apperr.CodeAmbiguousReference, "reference to source is ambiguous"),
							map[string]any{"path": record.rel, "target": ref.Target, "candidates": candidates},
						)
					}
					referencesFound++
					if backlinks == model.BacklinksRepair {
						replacement := rewriteTarget(ref, oldName, oldStem, newName, newStem)
						if replacement != ref.Target {
							changes[record.rel] = append(changes[record.rel], model.Patch{
								Path: record.rel, Start: ref.Start, End: ref.End,
								Before: append([]byte(nil), data[ref.Start:ref.End]...),
								After:  []byte(replacement), Kind: ref.Kind,
								OldTarget: ref.Target, NewTarget: replacement, ReferenceEdit: true,
							})
							roles[record.rel] = roleFor(record.rel, sourceRel)
							plan.LinksUpdated++
						}
					}
				} else if len(candidates) == 0 && resemblesSource(ref.Target, oldName, oldStem, sourceRel) {
					return model.Plan{}, apperr.WithDetails(
						apperr.New(apperr.CodeAmbiguousReference, "source-like reference cannot be resolved safely"),
						map[string]any{"path": record.rel, "target": ref.Target},
					)
				}
			}

			if record.rel == sourceRel && cfg.FrontmatterTitle == model.FrontmatterTitleExact {
				if title, ok := obsidian.FrontmatterTitle(data); ok && title.Target == oldStem {
					changes[record.rel] = append(changes[record.rel], model.Patch{
						Path: record.rel, Start: title.Start, End: title.End,
						Before: append([]byte(nil), data[title.Start:title.End]...),
						After:  []byte(newStem), Kind: title.Kind,
						OldTarget: oldStem, NewTarget: newStem,
					})
					roles[record.rel] = "source"
				}
			}
		case ".canvas", ".base", ".excalidraw", ".json":
			data, readErr := os.ReadFile(record.abs)
			if readErr != nil {
				return model.Plan{}, apperr.Wrap(apperr.CodeIOError, "cannot read structured vault file", readErr)
			}
			if bytes.Contains(data, []byte(oldName)) || bytes.Contains(data, []byte(sourceRel)) {
				if cfg.UnsupportedReferences == model.UnsupportedError {
					return model.Plan{}, apperr.WithDetails(
						apperr.New(apperr.CodeUnsupportedReference, "source is referenced by an unsupported structured format"),
						map[string]any{"path": record.rel},
					)
				}
				plan.Warnings = append(plan.Warnings, model.Warning{
					Code: "UNSUPPORTED_REFERENCE", Path: record.rel,
					Message: "possible source reference found in an unsupported structured format",
				})
			}
		}
	}

	if backlinks == model.BacklinksCheck && referencesFound > 0 {
		return model.Plan{}, apperr.WithDetails(
			apperr.New(apperr.CodeReferencesPresent, "references require repair before this rename"),
			map[string]any{"references": referencesFound},
		)
	}
	if backlinks == model.BacklinksOff && referencesFound > 0 {
		plan.Warnings = append(plan.Warnings, model.Warning{
			Code:    "REFERENCES_NOT_REPAIRED",
			Message: "references were found but backlink repair is disabled",
		})
	}

	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		record := index.record(rel)
		data, readErr := os.ReadFile(record.abs)
		if readErr != nil {
			return model.Plan{}, apperr.Wrap(apperr.CodeIOError, "cannot reread planned file", readErr)
		}
		rendered, applyErr := patch.Apply(data, changes[rel])
		if applyErr != nil {
			return model.Plan{}, apperr.Wrap(apperr.CodeIOError, "invalid patch plan", applyErr)
		}
		plan.FileChanges = append(plan.FileChanges, model.FileChange{
			Path: rel, Role: roles[rel], BeforeHash: patch.Hash(data), AfterHash: patch.Hash(rendered),
			Mode: record.mode, Patches: changes[rel],
		})
	}
	return plan, nil
}

func canonicalRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeIOError, "cannot resolve vault root", err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeIOError, "cannot canonicalize vault root", err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return "", apperr.New(apperr.CodeSourceOutsideVault, "vault root is not a directory")
	}
	return filepath.Clean(absolute), nil
}

func source(root, value string) (string, string, fs.FileInfo, error) {
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", nil, apperr.New(apperr.CodeSourceOutsideVault, "source must be inside the vault")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", nil, apperr.New(apperr.CodeSourceNotFound, "source does not exist")
	}
	if err != nil {
		return "", "", nil, apperr.Wrap(apperr.CodeIOError, "cannot inspect source", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", "", nil, apperr.New(apperr.CodeIOError, "source must be a regular, non-symlink file")
	}
	return path, filepath.ToSlash(rel), info, nil
}

func scan(root string, skipPaths ...string) (fileIndex, error) {
	index := fileIndex{
		byRel: make(map[string][]string), byNoExt: make(map[string][]string),
		byName: make(map[string][]string), byStem: make(map[string][]string),
		byAbs: make(map[string]string),
	}
	skips := make([]string, 0, len(skipPaths))
	for _, path := range skipPaths {
		if path != "" {
			absolute, _ := filepath.Abs(path)
			skips = append(skips, filepath.Clean(absolute))
		}
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != root && entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") || withinAny(path, skips) {
				return filepath.SkipDir
			}
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		record := fileRecord{rel: rel, abs: path, mode: info.Mode()}
		index.records = append(index.records, record)
		index.byAbs[filepath.Clean(path)] = rel
		index.byRel[naming.Canonical(rel)] = append(index.byRel[naming.Canonical(rel)], rel)
		noExt := strings.TrimSuffix(rel, filepath.Ext(rel))
		index.byNoExt[naming.Canonical(noExt)] = append(index.byNoExt[naming.Canonical(noExt)], rel)
		name := filepath.Base(rel)
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		index.byName[naming.Canonical(name)] = append(index.byName[naming.Canonical(name)], rel)
		index.byStem[naming.Canonical(stem)] = append(index.byStem[naming.Canonical(stem)], rel)
		return nil
	})
	if err != nil {
		return fileIndex{}, apperr.Wrap(apperr.CodeIOError, "cannot scan vault", err)
	}
	sort.Slice(index.records, func(i, j int) bool { return index.records[i].rel < index.records[j].rel })
	return index, nil
}

func resolve(target, kind, referrer, root string, index fileIndex) []string {
	decoded := target
	if kind == "markdown" {
		if value, err := url.PathUnescape(target); err == nil {
			decoded = value
		}
	}
	decoded = filepath.ToSlash(filepath.Clean(filepath.FromSlash(decoded)))
	if filepath.IsAbs(filepath.FromSlash(decoded)) {
		absolute := filepath.Clean(filepath.FromSlash(decoded))
		if rel, ok := index.byAbs[absolute]; ok {
			return []string{rel}
		}
		if rel, err := filepath.Rel(root, absolute); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			decoded = filepath.ToSlash(rel)
		}
	}
	decoded = strings.TrimPrefix(decoded, "/")
	candidates := make(map[string]struct{})
	add := func(values []string) {
		for _, value := range values {
			candidates[value] = struct{}{}
		}
	}
	add(index.byRel[naming.Canonical(decoded)])
	add(index.byNoExt[naming.Canonical(decoded)])
	refDir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(referrer)))
	relative := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.FromSlash(refDir), filepath.FromSlash(decoded))))
	add(index.byRel[naming.Canonical(relative)])
	add(index.byNoExt[naming.Canonical(relative)])
	if !strings.Contains(decoded, "/") {
		add(index.byName[naming.Canonical(decoded)])
		add(index.byStem[naming.Canonical(decoded)])
	}
	out := make([]string, 0, len(candidates))
	for value := range candidates {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func rewriteTarget(ref obsidian.Reference, oldName, oldStem, newName, newStem string) string {
	decoded := ref.Target
	encoded := false
	if ref.Kind == "markdown" {
		if value, err := url.PathUnescape(ref.Target); err == nil {
			decoded = value
			encoded = value != ref.Target
		}
	}
	slash := strings.LastIndex(decoded, "/")
	prefix, base := "", decoded
	if slash >= 0 {
		prefix, base = decoded[:slash+1], decoded[slash+1:]
	}
	switch naming.Canonical(base) {
	case naming.Canonical(oldName):
		base = newName
	case naming.Canonical(oldStem):
		base = newStem
	default:
		return ref.Target
	}
	result := prefix + base
	if ref.Kind == "markdown" && !ref.Angle && (encoded || strings.ContainsAny(result, " \t") || !isASCII(result)) {
		return encodePath(result)
	}
	return result
}

func resemblesSource(target, oldName, oldStem, sourceRel string) bool {
	decoded, err := url.PathUnescape(target)
	if err != nil {
		decoded = target
	}
	decoded = strings.TrimPrefix(filepath.ToSlash(decoded), "/")
	base := filepath.Base(filepath.FromSlash(decoded))
	return naming.Canonical(base) == naming.Canonical(oldName) ||
		naming.Canonical(base) == naming.Canonical(oldStem) ||
		naming.Canonical(decoded) == naming.Canonical(sourceRel) ||
		naming.Canonical(strings.TrimSuffix(decoded, filepath.Ext(decoded))) ==
			naming.Canonical(strings.TrimSuffix(sourceRel, filepath.Ext(sourceRel)))
}

func encodePath(value string) string {
	parts := strings.Split(value, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func roleFor(path, source string) string {
	if path == source {
		return "source"
	}
	return "backlink"
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func withinAny(path string, parents []string) bool {
	for _, parent := range parents {
		if path == parent || strings.HasPrefix(path, parent+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}

func (index fileIndex) record(rel string) fileRecord {
	for _, record := range index.records {
		if record.rel == rel {
			return record
		}
	}
	return fileRecord{}
}
