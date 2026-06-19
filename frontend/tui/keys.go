package tui

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

type keyAction int

const (
	keyNone keyAction = iota
	keyEnter
	keyBackspace
	keyDelete
	keyLeft
	keyRight
	keyUp
	keyDown
	keyTab
	keyShiftTab
	keyCtrlT
	keyCtrlO
	keyCtrlBackspace
	keyEscape
	keyCtrlC
	keyCtrlD
	keyHome
	keyEnd
	keyRune
	keyPaste
	keyCtrlV
	keyCtrlShiftV
	keyWordLeft
	keyWordRight
	keyLineStart
	keyLineEnd
	keyDeleteWordBackward
	keyDeleteWordForward
	keyDeleteToLineStart
	keyDeleteToLineEnd
	keyUndo
)

type parsedKey struct {
	action keyAction
	r      rune
	paste  string
}

const (
	bracketedPasteStart = "\x1b[200~"
	bracketedPasteEnd   = "\x1b[201~"
)

// Global Kitty Protocol State
var kittyProtocolActive int32 // 0 for false, 1 for true

func SetKittyProtocolActive(active bool) {
	if active {
		atomic.StoreInt32(&kittyProtocolActive, 1)
	} else {
		atomic.StoreInt32(&kittyProtocolActive, 0)
	}
}

func IsKittyProtocolActive() bool {
	return atomic.LoadInt32(&kittyProtocolActive) == 1
}

// Modifiers bitmask matching Flame's keys.ts
const (
	modShift = 1
	modAlt   = 2
	modCtrl  = 4
	modSuper = 8
)

const lockMask = 64 + 128 // Caps Lock + Num Lock

var symbolKeys = map[string]bool{
	"`": true, "-": true, "=": true, "[": true, "]": true, "\\": true, ";": true, "'": true,
	",": true, ".": true, "/": true, "!": true, "@": true, "#": true, "$": true, "%": true,
	"^": true, "&": true, "*": true, "(": true, ")": true, "_": true, "+": true, "|": true,
	"~": true, "{": true, "}": true, ":": true, "<": true, ">": true, "?": true,
}

var kittyFunctionalKeyEquivalents = map[int]int{
	57399: 48,  // KP_0 -> 0
	57400: 49,  // KP_1 -> 1
	57401: 50,  // KP_2 -> 2
	57402: 51,  // KP_3 -> 3
	57403: 52,  // KP_4 -> 4
	57404: 53,  // KP_5 -> 5
	57405: 54,  // KP_6 -> 6
	57406: 55,  // KP_7 -> 7
	57407: 56,  // KP_8 -> 8
	57408: 57,  // KP_9 -> 9
	57409: 46,  // KP_DECIMAL -> .
	57410: 47,  // KP_DIVIDE -> /
	57411: 42,  // KP_MULTIPLY -> *
	57412: 45,  // KP_SUBTRACT -> -
	57413: 43,  // KP_ADD -> +
	57415: 61,  // KP_EQUAL -> =
	57416: 44,  // KP_SEPARATOR -> ,
	57417: -4,  // ARROW_CODEPOINTS.left -> left
	57418: -3,  // ARROW_CODEPOINTS.right -> right
	57419: -1,  // ARROW_CODEPOINTS.up -> up
	57420: -2,  // ARROW_CODEPOINTS.down -> down
	57421: -12, // FUNCTIONAL_CODEPOINTS.pageUp -> pageUp
	57422: -13, // FUNCTIONAL_CODEPOINTS.pageDown -> pageDown
	57423: -14, // FUNCTIONAL_CODEPOINTS.home -> home
	57424: -15, // FUNCTIONAL_CODEPOINTS.end -> end
	57425: -11, // FUNCTIONAL_CODEPOINTS.insert -> insert
	57426: -10, // FUNCTIONAL_CODEPOINTS.delete -> delete
}

var legacyKeySequences = map[string][]string{
	"up":       {"\x1b[A", "\x1bOA"},
	"down":     {"\x1b[B", "\x1bOB"},
	"right":    {"\x1b[C", "\x1bOC"},
	"left":     {"\x1b[D", "\x1bOD"},
	"home":     {"\x1b[H", "\x1bOH", "\x1b[1~", "\x1b[7~"},
	"end":      {"\x1b[F", "\x1bOF", "\x1b[4~", "\x1b[8~"},
	"insert":   {"\x1b[2~"},
	"delete":   {"\x1b[3~"},
	"pageUp":   {"\x1b[5~", "\x1b[[5~"},
	"pageDown": {"\x1b[6~", "\x1b[[6~"},
	"clear":    {"\x1b[E", "\x1bOE"},
	"f1":       {"\x1bOP", "\x1b[11~", "\x1b[[A"},
	"f2":       {"\x1bOQ", "\x1b[12~", "\x1b[[B"},
	"f3":       {"\x1bOR", "\x1b[13~", "\x1b[[C"},
	"f4":       {"\x1bOS", "\x1b[14~", "\x1b[[D"},
	"f5":       {"\x1b[15~", "\x1b[[E"},
	"f6":       {"\x1b[17~"},
	"f7":       {"\x1b[18~"},
	"f8":       {"\x1b[19~"},
	"f9":       {"\x1b[20~"},
	"f10":      {"\x1b[21~"},
	"f11":      {"\x1b[23~"},
	"f12":      {"\x1b[24~"},
}

