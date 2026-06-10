package highlight

import "strings"

// BlockKind distinguishes prose from fenced code in assistant text.
type BlockKind int

const (
	BlockProse BlockKind = iota
	BlockCode
)

// Block is a prose or fenced-code segment of a message.
type Block struct {
	Kind BlockKind
	Lang string
	Text string
}

// ParseBlocks splits text on markdown-style triple-backtick fences.
func ParseBlocks(text string) []Block {
	if text == "" {
		return nil
	}

	parts := strings.Split(text, "```")
	if len(parts) == 1 {
		return []Block{{Kind: BlockProse, Text: text}}
	}

	var blocks []Block
	for i, part := range parts {
		if i%2 == 0 {
			if part != "" {
				blocks = append(blocks, Block{Kind: BlockProse, Text: part})
			}
			continue
		}

		lang, code := splitFence(part)
		blocks = append(blocks, Block{
			Kind: BlockCode,
			Lang: lang,
			Text: strings.TrimRight(code, "\n"),
		})
	}
	return blocks
}

func splitFence(part string) (lang, code string) {
	part = strings.TrimPrefix(part, "\n")
	part = strings.TrimPrefix(part, "\r\n")
	if part == "" {
		return "", ""
	}

	lines := strings.SplitN(part, "\n", 2)
	first := strings.TrimSpace(lines[0])
	if len(lines) == 1 {
		if looksLikeLangTag(first) {
			return normalizeLang(first), ""
		}
		return "", part
	}

	if looksLikeLangTag(first) {
		return normalizeLang(first), lines[1]
	}
	return "", part
}

func looksLikeLangTag(s string) bool {
	if s == "" || len(s) > 24 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '+' || r == '#' || r == '-':
		default:
			return false
		}
	}
	return true
}

func normalizeLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "js", "javascript", "jsx", "mjs", "cjs":
		return "javascript"
	case "ts", "typescript", "tsx":
		return "typescript"
	case "py", "python":
		return "python"
	case "sh", "bash", "shell", "zsh":
		return "bash"
	case "golang":
		return "go"
	default:
		return lang
	}
}
