package tui

import (
	"os"
	"testing"
)

func TestMatchesKey(t *testing.T) {
	// 1. Kitty protocol with alternate keys (non-Latin layouts)
	t.Run("Cyrillic Ctrl+c with base layout key", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		cyrillicCtrlC := "\x1b[1089::99;5u"
		if !matchesKey(cyrillicCtrlC, "ctrl+c") {
			t.Errorf("expected %q to match ctrl+c", cyrillicCtrlC)
		}
	})

	t.Run("Cyrillic Ctrl+d with base layout key", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		cyrillicCtrlD := "\x1b[1074::100;5u"
		if !matchesKey(cyrillicCtrlD, "ctrl+d") {
			t.Errorf("expected %q to match ctrl+d", cyrillicCtrlD)
		}
	})

	t.Run("Cyrillic Ctrl+z with base layout key", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		cyrillicCtrlZ := "\x1b[1103::122;5u"
		if !matchesKey(cyrillicCtrlZ, "ctrl+z") {
			t.Errorf("expected %q to match ctrl+z", cyrillicCtrlZ)
		}
	})

	t.Run("Ctrl+Shift+p with base layout key", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		cyrillicCtrlShiftP := "\x1b[1079::112;6u"
		if !matchesKey(cyrillicCtrlShiftP, "ctrl+shift+p") {
			t.Errorf("expected %q to match ctrl+shift+p", cyrillicCtrlShiftP)
		}
	})

	t.Run("Direct codepoint when no base layout key", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		latinCtrlC := "\x1b[99;5u"
		if !matchesKey(latinCtrlC, "ctrl+c") {
			t.Errorf("expected %q to match ctrl+c", latinCtrlC)
		}
	})

	t.Run("super-modified Kitty bindings", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		cases := []struct {
			data  string
			keyId string
			match bool
		}{
			{"\x1b[107;9u", "super+k", true},
			{"\x1b[13;9u", "super+enter", true},
			{"\x1b[107;13u", "ctrl+super+k", true},
			{"\x1b[107;14u", "ctrl+shift+super+k", true},
			{"\x1b[107;13u", "super+k", false},
		}
		for _, tc := range cases {
			if matchesKey(tc.data, tc.keyId) != tc.match {
				t.Errorf("matchesKey(%q, %q) expected %v", tc.data, tc.keyId, tc.match)
			}
		}
	})

	t.Run("digit bindings via Kitty CSI-u", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		if !matchesKey("\x1b[49u", "1") {
			t.Error("expected \\x1b[49u to match 1")
		}
		if !matchesKey("\x1b[49;5u", "ctrl+1") {
			t.Error("expected \\x1b[49;5u to match ctrl+1")
		}
		if matchesKey("\x1b[49;5u", "ctrl+2") {
			t.Error("expected \\x1b[49;5u NOT to match ctrl+2")
		}
	})

	t.Run("normalize Kitty keypad functional keys", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		cases := []struct {
			data  string
			keyId string
		}{
			{"\x1b[57400u", "1"},
			{"\x1b[57410u", "/"},
			{"\x1b[57417u", "left"},
			{"\x1b[57426u", "delete"},
		}
		for _, tc := range cases {
			if !matchesKey(tc.data, tc.keyId) {
				t.Errorf("expected %q to match %q", tc.data, tc.keyId)
			}
		}
	})

	t.Run("shifted key in format", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		shiftedKey := "\x1b[99:67:99;2u"
		if !matchesKey(shiftedKey, "shift+c") {
			t.Errorf("expected %q to match shift+c", shiftedKey)
		}
	})

	t.Run("event type in format", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		releaseEvent := "\x1b[1089::99;5:3u"
		if !matchesKey(releaseEvent, "ctrl+c") {
			t.Errorf("expected %q to match ctrl+c", releaseEvent)
		}
	})

	t.Run("full format with shifted key, base key, and event type", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		fullFormat := "\x1b[1089:1057:99;6:2u"
		if !matchesKey(fullFormat, "ctrl+shift+c") {
			t.Errorf("expected %q to match ctrl+shift+c", fullFormat)
		}
	})

	t.Run("prefer codepoint for Latin letters in Dvorak remapping", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		dvorakCtrlK := "\x1b[107::118;5u"
		if !matchesKey(dvorakCtrlK, "ctrl+k") {
			t.Errorf("expected %q to match ctrl+k", dvorakCtrlK)
		}
		if matchesKey(dvorakCtrlK, "ctrl+v") {
			t.Errorf("expected %q NOT to match ctrl+v", dvorakCtrlK)
		}
	})

	t.Run("prefer codepoint for symbol keys in Dvorak remapping", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		dvorakCtrlSlash := "\x1b[47::91;5u"
		if !matchesKey(dvorakCtrlSlash, "ctrl+/") {
			t.Errorf("expected %q to match ctrl+/", dvorakCtrlSlash)
		}
		if matchesKey(dvorakCtrlSlash, "ctrl+[") {
			t.Errorf("expected %q NOT to match ctrl+[", dvorakCtrlSlash)
		}
	})

	t.Run("modifyOtherKeys matching", func(t *testing.T) {
		SetKittyProtocolActive(false)
		cases := []struct {
			data  string
			keyId string
		}{
			{"\x1b[27;5;99~", "ctrl+c"},
			{"\x1b[27;5;100~", "ctrl+d"},
			{"\x1b[27;5;122~", "ctrl+z"},
			{"\x1b[27;5;13~", "ctrl+enter"},
			{"\x1b[27;2;13~", "shift+enter"},
			{"\x1b[27;3;13~", "alt+enter"},
			{"\x1b[27;2;9~", "shift+tab"},
			{"\x1b[27;5;9~", "ctrl+tab"},
			{"\x1b[27;3;9~", "alt+tab"},
			{"\x1b[27;1;127~", "backspace"},
			{"\x1b[27;5;127~", "ctrl+backspace"},
			{"\x1b[27;3;127~", "alt+backspace"},
			{"\x1b[27;1;27~", "escape"},
			{"\x1b[27;1;32~", "space"},
			{"\x1b[27;5;32~", "ctrl+space"},
			{"\x1b[27;5;47~", "ctrl+/"},
			{"\x1b[27;5;49~", "ctrl+1"},
			{"\x1b[27;2;49~", "shift+1"},
			{"\x1b[27;2;69~", "shift+e"},
			{"\x1b[27;6;69~", "ctrl+shift+e"},
			{"\x1b[104;7u", "ctrl+alt+h"},
			{"\x1b[27;7;104~", "ctrl+alt+h"},
		}
		for _, tc := range cases {
			if !matchesKey(tc.data, tc.keyId) {
				t.Errorf("expected modifyOtherKeys %q to match %q", tc.data, tc.keyId)
			}
		}
	})

	t.Run("Legacy key matching", func(t *testing.T) {
		SetKittyProtocolActive(false)
		
		if !matchesKey("\x03", "ctrl+c") {
			t.Error("expected ASCII 3 to match ctrl+c")
		}
		if !matchesKey("\x04", "ctrl+d") {
			t.Error("expected ASCII 4 to match ctrl+d")
		}
		if !matchesKey("\x1b", "escape") {
			t.Error("expected escape byte to match escape")
		}
		if !matchesKey("\n", "enter") {
			t.Error("expected newline byte to match enter")
		}
		
		SetKittyProtocolActive(true)
		if !matchesKey("\n", "shift+enter") {
			t.Error("expected newline byte to match shift+enter when Kitty is active")
		}
		if matchesKey("\n", "enter") {
			t.Error("expected newline byte NOT to match enter when Kitty is active")
		}
		SetKittyProtocolActive(false)
		
		if !matchesKey("\x00", "ctrl+space") {
			t.Error("expected ASCII 0 to match ctrl+space")
		}
		if !matchesKey("\x1c", "ctrl+\\") {
			t.Error("expected ASCII 28 to match ctrl+\\")
		}
		if !matchesKey("\x1d", "ctrl+]") {
			t.Error("expected ASCII 29 to match ctrl+]")
		}
		if !matchesKey("\x1f", "ctrl+-") {
			t.Error("expected ASCII 31 to match ctrl+-")
		}
		if !matchesKey("\x1b\x1b", "ctrl+alt+[") {
			t.Error("expected ESC ESC to match ctrl+alt+[")
		}
		if !matchesKey("\x1b\x1c", "ctrl+alt+\\") {
			t.Error("expected ESC ASCII 28 to match ctrl+alt+\\")
		}
		if !matchesKey("\x1b\x1d", "ctrl+alt+]") {
			t.Error("expected ESC ASCII 29 to match ctrl+alt+]")
		}
		if !matchesKey("\x1b\x1f", "ctrl+alt+-") {
			t.Error("expected ESC ASCII 31 to match ctrl+alt+-")
		}
	})

	t.Run("Ambiguous Backspace behavior", func(t *testing.T) {
		SetKittyProtocolActive(false)
		
		// WT_SESSION unset
		os.Unsetenv("WT_SESSION")
		os.Unsetenv("SSH_CONNECTION")
		os.Unsetenv("SSH_CLIENT")
		os.Unsetenv("SSH_TTY")
		if !matchesKey("\x7f", "backspace") {
			t.Error("expected 0x7f to match backspace")
		}
		if matchesKey("\x7f", "ctrl+backspace") {
			t.Error("expected 0x7f NOT to match ctrl+backspace")
		}
		if !matchesKey("\x08", "backspace") {
			t.Error("expected 0x08 to match backspace when not WT")
		}
		if matchesKey("\x08", "ctrl+backspace") {
			t.Error("expected 0x08 NOT to match ctrl+backspace when not WT")
		}

		// WT_SESSION set (local WT)
		os.Setenv("WT_SESSION", "test-session")
		if !matchesKey("\x08", "ctrl+backspace") {
			t.Error("expected 0x08 to match ctrl+backspace in WT local")
		}
		if matchesKey("\x08", "backspace") {
			t.Error("expected 0x08 NOT to match backspace in WT local")
		}

		// WT_SESSION set (over SSH)
		os.Setenv("SSH_CONNECTION", "1 2 3 4")
		if matchesKey("\x08", "ctrl+backspace") {
			t.Error("expected 0x08 NOT to match ctrl+backspace over SSH")
		}
		if !matchesKey("\x08", "backspace") {
			t.Error("expected 0x08 to match backspace over SSH")
		}
		
		// Clean up
		os.Unsetenv("WT_SESSION")
		os.Unsetenv("SSH_CONNECTION")
	})
}