var legacyShiftSequences = map[string][]string{
	"up":       {"\x1b[a"},
	"down":     {"\x1b[b"},
	"right":    {"\x1b[c"},
	"left":     {"\x1b[d"},
	"clear":    {"\x1b[e"},
	"insert":   {"\x1b[2$"},
	"delete":   {"\x1b[3$"},
	"pageUp":   {"\x1b[5$"},
	"pageDown": {"\x1b[6$"},
	"home":     {"\x1b[7$"},
	"end":      {"\x1b[8$"},
}

var legacyCtrlSequences = map[string][]string{
	"up":       {"\x1bOa"},
	"down":     {"\x1bOb"},
	"right":    {"\x1bOc"},
	"left":     {"\x1bOd"},
	"clear":    {"\x1bOe"},
	"insert":   {"\x1b[2^"},
	"delete":   {"\x1b[3^"},
	"pageUp":   {"\x1b[5^"},
	"pageDown": {"\x1b[6^"},
	"home":     {"\x1b[7^"},
	"end":      {"\x1b[8^"},
}

var legacySequenceKeyIds = map[string]string{
	"\x1bOA":    "up",
	"\x1bOB":    "down",
	"\x1bOC":    "right",
	"\x1bOD":    "left",
	"\x1bOH":    "home",
	"\x1bOF":    "end",
	"\x1b[E":    "clear",
	"\x1bOE":    "clear",
	"\x1bOe":    "ctrl+clear",
	"\x1b[e":    "shift+clear",
	"\x1b[2~":   "insert",
	"\x1b[2$":   "shift+insert",
	"\x1b[2^":   "ctrl+insert",
	"\x1b[3$":   "shift+delete",
	"\x1b[3^":   "ctrl+delete",
	"\x1b[[5~":  "pageUp",
	"\x1b[[6~":  "pageDown",
	"\x1b[a":    "shift+up",
	"\x1b[b":    "shift+down",
	"\x1b[c":    "shift+right",
	"\x1b[d":    "shift+left",
	"\x1bOa":    "ctrl+up",
	"\x1bOb":    "ctrl+down",
	"\x1bOc":    "ctrl+right",
	"\x1bOd":    "ctrl+left",
	"\x1b[5$":   "shift+pageUp",
	"\x1b[6$":   "shift+pageDown",
	"\x1b[7$":   "shift+home",
	"\x1b[8$":   "shift+end",
	"\x1b[5^":   "ctrl+pageUp",
	"\x1b[6^":   "ctrl+pageDown",
	"\x1b[7^":   "ctrl+home",
	"\x1b[8^":   "ctrl+end",
	"\x1bOP":    "f1",
	"\x1bOQ":    "f2",
	"\x1bOR":    "f3",
	"\x1bOS":    "f4",
	"\x1b[11~":  "f1",
	"\x1b[12~":  "f2",
	"\x1b[13~":  "f3",
	"\x1b[14~":  "f4",
	"\x1b[[A":   "f1",
	"\x1b[[B":   "f2",
	"\x1b[[C":   "f3",
	"\x1b[[D":   "f4",
	"\x1b[[E":   "f5",
	"\x1b[15~":  "f5",
	"\x1b[17~":  "f6",
	"\x1b[18~":  "f7",
	"\x1b[19~":  "f8",
	"\x1b[20~":  "f9",
	"\x1b[21~":  "f10",
	"\x1b[23~":  "f11",
	"\x1b[24~":  "f12",
	"\x1bb":     "alt+left",
	"\x1bf":     "alt+right",
	"\x1bp":     "alt+up",
	"\x1bn":     "alt+down",
}

func normalizeKittyFunctionalCodepoint(codepoint int) int {
	if val, exists := kittyFunctionalKeyEquivalents[codepoint]; exists {
		return val
	}
	return codepoint
}

func normalizeShiftedLetterIdentityCodepoint(codepoint int, modifier int) int {
	effectiveModifier := modifier & ^lockMask
	if (effectiveModifier&modShift) != 0 && codepoint >= 65 && codepoint <= 90 {
		return codepoint + 32
	}
	return codepoint
}

func matchesLegacySequence(data string, sequences []string) bool {
	for _, seq := range sequences {
		if data == seq {
			return true
		}
	}
	return false
}

