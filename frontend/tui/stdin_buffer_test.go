package tui

import (
	"bytes"
	"testing"
	"time"
)

func TestStdinBufferCompleteSequences(t *testing.T) {
	var got [][]byte
	var pastes []string
	buf := newStdinBuffer(func(seq []byte) {
		got = append(got, seq)
	}, func(paste string) {
		pastes = append(pastes, paste)
	})

	// Process regular chars and CSI sequences
	buf.Process([]byte("a\x1b[A"))
	if len(got) != 2 || string(got[0]) != "a" || string(got[1]) != "\x1b[A" {
		t.Errorf("expected 'a' and '\\x1b[A', got %q", got)
	}
}

func TestStdinBufferBracketedPaste(t *testing.T) {
	var got [][]byte
	var pastes []string
	buf := newStdinBuffer(func(seq []byte) {
		got = append(got, seq)
	}, func(paste string) {
		pastes = append(pastes, paste)
	})

	buf.Process([]byte("\x1b[200~hello\nworld\x1b[201~"))
	if len(got) != 0 {
		t.Errorf("expected no key sequences during paste, got %q", got)
	}
	if len(pastes) != 1 || pastes[0] != "hello\nworld" {
		t.Errorf("expected paste 'hello\\nworld', got %q", pastes)
	}
}

func TestStdinBufferTimeoutFlush(t *testing.T) {
	var got [][]byte
	var pastes []string
	buf := newStdinBuffer(func(seq []byte) {
		got = append(got, seq)
	}, func(paste string) {
		pastes = append(pastes, paste)
	})

	// Process partial ESC
	buf.Process([]byte{0x1b})
	if len(got) != 0 {
		t.Errorf("expected ESC buffered, got %q", got)
	}

	// Wait for timeout to signal flushCh
	select {
	case <-buf.flushCh:
		flushed := buf.Flush()
		if len(flushed) != 1 || !bytes.Equal(flushed[0], []byte{0x1b}) {
			t.Errorf("expected flushed ESC, got %q", flushed)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("timeout signal not received")
	}
}

func TestExtractCompleteSequencesConcatenatedWezTermEsc(t *testing.T) {
	// WezTerm concatenates Esc and release sequence: \x1b\x1b[27;...u
	input := []byte("\x1b\x1b[27;1;27u")
	res := extractCompleteSequences(input)
	if len(res.sequences) != 2 || string(res.sequences[0]) != "\x1b" || string(res.sequences[1]) != "\x1b[27;1;27u" {
		t.Errorf("expected separate ESC and Kitty escape sequence, got %q", res.sequences)
	}
}
