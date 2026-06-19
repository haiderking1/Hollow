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
		{"\x1b[200~hello\x1b[201~", keyPaste, 0, "hello"},
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
