package opencode

import (
	"strings"
	"unicode"
)

var (
	thinkOpenTags = []string{
		string([]byte{0x3c, 0x74, 0x68, 0x69, 0x6e, 0x6b, 0x3e}),
		string([]byte{0x3c, 0x72, 0x65, 0x64, 0x61, 0x63, 0x74, 0x65, 0x64, 0x5f, 0x74, 0x68, 0x69, 0x6e, 0x6b, 0x69, 0x6e, 0x67, 0x3e}),
	}
	thinkCloseTags = []string{
		string([]byte{0x3c, 0x2f, 0x74, 0x68, 0x69, 0x6e, 0x6b, 0x3e}),
		string([]byte{0x3c, 0x2f, 0x72, 0x65, 0x64, 0x61, 0x63, 0x74, 0x65, 0x64, 0x5f, 0x74, 0x68, 0x69, 0x6e, 0x6b, 0x69, 0x6e, 0x67, 0x3e}),
	}
)

func maxThinkTagLen() int {
	n := 0
	for _, t := range thinkOpenTags {
		if len(t) > n {
			n = len(t)
		}
	}
	for _, t := range thinkCloseTags {
		if len(t) > n {
			n = len(t)
		}
	}
	return n
}

func findThinkOpen(s string, from int) (idx, tagLen int) {
	lower := strings.ToLower(s[from:])
	best := -1
	bestLen := 0
	for _, tag := range thinkOpenTags {
		if i := strings.Index(lower, strings.ToLower(tag)); i >= 0 && (best < 0 || i < best) {
			best = i
			bestLen = len(tag)
		}
	}
	if best < 0 {
		return -1, 0
	}
	return from + best, bestLen
}

func closeTagForOpen(openTag string) string {
	for i, open := range thinkOpenTags {
		if strings.EqualFold(openTag, open) {
			return thinkCloseTags[i]
		}
	}
	return thinkCloseTags[0]
}

func findThinkCloseFrom(s string, from int, openTag string) (idx, tagLen int) {
	closeTag := closeTagForOpen(openTag)
	lower := strings.ToLower(s[from:])
	if i := strings.Index(lower, strings.ToLower(closeTag)); i >= 0 {
		return from + i, len(closeTag)
	}
	best := -1
	bestLen := 0
	for _, tag := range thinkCloseTags {
		if i := strings.Index(lower, strings.ToLower(tag)); i >= 0 && (best < 0 || i < best) {
			best = i
			bestLen = len(tag)
		}
	}
	if best < 0 {
		return -1, 0
	}
	return from + best, bestLen
}

func skipSpace(s string, from int) int {
	for from < len(s) && unicode.IsSpace(rune(s[from])) {
		from++
	}
	return from
}

func SplitEmbeddedThinking(s string) (text, thinking string) {
	var textParts, thinkParts []string
	i := 0
	for i < len(s) {
		openIdx, openLen := findThinkOpen(s, i)
		if openIdx < 0 {
			textParts = append(textParts, s[i:])
			break
		}
		textParts = append(textParts, s[i:openIdx])
		thinkStart := openIdx + openLen
		openTag := s[openIdx : openIdx+openLen]
		closeIdx, closeLen := findThinkCloseFrom(s, thinkStart, openTag)
		if closeIdx < 0 {
			thinkParts = append(thinkParts, s[thinkStart:])
			break
		}
		thinkParts = append(thinkParts, s[thinkStart:closeIdx])
		i = skipSpace(s, closeIdx+closeLen)
	}
	return strings.Join(textParts, ""), strings.Join(thinkParts, "")
}

func SanitizeEmbeddedThinking(msg *Message) {
	if msg == nil || msg.Role != "assistant" {
		return
	}
	raw := ContentString(*msg)
	if raw == "" {
		return
	}
	text, embedded := SplitEmbeddedThinking(raw)
	if embedded == "" && text == raw {
		return
	}
	if text != "" {
		msg.Content = StringContent(text)
	} else {
		msg.Content = nil
	}
	existing := msg.GetReasoning()
	if embedded != "" {
		if existing != "" {
			existing += embedded
		} else {
			existing = embedded
		}
	}
	if existing != "" {
		msg.ReasoningContent = &existing
		msg.ReasoningDetails = nil
		msg.ReasoningPlain = nil
	}
}

type thinkStreamSplitter struct {
	inThink bool
	openTag string
	carry   strings.Builder
}

func (s *thinkStreamSplitter) feed(chunk string, emitText, emitThink func(string)) {
	data := s.carry.String() + chunk
	s.carry.Reset()
	if data == "" {
		return
	}

	maxHold := maxThinkTagLen() - 1
	i := 0
	for i < len(data) {
		if !s.inThink {
			openIdx, openLen := findThinkOpen(data, i)
			if openIdx < 0 {
				safeEnd := len(data)
				if safeEnd-i > maxHold {
					safeEnd = len(data) - maxHold
				}
				if safeEnd > i {
					emitText(data[i:safeEnd])
				}
				if safeEnd < len(data) {
					s.carry.WriteString(data[safeEnd:])
				}
				return
			}
			if openIdx > i {
				emitText(data[i:openIdx])
			}
			s.inThink = true
			s.openTag = data[openIdx : openIdx+openLen]
			i = openIdx + openLen
			continue
		}

		closeIdx, closeLen := findThinkCloseFrom(data, i, s.openTag)
		if closeIdx < 0 {
			safeEnd := len(data)
			if safeEnd-i > maxHold {
				safeEnd = len(data) - maxHold
			}
			if safeEnd > i {
				emitThink(data[i:safeEnd])
			}
			if safeEnd < len(data) {
				s.carry.WriteString(data[safeEnd:])
			}
			return
		}
		if closeIdx > i {
			emitThink(data[i:closeIdx])
		}
		s.inThink = false
		s.openTag = ""
		i = skipSpace(data, closeIdx+closeLen)
	}
}

func (s *thinkStreamSplitter) flush(emitText, emitThink func(string)) {
	if s.carry.Len() == 0 {
		return
	}
	rest := s.carry.String()
	s.carry.Reset()
	if s.inThink {
		emitThink(rest)
	} else {
		emitText(rest)
	}
}