func matchesLegacyModifierSequence(data string, key string, modifier int) bool {
	if modifier == modShift {
		return matchesLegacySequence(data, legacyShiftSequences[key])
	}
	if modifier == modCtrl {
		return matchesLegacySequence(data, legacyCtrlSequences[key])
	}
	return false
}

type ParsedKittySequence struct {
	Codepoint     int
	ShiftedKey    *int
	BaseLayoutKey *int
	Modifier      int
	EventType     string // "press", "repeat", "release"
}

var csiURegex = regexp.MustCompile(`^\x1b\[(\d+)(?::(\d*))?(?::(\d+))?(?:;(\d+))?(?::(\d+))?u$`)
var arrowRegex = regexp.MustCompile(`^\x1b\[1;(\d+)(?::(\d+))?([ABCD])$`)
var funcRegex = regexp.MustCompile(`^\x1b\[(\d+)(?:;(\d+))?(?::(\d+))?~$`)
var homeEndRegex = regexp.MustCompile(`^\x1b\[1;(\d+)(?::(\d+))?([HF])$`)

func parseEventType(eventTypeStr string) string {
	if eventTypeStr == "" {
		return "press"
	}
	eventType, err := strconv.Atoi(eventTypeStr)
	if err != nil {
		return "press"
	}
	if eventType == 2 {
		return "repeat"
	}
	if eventType == 3 {
		return "release"
	}
	return "press"
}

func parseKittySequence(data string) *ParsedKittySequence {
	if match := csiURegex.FindStringSubmatch(data); match != nil {
		codepoint, _ := strconv.Atoi(match[1])
		var shiftedKey *int
		if match[2] != "" {
			val, err := strconv.Atoi(match[2])
			if err == nil {
				shiftedKey = &val
			}
		}
		var baseLayoutKey *int
		if match[3] != "" {
			val, err := strconv.Atoi(match[3])
			if err == nil {
				baseLayoutKey = &val
			}
		}
		modifier := 1
		if match[4] != "" {
			modifier, _ = strconv.Atoi(match[4])
		}
		eventType := parseEventType(match[5])
		return &ParsedKittySequence{
			Codepoint:     codepoint,
			ShiftedKey:    shiftedKey,
			BaseLayoutKey: baseLayoutKey,
			Modifier:      modifier - 1,
			EventType:     eventType,
		}
	}

	if match := arrowRegex.FindStringSubmatch(data); match != nil {
		modifier, _ := strconv.Atoi(match[1])
		eventType := parseEventType(match[2])
		arrowCodes := map[string]int{"A": -1, "B": -2, "C": -3, "D": -4}
		return &ParsedKittySequence{
			Codepoint: arrowCodes[match[3]],
			Modifier:  modifier - 1,
			EventType: eventType,
		}
	}

	if match := funcRegex.FindStringSubmatch(data); match != nil {
		keyNum, _ := strconv.Atoi(match[1])
		modifier := 1
		if match[2] != "" {
			modifier, _ = strconv.Atoi(match[2])
		}
		eventType := parseEventType(match[3])
		funcCodes := map[int]int{
			2: -11, // insert
			3: -10, // delete
			5: -12, // pageUp
			6: -13, // pageDown
			7: -14, // home
			8: -15, // end
		}
		if codepoint, exists := funcCodes[keyNum]; exists {
			return &ParsedKittySequence{
				Codepoint: codepoint,
				Modifier:  modifier - 1,
				EventType: eventType,
			}
		}
	}

	if match := homeEndRegex.FindStringSubmatch(data); match != nil {
		modifier, _ := strconv.Atoi(match[1])
		eventType := parseEventType(match[2])
		codepoint := -14 // home
		if match[3] == "F" {
			codepoint = -15 // end
		}
		return &ParsedKittySequence{
			Codepoint: codepoint,
			Modifier:  modifier - 1,
			EventType: eventType,
		}
	}

	return nil
}

var modifyOtherKeysRegex = regexp.MustCompile(`^\x1b\[27;(\d+);(\d+)~$`)

type ParsedModifyOtherKeysSequence struct {
	Codepoint int
	Modifier  int
}

func parseModifyOtherKeysSequence(data string) *ParsedModifyOtherKeysSequence {
	match := modifyOtherKeysRegex.FindStringSubmatch(data)
	if match == nil {
		return nil
	}
	modValue, _ := strconv.Atoi(match[1])
	codepoint, _ := strconv.Atoi(match[2])
	return &ParsedModifyOtherKeysSequence{
		Codepoint: codepoint,
		Modifier:  modValue - 1,
	}
}

