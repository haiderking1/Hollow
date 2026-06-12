package tui

import "bytes"

const escByte = 0x1b

// stdinBuffer accumulates stdin chunks into complete escape sequences (Flame StdinBuffer).
type stdinBuffer struct {
	buf   []byte
	onSeq func([]byte)
}

func newStdinBuffer(onSeq func([]byte)) *stdinBuffer {
	return &stdinBuffer{onSeq: onSeq}
}

func (b *stdinBuffer) process(data []byte) {
	if len(data) == 0 && len(b.buf) == 0 {
		return
	}
	b.buf = append(b.buf, data...)
	for {
		seq, n, ok := nextInputSequence(b.buf)
		if !ok {
			return
		}
		b.buf = b.buf[n:]
		if len(seq) > 0 {
			b.onSeq(seq)
		}
	}
}

func nextInputSequence(buf []byte) (seq []byte, n int, ok bool) {
	if len(buf) == 0 {
		return nil, 0, false
	}
	if buf[0] != escByte {
		return buf[:1], 1, true
	}
	if len(buf) == 1 {
		return nil, 0, false
	}

	switch buf[1] {
	case '[':
		if len(buf) >= 3 && buf[2] == 'M' {
			if len(buf) < 6 {
				return nil, 0, false
			}
			return buf[:6], 6, true
		}
		if bytes.HasPrefix(buf, []byte("\x1b[200~")) {
			end := bytes.Index(buf, []byte("\x1b[201~"))
			if end == -1 {
				return nil, 0, false
			}
			return buf[:end+6], end + 6, true
		}
		for i := 2; i < len(buf); i++ {
			c := buf[i]
			if c >= 0x40 && c <= 0x7e {
				if i >= 3 && buf[2] == '<' && (c == 'M' || c == 'm') && !sgrMouseComplete(buf[2:i+1]) {
					return nil, 0, false
				}
				return buf[:i+1], i + 1, true
			}
		}
		return nil, 0, false
	case 'M':
		if len(buf) < 5 {
			return nil, 0, false
		}
		return buf[:5], 5, true
	case 'O':
		if len(buf) < 3 {
			return nil, 0, false
		}
		return buf[:3], 3, true
	default:
		return buf[:2], 2, true
	}
}

func sgrMouseComplete(payload []byte) bool {
	if len(payload) < 2 || payload[0] != '<' {
		return true
	}
	last := payload[len(payload)-1]
	if last != 'M' && last != 'm' {
		return false
	}
	parts := bytes.Split(payload[1:len(payload)-1], []byte{';'})
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}
