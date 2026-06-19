package tui

import "testing"

func TestStdinBufferLoneEscapeForwarded(t *testing.T) {
	var got [][]byte
	buf := newStdinBuffer(func(seq []byte) {
		got = append(got, append([]byte(nil), seq...))
	})

	buf.process([]byte{escByte})
	if len(got) != 1 || len(got[0]) != 1 || got[0][0] != escByte {
		t.Fatalf("expected lone ESC forwarded, got %v", got)
	}
}

func TestStdinBufferKittyEscapeBuffered(t *testing.T) {
	var got [][]byte
	buf := newStdinBuffer(func(seq []byte) {
		got = append(got, append([]byte(nil), seq...))
	})

	seq := []byte("\x1b[27~")
	buf.process(seq)
	if len(got) != 1 || string(got[0]) != string(seq) {
		t.Fatalf("expected kitty escape as one sequence, got %v", got)
	}
}

func TestStdinBufferLoneEscapeThenKeyReader(t *testing.T) {
	var forwarded []byte
	buf := newStdinBuffer(func(seq []byte) {
		forwarded = append(forwarded, seq...)
	})
	kr := newKeyReader()

	buf.process([]byte{escByte})
	keys, needsFlush := kr.feed(forwarded)
	if len(keys) != 0 {
		t.Fatalf("expected no immediate keys, got %v", keys)
	}
	if !needsFlush {
		t.Fatal("expected keyReader flush for lone ESC")
	}
	flushed := kr.flushPending()
	if len(flushed) != 1 || flushed[0].action != keyEscape {
		t.Fatalf("expected escape after flush, got %v", flushed)
	}
}