func matchesKittySequence(data string, expectedCodepoint int, expectedModifier int) bool {
	parsed := parseKittySequence(data)
	if parsed == nil {
		return false
	}
	actualMod := parsed.Modifier & ^lockMask
	expectedMod := expectedModifier & ^lockMask
	if actualMod != expectedMod {
		return false
	}

	normalizedCodepoint := normalizeShiftedLetterIdentityCodepoint(
		normalizeKittyFunctionalCodepoint(parsed.Codepoint),
		parsed.Modifier,
	)
	normalizedExpectedCodepoint := normalizeShiftedLetterIdentityCodepoint(
		normalizeKittyFunctionalCodepoint(expectedCodepoint),
		expectedModifier,
	)

	if normalizedCodepoint == normalizedExpectedCodepoint {
		return true
	}

	if parsed.BaseLayoutKey != nil && *parsed.BaseLayoutKey == expectedCodepoint {
		cp := normalizedCodepoint
		isLatinLetter := cp >= 97 && cp <= 122
		isKnownSymbol := symbolKeys[string(rune(cp))]
		if !isLatinLetter && !isKnownSymbol {
			return true
		}
	}
	return false
}

func matchesModifyOtherKeys(data string, expectedKeycode int, expectedModifier int) bool {
	parsed := parseModifyOtherKeysSequence(data)
	if parsed == nil {
		return false
	}
	return parsed.Codepoint == expectedKeycode && parsed.Modifier == expectedModifier
}

func isWindowsTerminalSession() bool {
	return os.Getenv("WT_SESSION") != "" &&
		os.Getenv("SSH_CONNECTION") == "" &&
		os.Getenv("SSH_CLIENT") == "" &&
		os.Getenv("SSH_TTY") == ""
}

func matchesRawBackspace(data string, expectedModifier int) bool {
	if data == "\x7f" {
		return expectedModifier == 0
	}
	if data != "\x08" {
		return false
	}
	if isWindowsTerminalSession() {
		return expectedModifier == modCtrl
	}
	return expectedModifier == 0
}

func rawCtrlChar(key string) string {
	if len(key) != 1 {
		return ""
	}
	char := strings.ToLower(key)
	code := int(char[0])
	if (code >= 97 && code <= 122) || char == "[" || char == "\\" || char == "]" || char == "_" {
		return string(rune(code & 0x1f))
	}
	if char == "-" {
		return string(rune(31))
	}
	return ""
}

func matchesPrintableModifyOtherKeys(data string, expectedKeycode int, expectedModifier int) bool {
	if expectedModifier == 0 {
		return false
	}
	parsed := parseModifyOtherKeysSequence(data)
	if parsed == nil || parsed.Modifier != expectedModifier {
		return false
	}
	return normalizeShiftedLetterIdentityCodepoint(parsed.Codepoint, parsed.Modifier) ==
		normalizeShiftedLetterIdentityCodepoint(expectedKeycode, expectedModifier)
}

type ParsedKeyId struct {
	Key   string
	Ctrl  bool
	Shift bool
	Alt   bool
	Super bool
}

func parseKeyId(keyId string) *ParsedKeyId {
	parts := strings.Split(strings.ToLower(keyId), "+")
	if len(parts) == 0 {
		return nil
	}
	key := parts[len(parts)-1]
	if key == "" {
		return nil
	}
	hasCtrl := false
	hasShift := false
	hasAlt := false
	hasSuper := false
	for i := 0; i < len(parts)-1; i++ {
		switch parts[i] {
		case "ctrl":
			hasCtrl = true
		case "shift":
			hasShift = true
		case "alt":
			hasAlt = true
		case "super":
			hasSuper = true
		}
	}
	return &ParsedKeyId{
		Key:   key,
		Ctrl:  hasCtrl,
		Shift: hasShift,
		Alt:   hasAlt,
		Super: hasSuper,
	}
}

