package obsidian

import (
	"bytes"
	"net/url"
	"sort"
	"strings"
)

type Reference struct {
	Start  int
	End    int
	Target string
	Kind   string
	Angle  bool
}

func Parse(data []byte) []Reference {
	protected := protectedBytes(data)
	refs := make([]Reference, 0)

	for i := 0; i+3 < len(data); i++ {
		if protected[i] {
			continue
		}
		open := i
		if data[i] == '!' && i+2 < len(data) && data[i+1] == '[' && data[i+2] == '[' {
			open = i + 1
		}
		if data[open] != '[' || open+1 >= len(data) || data[open+1] != '[' || protected[open] {
			continue
		}
		closeRel := bytes.Index(data[open+2:], []byte("]]"))
		if closeRel < 0 {
			continue
		}
		closePos := open + 2 + closeRel
		inner := data[open+2 : closePos]
		targetEnd := len(inner)
		if index := bytes.IndexAny(inner, "|#"); index >= 0 {
			targetEnd = index
		}
		if targetEnd > 0 {
			start := open + 2
			refs = append(refs, Reference{
				Start:  start,
				End:    start + targetEnd,
				Target: string(inner[:targetEnd]),
				Kind:   "wikilink",
			})
		}
		i = closePos + 1
	}

	for i := 0; i+2 < len(data); i++ {
		if protected[i] || data[i] != ']' || data[i+1] != '(' {
			continue
		}
		closePos := findMarkdownClose(data, i+2, protected)
		if closePos < 0 {
			continue
		}
		start := i + 2
		for start < closePos && isSpace(data[start]) {
			start++
		}
		angle := start < closePos && data[start] == '<'
		if angle {
			start++
		}
		end := start
		for end < closePos {
			if angle {
				if data[end] == '>' {
					break
				}
			} else if isSpace(data[end]) || data[end] == ')' {
				break
			}
			end++
		}
		if fragment := bytes.IndexByte(data[start:end], '#'); fragment >= 0 {
			end = start + fragment
		}
		if end > start {
			target := string(data[start:end])
			if localTarget(target) {
				refs = append(refs, Reference{Start: start, End: end, Target: target, Kind: "markdown", Angle: angle})
			}
		}
		i = closePos
	}

	refs = append(refs, sourceFields(data, protected)...)
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Start == refs[j].Start {
			return refs[i].End < refs[j].End
		}
		return refs[i].Start < refs[j].Start
	})
	return refs
}

func FrontmatterTitle(data []byte) (Reference, bool) {
	start, end, ok := frontmatterBounds(data)
	if !ok {
		return Reference{}, false
	}
	lineStart := start
	for lineStart < end {
		lineEnd := bytes.IndexByte(data[lineStart:end], '\n')
		if lineEnd < 0 {
			lineEnd = end - lineStart
		}
		lineEnd += lineStart
		line := bytes.TrimSuffix(data[lineStart:lineEnd], []byte("\r"))
		colon := bytes.IndexByte(line, ':')
		if colon > 0 && strings.EqualFold(strings.TrimSpace(string(line[:colon])), "title") {
			valueStart := lineStart + colon + 1
			for valueStart < lineEnd && isSpace(data[valueStart]) {
				valueStart++
			}
			valueEnd := lineEnd
			for valueEnd > valueStart && isSpace(data[valueEnd-1]) {
				valueEnd--
			}
			if valueEnd-valueStart >= 2 && ((data[valueStart] == '"' && data[valueEnd-1] == '"') ||
				(data[valueStart] == '\'' && data[valueEnd-1] == '\'')) {
				valueStart++
				valueEnd--
			}
			return Reference{
				Start: valueStart, End: valueEnd, Target: string(data[valueStart:valueEnd]), Kind: "frontmatter-title",
			}, true
		}
		lineStart = lineEnd + 1
	}
	return Reference{}, false
}