func TestParseKey(t *testing.T) {
	t.Run("Cyrillic base layout keys", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		cyrillicCtrlC := "\x1b[1089::99;5u"
		if parseKey(cyrillicCtrlC) != "ctrl+c" {
			t.Errorf("expected parse %q to be 'ctrl+c', got %q", cyrillicCtrlC, parseKey(cyrillicCtrlC))
		}
	})

	t.Run("Dvorak priority", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		dvorakCtrlK := "\x1b[107::118;5u"
		if parseKey(dvorakCtrlK) != "ctrl+k" {
			t.Errorf("expected parse %q to be 'ctrl+k', got %q", dvorakCtrlK, parseKey(dvorakCtrlK))
		}
	})

	t.Run("Keypad functional keys normalizations", func(t *testing.T) {
		SetKittyProtocolActive(true)
		defer SetKittyProtocolActive(false)
		
		cases := []struct {
			data     string
			expected string
		}{
			{"\x1b[57399u", "0"},
			{"\x1b[57409u", "."},
			{"\x1b[57413u", "+"},
			{"\x1b[57416u", ","},
			{"\x1b[57417u", "left"},
			{"\x1b[57418u", "right"},
			{"\x1b[57419u", "up"},
			{"\x1b[57420u", "down"},
			{"\x1b[57421u", "pageUp"},
			{"\x1b[57422u", "pageDown"},
			{"\x1b[57423u", "home"},
			{"\x1b[57424u", "end"},
			{"\x1b[57425u", "insert"},
			{"\x1b[57426u", "delete"},
		}
		for _, tc := range cases {
			if parseKey(tc.data) != tc.expected {
				t.Errorf("parseKey(%q) expected %q, got %q", tc.data, tc.expected, parseKey(tc.data))
			}
		}
	})
}