func matchesKey(data string, keyId string) bool {
	parsed := parseKeyId(keyId)
	if parsed == nil {
		return false
	}
	key := parsed.Key
	modifier := 0
	if parsed.Shift {
		modifier |= modShift
	}
	if parsed.Alt {
		modifier |= modAlt
	}
	if parsed.Ctrl {
		modifier |= modCtrl
	}
	if parsed.Super {
		modifier |= modSuper
	}

	kittyActive := IsKittyProtocolActive()

	switch key {
	case "escape", "esc":
		if modifier != 0 {
			return false
		}
		return data == "\x1b" ||
			matchesKittySequence(data, 27, 0) ||
			matchesModifyOtherKeys(data, 27, 0)

	case "space":
		if !kittyActive {
			if modifier == modCtrl && data == "\x00" {
				return true
			}
			if modifier == modAlt && data == "\x1b " {
				return true
			}
		}
		if modifier == 0 {
			return data == " " ||
				matchesKittySequence(data, 32, 0) ||
				matchesModifyOtherKeys(data, 32, 0)
		}
		return matchesKittySequence(data, 32, modifier) ||
			matchesModifyOtherKeys(data, 32, modifier)

	case "tab":
		if modifier == modShift {
			return data == "\x1b[Z" ||
				matchesKittySequence(data, 9, modShift) ||
				matchesModifyOtherKeys(data, 9, modShift)
		}
		if modifier == 0 {
			return data == "\t" || matchesKittySequence(data, 9, 0)
		}
		return matchesKittySequence(data, 9, modifier) ||
			matchesModifyOtherKeys(data, 9, modifier)

	case "enter", "return":
		if modifier == modShift {
			if matchesKittySequence(data, 13, modShift) ||
				matchesKittySequence(data, 57414, modShift) {
				return true
			}
			if matchesModifyOtherKeys(data, 13, modShift) {
				return true
			}
			if kittyActive {
				return data == "\x1b\r" || data == "\n"
			}
			return false
		}
		if modifier == modAlt {
			if matchesKittySequence(data, 13, modAlt) ||
				matchesKittySequence(data, 57414, modAlt) {
				return true
			}
			if matchesModifyOtherKeys(data, 13, modAlt) {
				return true
			}
			if !kittyActive {
				return data == "\x1b\r"
			}
			return false
		}
		if modifier == 0 {
			return data == "\r" ||
				(!kittyActive && data == "\n") ||
				data == "\x1bOM" ||
				matchesKittySequence(data, 13, 0) ||
				matchesKittySequence(data, 57414, 0)
		}
		return matchesKittySequence(data, 13, modifier) ||
			matchesKittySequence(data, 57414, modifier) ||
			matchesModifyOtherKeys(data, 13, modifier)

	case "backspace":
		if modifier == modAlt {
			if data == "\x1b\x7f" || data == "\x1b\b" {
				return true
			}
			return matchesKittySequence(data, 127, modAlt) ||
				matchesModifyOtherKeys(data, 127, modAlt)
		}
		if modifier == modCtrl {
			if matchesRawBackspace(data, modCtrl) {
				return true
			}
			return matchesKittySequence(data, 127, modCtrl) ||
				matchesModifyOtherKeys(data, 127, modCtrl)
		}
		if modifier == 0 {
			return matchesRawBackspace(data, 0) ||
				matchesKittySequence(data, 127, 0) ||
				matchesModifyOtherKeys(data, 127, 0)
		}
		return matchesKittySequence(data, 127, modifier) ||
			matchesModifyOtherKeys(data, 127, modifier)

	case "insert":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["insert"]) ||
				matchesKittySequence(data, -11, 0)
		}
		if matchesLegacyModifierSequence(data, "insert", modifier) {
			return true
		}
		return matchesKittySequence(data, -11, modifier)

	case "delete":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["delete"]) ||
				matchesKittySequence(data, -10, 0)
		}
		if matchesLegacyModifierSequence(data, "delete", modifier) {
			return true
		}
		return matchesKittySequence(data, -10, modifier)

	case "clear":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["clear"])
		}
		return matchesLegacyModifierSequence(data, "clear", modifier)

	case "home":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["home"]) ||
				matchesKittySequence(data, -14, 0)
		}
		if matchesLegacyModifierSequence(data, "home", modifier) {
			return true
		}
		return matchesKittySequence(data, -14, modifier)

	case "end":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["end"]) ||
				matchesKittySequence(data, -15, 0)
		}
		if matchesLegacyModifierSequence(data, "end", modifier) {
			return true
		}
		return matchesKittySequence(data, -15, modifier)

	case "pageup":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["pageUp"]) ||
				matchesKittySequence(data, -12, 0)
		}
		if matchesLegacyModifierSequence(data, "pageUp", modifier) {
			return true
		}
		return matchesKittySequence(data, -12, modifier)

	case "pagedown":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["pageDown"]) ||
				matchesKittySequence(data, -13, 0)
		}
		if matchesLegacyModifierSequence(data, "pageDown", modifier) {
			return true
		}
		return matchesKittySequence(data, -13, modifier)

	case "up":
		if modifier == modAlt {
			return data == "\x1bp" || matchesKittySequence(data, -1, modAlt)
		}
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["up"]) ||
				matchesKittySequence(data, -1, 0)
		}
		if matchesLegacyModifierSequence(data, "up", modifier) {
			return true
		}
		return matchesKittySequence(data, -1, modifier)

	case "down":
		if modifier == modAlt {
			return data == "\x1bn" || matchesKittySequence(data, -2, modAlt)
		}
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["down"]) ||
				matchesKittySequence(data, -2, 0)
		}
		if matchesLegacyModifierSequence(data, "down", modifier) {
			return true
		}
		return matchesKittySequence(data, -2, modifier)

	case "left":
		if modifier == modAlt {
			return data == "\x1b[1;3D" ||
				(!kittyActive && data == "\x1bB") ||
				data == "\x1bb" ||
				matchesKittySequence(data, -4, modAlt)
		}
		if modifier == modCtrl {
			return data == "\x1b[1;5D" ||
				matchesLegacyModifierSequence(data, "left", modCtrl) ||
				matchesKittySequence(data, -4, modCtrl)
		}
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["left"]) ||
				matchesKittySequence(data, -4, 0)
		}
		if matchesLegacyModifierSequence(data, "left", modifier) {
			return true
		}
		return matchesKittySequence(data, -4, modifier)

	case "right":
		if modifier == modAlt {
			return data == "\x1b[1;3C" ||
				(!kittyActive && data == "\x1bF") ||
				data == "\x1bf" ||
				matchesKittySequence(data, -3, modAlt)
		}
		if modifier == modCtrl {
			return data == "\x1b[1;5C" ||
				matchesLegacyModifierSequence(data, "right", modCtrl) ||
				matchesKittySequence(data, -3, modCtrl)
		}
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["right"]) ||
				matchesKittySequence(data, -3, 0)
		}
		if matchesLegacyModifierSequence(data, "right", modifier) {
			return true
		}
		return matchesKittySequence(data, -3, modifier)

	case "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12":
		if modifier != 0 {
			return false
		}
		return matchesLegacySequence(data, legacyKeySequences[key])
	}

	// Handle single letter/digit keys and symbols
	runes := []rune(key)
	if len(runes) == 1 && ((key >= "a" && key <= "z") || (key >= "0" && key <= "9") || symbolKeys[key]) {
		codepoint := int(runes[0])
		rawCtrl := rawCtrlChar(key)
		isLetter := key >= "a" && key <= "z"
		isDigit := key >= "0" && key <= "9"

		if modifier == modCtrl+modAlt && !kittyActive && rawCtrl != "" {
			if data == "\x1b"+rawCtrl {
				return true
			}
		}

		if modifier == modAlt && !kittyActive && (isLetter || isDigit) {
			if data == "\x1b"+key {
				return true
			}
		}

		if modifier == modCtrl {
			if rawCtrl != "" && data == rawCtrl {
				return true
			}
			return matchesKittySequence(data, codepoint, modCtrl) ||
				matchesPrintableModifyOtherKeys(data, codepoint, modCtrl)
		}

		if modifier == modShift+modCtrl {
			return matchesKittySequence(data, codepoint, modShift+modCtrl) ||
				matchesPrintableModifyOtherKeys(data, codepoint, modShift+modCtrl)
		}

		if modifier == modShift {
			if isLetter && data == strings.ToUpper(key) {
				return true
			}
			return matchesKittySequence(data, codepoint, modShift) ||
				matchesPrintableModifyOtherKeys(data, codepoint, modShift)
		}

		if modifier != 0 {
			return matchesKittySequence(data, codepoint, modifier) ||
				matchesPrintableModifyOtherKeys(data, codepoint, modifier)
		}

		return data == key || matchesKittySequence(data, codepoint, 0)
	}

	return false
}

