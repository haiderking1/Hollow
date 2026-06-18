package opencode

import (
	"strings"
	"testing"
)

func TestParseSSEBlockJoinsDataLines(t *testing.T) {
	block := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}"
	parsed := parseSSEBlock(block)
	if parsed.EventType != "response.output_text.delta" {
		t.Fatalf("event = %q", parsed.EventType)
	}
	if !strings.Contains(parsed.Data, "Hello") {
		t.Fatalf("data = %q", parsed.Data)
	}
}

func TestForEachSSEBlockLargePayload(t *testing.T) {
	payload := strings.Repeat("x", 2*1024*1024)
	input := "data: " + payload + "\n\n"
	var got string
	if err := forEachSSEBlock(strings.NewReader(input), func(block sseBlock) error {
		got = block.Data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(payload) {
		t.Fatalf("got len %d want %d", len(got), len(payload))
	}
}

func TestForEachSSEBlockDone(t *testing.T) {
	var done bool
	err := forEachSSEBlock(strings.NewReader("data: [DONE]\n\n"), func(block sseBlock) error {
		done = block.Done
		return ErrSSEDone
	})
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("expected done block")
	}
}
