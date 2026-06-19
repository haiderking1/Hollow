package tui

import (
	"bytes"
	"regexp"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

type stdinBuffer struct {
	mu             sync.Mutex
	buf            []byte
	onSeq          func([]byte)
	onPaste        func(string)
	timeoutMs      time.Duration
	flushTimer     *time.Timer
	flushCh        chan struct{}
	
	pasteMode      bool
	pasteBuffer    []byte
	pendingKittyCP *int
}

func newStdinBuffer(onSeq func([]byte), onPaste func(string)) *stdinBuffer {
	sb := &stdinBuffer{
		onSeq:     onSeq,
		onPaste:   onPaste,
		timeoutMs: 10 * time.Millisecond,
		flushCh:   make(chan struct{}, 1),
	}
	return sb
}

func (sb *stdinBuffer) Process(data []byte) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	
	if sb.flushTimer != nil {
		sb.flushTimer.Stop()
		sb.flushTimer = nil
	}
	
	if len(data) == 0 && len(sb.buf) == 0 {
		sb.emitDataSequence(nil)
		return
	}
	
	sb.buf = append(sb.buf, data...)
	
	if sb.pasteMode {
		sb.pasteBuffer = append(sb.pasteBuffer, sb.buf...)
		sb.buf = nil
		
		endIndex := bytes.Index(sb.pasteBuffer, []byte(bracketedPasteEnd))
		if endIndex != -1 {
			pastedContent := sb.pasteBuffer[:endIndex]
			remaining := sb.pasteBuffer[endIndex+len(bracketedPasteEnd):]
			
			sb.pasteMode = false
			sb.pasteBuffer = nil
			sb.pendingKittyCP = nil
			
			sb.mu.Unlock()
			sb.onPaste(string(pastedContent))
			sb.mu.Lock()
			
			if len(remaining) > 0 {
				sb.mu.Unlock()
				sb.Process(remaining)
				sb.mu.Lock()
			}
		}
		return
	}
	
	startIndex := bytes.Index(sb.buf, []byte(bracketedPasteStart))
	if startIndex != -1 {
		if startIndex > 0 {
			beforePaste := sb.buf[:startIndex]
			result := extractCompleteSequences(beforePaste)
			for _, seq := range result.sequences {
				sb.emitDataSequence(seq)
			}
		}
		
		sb.pendingKittyCP = nil
		sb.pasteMode = true
		sb.pasteBuffer = sb.buf[startIndex+len(bracketedPasteStart):]
		sb.buf = nil
		
		endIndex := bytes.Index(sb.pasteBuffer, []byte(bracketedPasteEnd))
		if endIndex != -1 {
			pastedContent := sb.pasteBuffer[:endIndex]
			remaining := sb.pasteBuffer[endIndex+len(bracketedPasteEnd):]
			
			sb.pasteMode = false
			sb.pasteBuffer = nil
			sb.pendingKittyCP = nil
			
			sb.mu.Unlock()
			sb.onPaste(string(pastedContent))
			sb.mu.Lock()
			
			if len(remaining) > 0 {
				sb.mu.Unlock()
				sb.Process(remaining)
				sb.mu.Lock()
			}
		}
		return
	}
	
	result := extractCompleteSequences(sb.buf)
	sb.buf = result.remainder
	
	for _, seq := range result.sequences {
		sb.emitDataSequence(seq)
	}
	
	if len(sb.buf) > 0 {
		sb.flushTimer = time.AfterFunc(sb.timeoutMs, func() {
			select {
			case sb.flushCh <- struct{}{}:
			default:
			}
		})
	}
}

func (sb *stdinBuffer) emitDataSequence(sequence []byte) {
	if len(sequence) == 0 {
		sb.onSeq(nil)
		return
	}
	
	// Kitty double send deduplication
	var rawCodepoint *int
	r, size := utf8.DecodeRune(sequence)
	if r != utf8.RuneError && size == len(sequence) {
		cp := int(r)
		rawCodepoint = &cp
	}
	
	if rawCodepoint != nil && sb.pendingKittyCP != nil && *rawCodepoint == *sb.pendingKittyCP {
		sb.pendingKittyCP = nil
		return
	}
	
	sb.pendingKittyCP = parseUnmodifiedKittyPrintableCodepoint(string(sequence))
	sb.onSeq(sequence)
}

func (sb *stdinBuffer) Flush() [][]byte {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	
	if sb.flushTimer != nil {
		sb.flushTimer.Stop()
		sb.flushTimer = nil
	}
	
	if len(sb.buf) == 0 {
		return nil
	}
	
	flushed := [][]byte{sb.buf}
	sb.buf = nil
	sb.pendingKittyCP = nil
	return flushed
}

func (sb *stdinBuffer) Clear() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	
	if sb.flushTimer != nil {
		sb.flushTimer.Stop()
		sb.flushTimer = nil
	}
	sb.buf = nil
	sb.pasteMode = false
	sb.pasteBuffer = nil
	sb.pendingKittyCP = nil
}

type sequenceStatus int

const (
	statusComplete sequenceStatus = iota
	statusIncomplete
	statusNotEscape
)