func formatKeyNameWithModifiers(keyName string, modifier int) string {
	effectiveMod := modifier & ^lockMask
	supportedModifierMask := modShift | modAlt | modCtrl | modSuper
	if (effectiveMod & ^supportedModifierMask) != 0 {
		return ""
	}
	var mods []string
	if (effectiveMod & modShift) != 0 {
		mods = append(mods, "shift")
	}
	if (effectiveMod & modCtrl) != 0 {
		mods = append(mods, "ctrl")
	}
	if (effectiveMod & modAlt) != 0 {
		mods = append(mods, "alt")
	}
	if (effectiveMod & modSuper) != 0 {
		mods = append(mods, "super")
	}
	if len(mods) > 0 {
		return strings.Join(mods, "+") + "+" + keyName
	}
	return keyName
}

func formatParsedKey(codepoint int, modifier int, baseLayoutKey *int) string {
	normalizedCodepoint := normalizeKittyFunctionalCodepoint(codepoint)
	identityCodepoint := normalizeShiftedLetterIdentityCodepoint(normalizedCodepoint, modifier)

	isLatinLetter := identityCodepoint >= 97 && identityCodepoint <= 122
	isDigit := identityCodepoint >= 48 && identityCodepoint <= 57
	isKnownSymbol := symbolKeys[string(rune(identityCodepoint))]

	effectiveCodepoint := identityCodepoint
	if !isLatinLetter && !isDigit && !isKnownSymbol && baseLayoutKey != nil {
		effectiveCodepoint = *baseLayoutKey
	}

	var keyName string
	switch effectiveCodepoint {
	case 27:
		keyName = "escape"
	case 9:
		keyName = "tab"
	case 13, 57414:
		keyName = "enter"
	case 32:
		keyName = "space"
	case 127:
		keyName = "backspace"
	case -10:
		keyName = "delete"
	case -11:
		keyName = "insert"
	case -14:
		keyName = "home"
	case -15:
		keyName = "end"
	case -12:
		keyName = "pageUp"
	case -13:
		keyName = "pageDown"
	case -1:
		keyName = "up"
	case -2:
		keyName = "down"
	case -4:
		keyName = "left"
	case -3:
		keyName = "right"
	default:
		if effectiveCodepoint >= 48 && effectiveCodepoint <= 57 {
			keyName = string(rune(effectiveCodepoint))
		} else if effectiveCodepoint >= 97 && effectiveCodepoint <= 122 {
			keyName = string(rune(effectiveCodepoint))
		} else if symbolKeys[string(rune(effectiveCodepoint))] {
			keyName = string(rune(effectiveCodepoint))
		}
	}

	if keyName == "" {
		return ""
	}
	return formatKeyNameWithModifiers(keyName, modifier)
}