func TestDecodeKittyPrintable(t *testing.T) {
	cases := []struct {
		data     string
		expected string
		ok       bool
	}{
		{"\x1b[57399u", "0", true},
		{"\x1b[57400u", "1", true},
		{"\x1b[57409u", ".", true},
		{"\x1b[57410u", "/", true},
		{"\x1b[57411u", "*", true},
		{"\x1b[57412u", "-", true},
		{"\x1b[57413u", "+", true},
		{"\x1b[57415u", "=", true},
		{"\x1b[57416u", ",", true},
		{"\x1b[57417u", "", false},
	}
	for _, tc := range cases {
		val, ok := decodeKittyPrintable(tc.data)
		if ok != tc.ok || val != tc.expected {
			t.Errorf("decodeKittyPrintable(%q) expected (%q, %v), got (%q, %v)", tc.data, tc.expected, tc.ok, val, ok)
		}
	}
}

func TestDecodePrintableKey(t *testing.T) {
	cases := []struct {
		data     string
		expected string
		ok       bool
	}{
		{"\x1b[27;2;69~", "E", true},
		{"\x1b[27;2;196~", "Ä", true},
		{"\x1b[27;2;32~", " ", true},
		{"\x1b[27;2;13~", "", false},
		{"\x1b[27;6;69~", "", false},
	}
	for _, tc := range cases {
		val, ok := decodePrintableKey(tc.data)
		if ok != tc.ok || val != tc.expected {
			t.Errorf("decodePrintableKey(%q) expected (%q, %v), got (%q, %v)", tc.data, tc.expected, tc.ok, val, ok)
		}
	}
}