func sourceFields(data []byte, protected []bool) []Reference {
	refs := make([]Reference, 0)
	lineStart := 0
	for lineStart < len(data) {
		lineEnd := bytes.IndexByte(data[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(data) - lineStart
		}
		lineEnd += lineStart
		if lineStart < len(protected) && !protected[lineStart] {
			line := bytes.TrimSuffix(data[lineStart:lineEnd], []byte("\r"))
			colon := bytes.IndexByte(line, ':')
			if colon > 0 {
				key := strings.ToLower(strings.TrimSpace(string(line[:colon])))
				if key == "source" || strings.HasPrefix(key, "source ") {
					start := lineStart + colon + 1
					for start < lineEnd && isSpace(data[start]) {
						start++
					}
					end := lineEnd
					for end > start && isSpace(data[end-1]) {
						end--
					}
					if end-start >= 2 && ((data[start] == '"' && data[end-1] == '"') ||
						(data[start] == '\'' && data[end-1] == '\'')) {
						start++
						end--
					}
					if end > start {
						value := string(data[start:end])
						if localTarget(value) && !strings.Contains(value, "[[") && !strings.Contains(value, "](") {
							refs = append(refs, Reference{Start: start, End: end, Target: value, Kind: "source-field"})
						}
					}
				}
			}
		}
		lineStart = lineEnd + 1
	}
	return refs
}

func protectedBytes(data []byte) []bool {
	protected := make([]bool, len(data))
	protectFrontmatterAliases(data, protected)
	inFence := false
	var fence byte
	lineStart := 0
	for lineStart < len(data) {
		lineEnd := bytes.IndexByte(data[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(data) - lineStart
		}
		lineEnd += lineStart
		trimmed := bytes.TrimLeft(data[lineStart:lineEnd], " \t")
		if len(trimmed) >= 3 && ((trimmed[0] == '`' && bytes.HasPrefix(trimmed, []byte("```"))) ||
			(trimmed[0] == '~' && bytes.HasPrefix(trimmed, []byte("~~~")))) {
			if !inFence {
				inFence = true
				fence = trimmed[0]
			} else if trimmed[0] == fence {
				for i := lineStart; i < min(lineEnd+1, len(data)); i++ {
					protected[i] = true
				}
				inFence = false
				lineStart = lineEnd + 1
				continue
			}
		}
		if inFence {
			for i := lineStart; i < min(lineEnd+1, len(data)); i++ {
				protected[i] = true
			}
		}
		lineStart = lineEnd + 1
	}

	for i := 0; i < len(data); {
		if protected[i] {
			i++
			continue
		}
		if i+3 < len(data) && bytes.Equal(data[i:i+4], []byte("<!--")) {
			end := bytes.Index(data[i+4:], []byte("-->"))
			if end < 0 {
				end = len(data) - i - 4
			} else {
				end += 3
			}
			for j := i; j < min(i+4+end, len(data)); j++ {
				protected[j] = true
			}
			i += 4 + end
			continue
		}
		if data[i] == '`' {
			run := 1
			for i+run < len(data) && data[i+run] == '`' {
				run++
			}
			close := bytes.Index(data[i+run:], bytes.Repeat([]byte{'`'}, run))
			if close >= 0 {
				end := i + run + close + run
				for j := i; j < end; j++ {
					protected[j] = true
				}
				i = end
				continue
			}
		}
		i++
	}
	return protected
}

func protectFrontmatterAliases(data []byte, protected []bool) {
	start, end, ok := frontmatterBounds(data)
	if !ok {
		return
	}
	inAliases := false
	lineStart := start
	for lineStart < end {
		lineEnd := bytes.IndexByte(data[lineStart:end], '\n')
		if lineEnd < 0 {
			lineEnd = end - lineStart
		}
		lineEnd += lineStart
		line := bytes.TrimSuffix(data[lineStart:lineEnd], []byte("\r"))
		trimmed := bytes.TrimSpace(line)
		indented := len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
		colon := bytes.IndexByte(trimmed, ':')
		if !indented && colon > 0 {
			key := strings.ToLower(strings.TrimSpace(string(trimmed[:colon])))
			inAliases = key == "alias" || key == "aliases"
		}
		if inAliases {
			for i := lineStart; i < min(lineEnd+1, len(data)); i++ {
				protected[i] = true
			}
		}
		lineStart = lineEnd + 1
	}
}

func frontmatterBounds(data []byte) (int, int, bool) {
	offset := 0
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		offset = 3
	}
	if !bytes.HasPrefix(data[offset:], []byte("---\n")) && !bytes.HasPrefix(data[offset:], []byte("---\r\n")) {
		return 0, 0, false
	}
	firstEnd := bytes.IndexByte(data[offset:], '\n')
	start := offset + firstEnd + 1
	pos := start
	for pos < len(data) {
		lineEnd := bytes.IndexByte(data[pos:], '\n')
		if lineEnd < 0 {
			lineEnd = len(data) - pos
		}
		line := bytes.TrimSpace(data[pos : pos+lineEnd])
		if bytes.Equal(line, []byte("---")) {
			return start, pos, true
		}
		pos += lineEnd + 1
	}
	return 0, 0, false
}

func findMarkdownClose(data []byte, start int, protected []bool) int {
	depth := 0
	for i := start; i < len(data); i++ {
		if protected[i] {
			continue
		}
		switch data[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

func localTarget(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	parsed, err := url.Parse(trimmed)
	return err == nil && parsed.Scheme == "" && !strings.HasPrefix(trimmed, "//")
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}
