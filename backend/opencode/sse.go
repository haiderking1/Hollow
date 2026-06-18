package opencode

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// ErrSSEDone signals the consumer should stop reading SSE data.
var ErrSSEDone = errors.New("sse done")

// sseBlock is one SSE event block (lines between blank-line delimiters).
type sseBlock struct {
	EventType string
	Data      string
	Done      bool
}

// forEachSSEBlock parses SSE like Flame packages/ai parseSSE: accumulate bytes,
// split on "\n\n", then extract data: (and optional event:) lines per block.
func forEachSSEBlock(r io.Reader, fn func(block sseBlock) error) error {
	br := bufio.NewReaderSize(r, 256*1024)
	var buf strings.Builder
	chunk := make([]byte, 32*1024)

	for {
		n, err := br.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			if err := drainSSEBuffer(&buf, fn); err != nil {
				return err
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	if buf.Len() > 0 {
		if err := consumeSSEBlock(buf.String(), fn); err != nil {
			return err
		}
	}
	return nil
}

func drainSSEBuffer(buf *strings.Builder, fn func(block sseBlock) error) error {
	s := buf.String()
	for {
		idx := strings.Index(s, "\n\n")
		if idx < 0 {
			buf.Reset()
			buf.WriteString(s)
			return nil
		}
		blockText := s[:idx]
		s = s[idx+2:]
		if err := consumeSSEBlock(blockText, fn); err != nil {
			return err
		}
	}
}

func consumeSSEBlock(blockText string, fn func(block sseBlock) error) error {
	blockText = strings.TrimSpace(blockText)
	if blockText == "" {
		return nil
	}
	parsed := parseSSEBlock(blockText)
	if parsed.Data == "" && !parsed.Done {
		return nil
	}
	if err := fn(parsed); err != nil {
		if errors.Is(err, ErrSSEDone) {
			return nil
		}
		return err
	}
	return nil
}

func parseSSEBlock(blockText string) sseBlock {
	var eventType string
	var dataLines []string
	for _, line := range strings.Split(blockText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	data := strings.Join(dataLines, "\n")
	return sseBlock{
		EventType: eventType,
		Data:      data,
		Done:      data == "[DONE]",
	}
}
