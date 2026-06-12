package tui

import "testing"

func TestKeyReaderLoneEscape(t *testing.T) {
	kr := newKeyReader()

	keys, needsFlush := kr.feed([]byte{27})
	if len(keys) != 0 {
		t.Fatalf("expected no immediate keys, got %v", keys)
	}
	if !needsFlush {
		t.Fatal("expected flush timer for lone ESC")
	}

	flushed := kr.flushPending()
	if len(flushed) != 1 || flushed[0].action != keyEscape {
		t.Fatalf("expected single escape flush, got %v", flushed)
	}
}

func TestKeyReaderDiscardsX10Mouse(t *testing.T) {
	kr := newKeyReader()
	// ESC [ M Cb Cx Cy
	seq := []byte{27, '[', 'M', 'C', 'X', '0'}
	keys, needsFlush := kr.feed(seq)
	if needsFlush || len(keys) != 0 {
		t.Fatalf("expected mouse discarded, keys=%v flush=%v", keys, needsFlush)
	}
	if len(kr.buf) != 0 {
		t.Fatalf("buffer not drained: %q", kr.buf)
	}
}

func TestKeyReaderDiscardsOrphanMouseWithoutESC(t *testing.T) {
	kr := newKeyReader()
	seq := []byte("[MCX0[MCY0")
	keys, _ := kr.feed(seq)
	if len(keys) != 0 {
		t.Fatalf("expected orphan mouse discarded, got %v", keys)
	}
}

func TestKeyReaderDiscardsSGRMouseWheel(t *testing.T) {
	kr := newKeyReader()
	// scroll-up wheel event
	seq := []byte("\x1b[<64;12;5M")
	keys, _ := kr.feed(seq)
	if len(keys) != 0 {
		t.Fatalf("expected wheel discarded, got %v", keys)
	}
}

func TestKeyReaderEscapeAndArrow(t *testing.T) {
	kr := newKeyReader()

	keys, needsFlush := kr.feed([]byte{27, '[', 'A'})
	if needsFlush {
		t.Fatal("complete sequence should not need flush")
	}
	if len(keys) != 1 || keys[0].action != keyUp {
		t.Fatalf("expected up arrow, got %v", keys)
	}
}

func TestKeyReaderKittyEscape(t *testing.T) {
	kr := newKeyReader()

	seq := []byte("\x1b[27;1;27~")
	keys, needsFlush := kr.feed(seq)
	if needsFlush {
		t.Fatal("kitty escape should not need flush")
	}
	if len(keys) != 1 || keys[0].action != keyEscape {
		t.Fatalf("expected escape, got %v", keys)
	}
}
