package naming

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/antopolskiy/vault-rename/internal/apperr"
)

var (
	uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	hexPattern  = regexp.MustCompile(`(?i)^[0-9a-f]{16,64}$`)
	captureID   = regexp.MustCompile(`(?i)^(?:img|dsc|pxl|scan|screenshot|recording|audio|video)[-_ ]?\d+$`)
	datePrefix  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[ -]\d{4})?(?: |$)`)
)

var generic = map[string]struct{}{
	"untitled": {}, "note": {}, "notes": {}, "document": {}, "documents": {},
	"file": {}, "files": {}, "final": {}, "latest": {}, "copy": {}, "new note": {},
	"new document": {}, "new file": {}, "transcript": {}, "recording": {},
}

var windowsReserved = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {}, "com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {}, "lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

type Validation struct {
	CaseOnly bool
}

func Validate(sourcePath, newName string) (Validation, error) {
	if newName == "" || newName != filepath.Base(newName) || strings.ContainsAny(newName, `/\`) {
		return Validation{}, invalid("new name must be a basename")
	}
	if !utf8.ValidString(newName) {
		return Validation{}, invalid("new name must be valid UTF-8")
	}
	if newName != strings.TrimSpace(newName) || strings.HasSuffix(newName, ".") || strings.Contains(newName, "  ") {
		return Validation{}, invalid("new name contains irregular whitespace or a trailing period")
	}
	for _, r := range newName {
		if unicode.IsControl(r) {
			return Validation{}, invalid("new name contains a control character")
		}
	}

	oldName := filepath.Base(sourcePath)
	if newName == oldName {
		return Validation{}, apperr.New(apperr.CodeNoChange, "source already has that name")
	}
	oldExt := filepath.Ext(oldName)
	newExt := filepath.Ext(newName)
	if oldExt == "" || newExt == "" || oldExt != newExt {
		return Validation{}, invalid("new name must preserve the source file extension")
	}
	if newExt != strings.ToLower(newExt) {
		return Validation{}, invalid("file extension must be lowercase")
	}

	stem := strings.TrimSuffix(newName, newExt)
	if stem == "" || !containsLetterOrDigit(stem) {
		return Validation{}, invalid("new name needs a descriptive stem")
	}
	if _, ok := windowsReserved[strings.ToLower(strings.SplitN(stem, ".", 2)[0])]; ok {
		return Validation{}, invalid("new name is reserved on Windows")
	}
	if unhelpful(stem) {
		return Validation{}, invalid("new name is generic, generated, or identifier-only")
	}

	if strings.EqualFold(newExt, ".md") {
		if err := validateMarkdown(stem); err != nil {
			return Validation{}, err
		}
	} else if err := validateOther(stem); err != nil {
		return Validation{}, err
	}

	oldCanonical := Canonical(oldName)
	newCanonical := Canonical(newName)
	if oldCanonical == newCanonical && norm.NFC.String(oldName) == norm.NFC.String(newName) {
		return Validation{CaseOnly: true}, nil
	}
	if oldCanonical == newCanonical && strings.EqualFold(oldName, newName) {
		return Validation{CaseOnly: true}, nil
	}
	return Validation{}, nil
}

func CheckCollision(parent, sourceName, newName string) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot inspect target directory", err)
	}
	targetCanonical := Canonical(newName)
	for _, entry := range entries {
		if entry.Name() == sourceName {
			continue
		}
		if Canonical(entry.Name()) == targetCanonical {
			return apperr.WithDetails(
				apperr.New(apperr.CodeTargetExists, "target name already exists or is canonically equivalent"),
				map[string]any{"existing": entry.Name()},
			)
		}
	}
	return nil
}

func Canonical(value string) string {
	return cases.Fold().String(norm.NFC.String(value))
}

func validateMarkdown(stem string) error {
	if strings.Contains(stem, "_") {
		return invalid("Markdown filenames must be human-readable titles, not underscore-separated slugs")
	}
	withoutDate := datePrefix.ReplaceAllString(stem, "")
	if strings.Contains(withoutDate, "-") && !strings.ContainsAny(withoutDate, " \t") &&
		withoutDate == strings.ToLower(withoutDate) {
		return invalid("Markdown filenames must be human-readable titles, not kebab-case slugs")
	}
	if strings.Contains(stem, ".") {
		return invalid("Markdown title stem cannot contain a technical suffix")
	}
	title := strings.TrimSpace(datePrefix.ReplaceAllString(stem, ""))
	if strings.Contains(title, " ") {
		for _, r := range title {
			if unicode.IsLetter(r) {
				if unicode.IsLower(r) && !unicode.In(r, unicode.Georgian) {
					return invalid("Markdown filenames must use normal sentence capitalization")
				}
				break
			}
		}
	}
	return nil
}

func validateOther(stem string) error {
	if stem != strings.ToLower(stem) || strings.ContainsAny(stem, "_ \t\r\n") ||
		strings.HasPrefix(stem, "-") || strings.HasSuffix(stem, "-") || strings.Contains(stem, "--") {
		return invalid("non-Markdown filenames must use lowercase kebab-case")
	}
	for _, r := range stem {
		if r != '-' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return invalid("non-Markdown filenames must use letters, numbers, and single hyphens")
		}
	}
	if !containsLetter(stem) {
		return invalid("non-Markdown filename needs a semantic word")
	}
	return nil
}

func unhelpful(stem string) bool {
	folded := strings.ToLower(strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(stem)))
	folded = strings.Join(strings.Fields(folded), " ")
	if _, ok := generic[folded]; ok {
		return true
	}
	compact := strings.ReplaceAll(folded, " ", "")
	return strings.HasPrefix(folded, "http ") || strings.HasPrefix(folded, "https ") ||
		strings.HasPrefix(folded, "www.") || uuidPattern.MatchString(folded) ||
		hexPattern.MatchString(compact) || captureID.MatchString(folded) ||
		strings.HasSuffix(folded, " copy") || strings.HasSuffix(folded, " final") ||
		strings.HasSuffix(folded, " latest")
}

func containsLetterOrDigit(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func containsLetter(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func invalid(message string) error {
	return apperr.New(apperr.CodeInvalidName, message)
}
