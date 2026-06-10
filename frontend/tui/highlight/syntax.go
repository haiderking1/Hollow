package highlight

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var keywordSets = map[string]map[string]struct{}{
	"javascript": jsKeywords(),
	"typescript": jsKeywords(),
	"go":         goKeywords(),
	"python":     pyKeywords(),
	"bash":       bashKeywords(),
	"json":       {},
}

func jsKeywords() map[string]struct{} {
	words := []string{
		"async", "await", "break", "case", "catch", "class", "const", "continue",
		"debugger", "default", "delete", "do", "else", "export", "extends", "false",
		"finally", "for", "from", "function", "if", "import", "in", "instanceof",
		"let", "new", "null", "of", "return", "static", "super", "switch", "this",
		"throw", "true", "try", "typeof", "undefined", "var", "void", "while", "with",
		"yield", "interface", "type", "enum", "implements", "package", "private",
		"protected", "public", "readonly", "declare", "namespace", "module", "as", "is",
	}
	return set(words)
}

func goKeywords() map[string]struct{} {
	words := []string{
		"break", "case", "chan", "const", "continue", "default", "defer", "else",
		"fallthrough", "for", "func", "go", "goto", "if", "import", "interface",
		"map", "package", "range", "return", "select", "struct", "switch", "type",
		"var", "true", "false", "nil", "iota",
	}
	return set(words)
}

func pyKeywords() map[string]struct{} {
	words := []string{
		"and", "as", "assert", "async", "await", "break", "class", "continue", "def",
		"del", "elif", "else", "except", "False", "finally", "for", "from", "global",
		"if", "import", "in", "is", "lambda", "None", "nonlocal", "not", "or", "pass",
		"raise", "return", "True", "try", "while", "with", "yield",
	}
	return set(words)
}

func bashKeywords() map[string]struct{} {
	words := []string{
		"if", "then", "else", "elif", "fi", "for", "in", "do", "done", "case", "esac",
		"function", "select", "until", "while", "export", "local", "return", "exit",
		"echo", "cd", "pwd", "source", "readonly", "declare", "unset", "set", "shift",
	}
	return set(words)
}

func set(words []string) map[string]struct{} {
	m := make(map[string]struct{}, len(words))
	for _, w := range words {
		m[w] = struct{}{}
	}
	return m
}

// HighlightCode applies Gruvbox syntax colors to source text.
func HighlightCode(lang, src string, p Palette) string {
	lang = normalizeLang(lang)
	keywords := keywordSets[lang]
	if lang == "json" {
		return highlightJSON(src, p)
	}

	var out strings.Builder
	i := 0
	for i < len(src) {
		switch {
		case matchPrefix(src, i, "//"):
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				out.WriteString(p.paint(p.Comment, src[i:]))
				i = len(src)
			} else {
				out.WriteString(p.paint(p.Comment, src[i:i+j]))
				i += j
			}
		case matchPrefix(src, i, "#") && lang != "javascript" && lang != "typescript" && lang != "go":
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				out.WriteString(p.paint(p.Comment, src[i:]))
				i = len(src)
			} else {
				out.WriteString(p.paint(p.Comment, src[i:i+j]))
				i += j
			}
		case matchPrefix(src, i, "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				out.WriteString(p.paint(p.Comment, src[i:]))
				i = len(src)
			} else {
				end += i + 2
				out.WriteString(p.paint(p.Comment, src[i:end+2]))
				i = end + 2
			}
		case src[i] == '"' || src[i] == '\'' || src[i] == '`':
			quote := src[i]
			j := readString(src, i+1, quote)
			out.WriteString(p.paint(p.String, src[i:j]))
			i = j
		default:
			r, size := utf8.DecodeRuneInString(src[i:])
			if unicode.IsDigit(r) || (r == '.' && i+size < len(src) && unicode.IsDigit(rune(src[i+size]))) {
				j := readNumber(src, i)
				out.WriteString(p.paint(p.Number, src[i:j]))
				i = j
			} else if unicode.IsLetter(r) || r == '_' {
				j := readIdent(src, i)
				word := src[i:j]
				if _, ok := keywords[word]; ok {
					out.WriteString(p.paint(p.Keyword, word))
				} else if lang == "go" && i > 0 && src[i-1] == '.' {
					out.WriteString(p.paint(p.Function, word))
				} else if looksLikeType(word, lang) {
					out.WriteString(p.paint(p.Type, word))
				} else {
					out.WriteString(p.paint(p.Fg, word))
				}
				i = j
			} else {
				if isPunct(r) {
					out.WriteString(p.paint(p.Punctuation, src[i:i+size]))
				} else {
					out.WriteString(p.paint(p.Fg, src[i:i+size]))
				}
				i += size
			}
		}
	}
	return out.String()
}

