package tui

import "testing"

func TestSeqToParsedKey(t *testing.T) {
	cases := []struct {
		seq      string
		action   keyAction
		runeVal  rune
		pasteVal string
	}{
		{"\x03", keyCtrlC, 0, ""},
		{"\x1b[99;5u", keyCtrlC, 0, ""},
		{"\x04", keyCtrlD, 0, ""},
		{"\x1b[100;5u", keyCtrlD, 0, ""},
		{"\r", keyEnter, 0, ""},
		{"\x7f", keyBackspace, 0, ""},
		{"\x1b[Z", keyShiftTab, 0, ""},
		{"a", keyRune, 'a', ""},
		{" ", keyRune, ' ', ""},
		{"\x1b[32;2u", keyRune, ' ', ""},
		{"\x1b[200~hello\x1b[201~", keyPaste, 0, "hello"},
		// Emacs / Flame movement bindings
		{"\x01", keyLineStart, 0, ""}, // ctrl+a
		{"\x05", keyLineEnd, 0, ""},   // ctrl+e
		{"\x02", keyLeft, 0, ""},      // ctrl+b
		{"\x06", keyRight, 0, ""},     // ctrl+f
		{"\x1bb", keyWordLeft, 0, ""}, // alt+b
		{"\x1bf", keyWordRight, 0, ""}, // alt+f
		{"\x1b[1;5D", keyWordLeft, 0, ""}, // ctrl+left
		{"\x1b[1;5C", keyWordRight, 0, ""}, // ctrl+right
		// Emacs / Flame deletion & edit bindings
		{"\x17", keyDeleteWordBackward, 0, ""}, // ctrl+w
		{"\x1b\x7f", keyDeleteWordBackward, 0, ""}, // alt+backspace
		{"\x1b[127;5u", keyDeleteWordBackward, 0, ""}, // ctrl+backspace
		{"\x1bd", keyDeleteWordForward, 0, ""}, // alt+d
		{"\x1b[3;3~", keyDeleteWordForward, 0, ""}, // alt+delete
		{"\x15", keyDeleteToLineStart, 0, ""}, // ctrl+u
		{"\x0b", keyDeleteToLineEnd, 0, ""}, // ctrl+k
		{"\x1f", keyUndo, 0, ""}, // ctrl+-
	}

	for _, tc := range cases {
		pk := SeqToParsedKey(tc.seq)
		if pk.action != tc.action {
			t.Errorf("SeqToParsedKey(%q) expected action %v, got %v", tc.seq, tc.action, pk.action)
		}
		if tc.action == keyRune && pk.r != tc.runeVal {
			t.Errorf("SeqToParsedKey(%q) expected rune %q, got %q", tc.seq, tc.runeVal, pk.r)
		}
		if tc.action == keyPaste && pk.paste != tc.pasteVal {
			t.Errorf("SeqToParsedKey(%q) expected paste %q, got %q", tc.seq, tc.pasteVal, pk.paste)
		}
	}
}