func parseKey(data string) string {
	kitty := parseKittySequence(data)
	if kitty != nil {
		return formatParsedKey(kitty.Codepoint, kitty.Modifier, kitty.BaseLayoutKey)
	}

	modifyOtherKeys := parseModifyOtherKeysSequence(data)
	if modifyOtherKeys != nil {
		return formatParsedKey(modifyOtherKeys.Codepoint, modifyOtherKeys.Modifier, nil)
	}

	kittyActive := IsKittyProtocolActive()

	// Mode-aware legacy sequences
	if kittyActive {
		if data == "\x1b\r" || data == "\n" {
			return "shift+enter"
		}
	}

	if val, exists := legacySequenceKeyIds[data]; exists {
		return val
	}

	// Legacy sequences
	if data == "\x1b" {
		return "escape"
	}
	if data == "\x1c" {
		return "ctrl+\\"
	}
	if data == "\x1d" {
		return "ctrl+]"
	}
	if data == "\x1f" {
		return "ctrl+-"
	}
	if data == "\x1b\x1b" {
		return "ctrl+alt+["
	}
	if data == "\x1b\x1c" {
		return "ctrl+alt+\\"
	}
	if data == "\x1b\x1d" {
		return "ctrl+alt+]"
	}
	if data == "\x1b\x1f" {
		return "ctrl+alt+-"
	}
	if data == "\t" {
		return "tab"
	}
	if data == "\r" || (!kittyActive && data == "\n") || data == "\x1bOM" {
		return "enter"
	}
	if data == "\x00" {
		return "ctrl+space"
	}
	if data == " " {
		return "space"
	}
	if data == "\x7f" {
		return "backspace"
	}
	if data == "\x08" {
		if isWindowsTerminalSession() {
			return "ctrl+backspace"
		}
		return "backspace"
	}
	if data == "\x1b[Z" {
		return "shift+tab"
	}
	if !kittyActive && data == "\x1b\r" {
		return "alt+enter"
	}
	if !kittyActive && data == "\x1b " {
		return "alt+space"
	}
	if data == "\x1b\x7f" || data == "\x1b\b" {
		return "alt+backspace"
	}
	if !kittyActive && data == "\x1bB" {
		return "alt+left"
	}
	if !kittyActive && data == "\x1bF" {
		return "alt+right"
	}
	if !kittyActive && len(data) == 2 && data[0] == '\x1b' {
		code := int(data[1])
		if code >= 1 && code <= 26 {
			return "ctrl+alt+" + string(rune(code+96))
		}
		if (code >= 97 && code <= 122) || (code >= 48 && code <= 57) {
			return "alt+" + string(rune(code))
		}
	}
	if data == "\x1b[A" {
		return "up"
	}
	if data == "\x1b[B" {
		return "down"
	}
	if data == "\x1b[C" {
		return "right"
	}
	if data == "\x1b[D" {
		return "left"
	}
	if data == "\x1b[H" || data == "\x1bOH" {
		return "home"
	}
	if data == "\x1b[F" || data == "\x1bOF" {
		return "end"
	}
	if data == "\x1b[3~" {
		return "delete"
	}
	if data == "\x1b[5~" {
		return "pageUp"
	}
	if data == "\x1b[6~" {
		return "pageDown"
	}

	// Raw Ctrl+letter
	if len(data) == 1 {
		code := int(data[0])
		if code >= 1 && code <= 26 {
			return "ctrl+" + string(rune(code+96))
		}
		if code >= 32 && code <= 126 {
			return data
		}
	}

	return ""
}

func decodeKittyPrintable(data string) (string, bool) {
	parsed := parseKittySequence(data)
	if parsed == nil {
		return "", false
	}
	if !csiURegex.MatchString(data) {
		return "", false
	}

	allowedModifiers := modShift | lockMask
	if (parsed.Modifier & ^allowedModifiers) != 0 {
		return "", false
	}
	if (parsed.Modifier & (modAlt | modCtrl)) != 0 {
		return "", false
	}

	effectiveCodepoint := parsed.Codepoint
	if (parsed.Modifier&modShift) != 0 && parsed.ShiftedKey != nil {
		effectiveCodepoint = *parsed.ShiftedKey
	}
	effectiveCodepoint = normalizeKittyFunctionalCodepoint(effectiveCodepoint)

	if effectiveCodepoint < 32 {
		return "", false
	}

	return string(rune(effectiveCodepoint)), true
}