func highlightJSON(src string, p Palette) string {
	var out strings.Builder
	i := 0
	for i < len(src) {
		switch src[i] {
		case '"':
			j := readString(src, i+1, '"')
			key := src[i:j]
			rest := strings.TrimLeft(src[j:], " \t\n\r:")
			if strings.HasPrefix(rest, ":") {
				out.WriteString(p.paint(p.Type, key))
			} else {
				out.WriteString(p.paint(p.String, key))
			}
			i = j
		case 't', 'f', 'n':
			if word, ok := readJSONLiteral(src, i); ok {
				out.WriteString(p.paint(p.Keyword, word))
				i += len(word)
				continue
			}
			out.WriteString(p.paint(p.Fg, src[i:i+1]))
			i++
		default:
			if r, size := utf8.DecodeRuneInString(src[i:]); unicode.IsDigit(r) || r == '-' {
				j := readNumber(src, i)
				out.WriteString(p.paint(p.Number, src[i:j]))
				i = j
			} else if isPunct(r) {
				out.WriteString(p.paint(p.Punctuation, src[i:i+size]))
				i += size
			} else {
				out.WriteString(p.paint(p.Fg, src[i:i+size]))
				i += size
			}
		}
	}
	return out.String()
}

func matchPrefix(s string, i int, prefix string) bool {
	return i+len(prefix) <= len(s) && s[i:i+len(prefix)] == prefix
}

func readString(s string, i int, quote byte) int {
	escaped := false
	for i < len(s) {
		if escaped {
			escaped = false
			i++
			continue
		}
		if s[i] == '\\' {
			escaped = true
			i++
			continue
		}
		if s[i] == quote {
			return i + 1
		}
		if quote == '`' && s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			depth := 1
			i += 2
			for i < len(s) && depth > 0 {
				switch s[i] {
				case '{':
					depth++
				case '}':
					depth--
				}
				i++
			}
			continue
		}
		i++
	}
	return len(s)
}

func readNumber(s string, i int) int {
	j := i
	if j < len(s) && s[j] == '-' {
		j++
	}
	for j < len(s) {
		r, size := utf8.DecodeRuneInString(s[j:])
		if unicode.IsDigit(r) || r == '.' || r == 'x' || r == 'X' ||
			(r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			j += size
			continue
		}
		break
	}
	if j == i || (j == i+1 && s[i] == '-') {
		return i + 1
	}
	return j
}

func readIdent(s string, i int) int {
	j := i
	for j < len(s) {
		r, size := utf8.DecodeRuneInString(s[j:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			j += size
			continue
		}
		break
	}
	return j
}

func readJSONLiteral(s string, i int) (string, bool) {
	for _, lit := range []string{"true", "false", "null"} {
		if strings.HasPrefix(s[i:], lit) {
			return lit, true
		}
	}
	return "", false
}

func looksLikeType(word, lang string) bool {
	if lang != "go" && lang != "typescript" {
		return false
	}
	if len(word) == 0 {
		return false
	}
	return unicode.IsUpper(rune(word[0]))
}

func isPunct(r rune) bool {
	switch r {
	case '(', ')', '[', ']', '{', '}', ',', ';', ':', '.', '+', '-', '*', '/', '%',
		'=', '!', '<', '>', '&', '|', '^', '~', '?':
		return true
	default:
		return false
	}
}