func isCompleteSequence(data []byte) sequenceStatus {
	if len(data) == 0 || data[0] != 0x1b {
		return statusNotEscape
	}
	if len(data) == 1 {
		return statusIncomplete
	}
	
	afterEsc := data[1:]
	
	// CSI sequences: ESC [
	if afterEsc[0] == '[' {
		if len(afterEsc) >= 2 && afterEsc[1] == 'M' {
			if len(data) >= 6 {
				return statusComplete
			}
			return statusIncomplete
		}
		return isCompleteCsiSequence(data)
	}
	
	// OSC sequences: ESC ]
	if afterEsc[0] == ']' {
		return isCompleteOscSequence(data)
	}
	
	// DCS sequences: ESC P
	if afterEsc[0] == 'P' {
		return isCompleteDcsSequence(data)
	}
	
	// APC sequences: ESC _
	if afterEsc[0] == '_' {
		return isCompleteApcSequence(data)
	}
	
	// SS3 sequences: ESC O
	if afterEsc[0] == 'O' {
		if len(afterEsc) >= 2 {
			return statusComplete
		}
		return statusIncomplete
	}
	
	// Meta key sequences: ESC followed by a single character
	r, size := utf8.DecodeRune(afterEsc)
	if r != utf8.RuneError || size > 0 {
		if len(afterEsc) >= size {
			return statusComplete
		}
		return statusIncomplete
	}
	
	return statusComplete
}

var sgrMousePattern = regexp.MustCompile(`^<\d+;\d+;\d+[Mm]$`)

func isCompleteCsiSequence(data []byte) sequenceStatus {
	if len(data) < 3 {
		return statusIncomplete
	}
	
	payload := data[2:]
	lastChar := payload[len(payload)-1]
	
	if lastChar >= 0x40 && lastChar <= 0x7e {
		if len(payload) > 0 && payload[0] == '<' {
			if sgrMousePattern.Match(payload) {
				return statusComplete
			}
			if lastChar == 'M' || lastChar == 'm' {
				parts := bytes.Split(payload[1:len(payload)-1], []byte{';'})
				if len(parts) == 3 {
					allDigits := true
					for _, p := range parts {
						if len(p) == 0 {
							allDigits = false
							break
						}
						for _, b := range p {
							if b < '0' || b > '9' {
								allDigits = false
								break
							}
						}
					}
					if allDigits {
						return statusComplete
					}
				}
			}
			return statusIncomplete
		}
		return statusComplete
	}
	
	return statusIncomplete
}

func isCompleteOscSequence(data []byte) sequenceStatus {
	if len(data) < 3 {
		return statusIncomplete
	}
	if bytes.HasSuffix(data, []byte("\x1b\\")) || data[len(data)-1] == 0x07 {
		return statusComplete
	}
	return statusIncomplete
}

func isCompleteDcsSequence(data []byte) sequenceStatus {
	if len(data) < 3 {
		return statusIncomplete
	}
	if bytes.HasSuffix(data, []byte("\x1b\\")) {
		return statusComplete
	}
	return statusIncomplete
}

func isCompleteApcSequence(data []byte) sequenceStatus {
	if len(data) < 3 {
		return statusIncomplete
	}
	if bytes.HasSuffix(data, []byte("\x1b\\")) {
		return statusComplete
	}
	return statusIncomplete
}

type extractResult struct {
	sequences [][]byte
	remainder []byte
}

func extractCompleteSequences(buffer []byte) extractResult {
	var sequences [][]byte
	pos := 0
	
	for pos < len(buffer) {
		remaining := buffer[pos:]
		
		if remaining[0] == 0x1b { // ESC
			seqEnd := 1
			for seqEnd <= len(remaining) {
				candidate := remaining[:seqEnd]
				status := isCompleteSequence(candidate)
				
				if status == statusComplete {
					if len(candidate) == 2 && candidate[0] == 0x1b && candidate[1] == 0x1b {
						if seqEnd < len(remaining) {
							nextChar := remaining[seqEnd]
							if nextChar == '[' || nextChar == ']' || nextChar == 'O' || nextChar == 'P' || nextChar == '_' {
								sequences = append(sequences, []byte{0x1b})
								pos += 1
								goto outerLoop
							}
						}
					}
					sequences = append(sequences, candidate)
					pos += seqEnd
					goto outerLoop
				} else if status == statusIncomplete {
					seqEnd++
				} else {
					sequences = append(sequences, candidate)
					pos += seqEnd
					goto outerLoop
				}
			}
			
			if seqEnd > len(remaining) {
				return extractResult{
					sequences: sequences,
					remainder: remaining,
				}
			}
		} else {
			r, size := utf8.DecodeRune(remaining)
			if r == utf8.RuneError && size == 1 {
				sequences = append(sequences, remaining[:1])
				pos += 1
			} else {
				sequences = append(sequences, remaining[:size])
				pos += size
			}
		}
	outerLoop:
	}
	
	return extractResult{
		sequences: sequences,
		remainder: nil,
	}
}

var unmodifiedKittyRegex = regexp.MustCompile(`^\x1b\[(\d+)(?::\d*)?(?::\d+)?u$`)

func parseUnmodifiedKittyPrintableCodepoint(sequence string) *int {
	match := unmodifiedKittyRegex.FindStringSubmatch(sequence)
	if match == nil {
		return nil
	}
	cp, err := strconv.Atoi(match[1])
	if err == nil && cp >= 32 {
		return &cp
	}
	return nil
}