func decodeModifyOtherKeysPrintable(data string) (string, bool) {
	parsed := parseModifyOtherKeysSequence(data)
	if parsed == nil {
		return "", false
	}
	modifier := parsed.Modifier & ^lockMask
	if (modifier & ^modShift) != 0 {
		return "", false
	}
	if parsed.Codepoint < 32 {
		return "", false
	}
	return string(rune(parsed.Codepoint)), true
}

func decodePrintableKey(data string) (string, bool) {
	if val, ok := decodeKittyPrintable(data); ok {
		return val, true
	}
	return decodeModifyOtherKeysPrintable(data)
}

func SeqToParsedKey(seq string) parsedKey {
	if strings.HasPrefix(seq, bracketedPasteStart) && strings.HasSuffix(seq, bracketedPasteEnd) {
		content := seq[len(bracketedPasteStart) : len(seq)-len(bracketedPasteEnd)]
		return parsedKey{action: keyPaste, paste: content}
	}

	if isKeyRelease(seq) {
		return parsedKey{action: keyNone}
	}

	keyId := parseKey(seq)
	if keyId == "" {
		return parsedKey{action: keyNone}
	}

	switch keyId {
	case "ctrl+c":
		return parsedKey{action: keyCtrlC}
	case "ctrl+d":
		return parsedKey{action: keyCtrlD}
	case "ctrl+o":
		return parsedKey{action: keyCtrlO}
	case "ctrl+t":
		return parsedKey{action: keyCtrlT}
	case "ctrl+v":
		return parsedKey{action: keyCtrlV}
	case "ctrl+shift+v", "shift+ctrl+v":
		return parsedKey{action: keyCtrlShiftV}
	case "enter":
		return parsedKey{action: keyEnter}
	case "backspace":
		return parsedKey{action: keyBackspace}
	case "delete":
		return parsedKey{action: keyDelete}
	case "tab":
		return parsedKey{action: keyTab}
	case "shift+tab":
		return parsedKey{action: keyShiftTab}
	case "escape":
		return parsedKey{action: keyEscape}
	case "up":
		return parsedKey{action: keyUp}
	case "down":
		return parsedKey{action: keyDown}
	case "left", "ctrl+b":
		return parsedKey{action: keyLeft}
	case "right", "ctrl+f":
		return parsedKey{action: keyRight}
	case "alt+left", "ctrl+left", "alt+b":
		return parsedKey{action: keyWordLeft}
	case "alt+right", "ctrl+right", "alt+f":
		return parsedKey{action: keyWordRight}
	case "home", "ctrl+a":
		return parsedKey{action: keyLineStart}
	case "end", "ctrl+e":
		return parsedKey{action: keyLineEnd}
	case "ctrl+w", "alt+backspace", "ctrl+backspace":
		return parsedKey{action: keyDeleteWordBackward}
	case "alt+d", "alt+delete":
		return parsedKey{action: keyDeleteWordForward}
	case "ctrl+u":
		return parsedKey{action: keyDeleteToLineStart}
	case "ctrl+k":
		return parsedKey{action: keyDeleteToLineEnd}
	case "ctrl+-":
		return parsedKey{action: keyUndo}
	case "space", "shift+space":
		return parsedKey{action: keyRune, r: ' '}
	}

	runes := []rune(keyId)
	if len(runes) == 1 {
		return parsedKey{action: keyRune, r: runes[0]}
	}

	if printable, ok := decodePrintableKey(seq); ok && printable != "" {
		prunes := []rune(printable)
		if len(prunes) == 1 {
			return parsedKey{action: keyRune, r: prunes[0]}
		}
	}

	return parsedKey{action: keyNone}
}

func isKeyRelease(data string) bool {
	if strings.Contains(data, "\x1b[200~") {
		return false
	}
	if strings.Contains(data, ":3u") ||
		strings.Contains(data, ":3~") ||
		strings.Contains(data, ":3A") ||
		strings.Contains(data, ":3B") ||
		strings.Contains(data, ":3C") ||
		strings.Contains(data, ":3D") ||
		strings.Contains(data, ":3H") ||
		strings.Contains(data, ":3F") {
		return true
	}
	return false
}

func isKeyRepeat(data string) bool {
	if strings.Contains(data, "\x1b[200~") {
		return false
	}
	if strings.Contains(data, ":2u") ||
		strings.Contains(data, ":2~") ||
		strings.Contains(data, ":2A") ||
		strings.Contains(data, ":2B") ||
		strings.Contains(data, ":2C") ||
		strings.Contains(data, ":2D") ||
		strings.Contains(data, ":2H") ||
		strings.Contains(data, ":2F") {
		return true
	}
	return false
}
