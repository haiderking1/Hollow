package markdown

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/term"
)

// Foot identifies itself in tertiary DA as "FOOT" (hex 464f4f54).
// See https://codeberg.org/dnkl/foot#programmatically-checking-if-running-in-foot
const footTertiaryDA = "464f4f54"

// Kitty graphics query response token when the terminal supports the protocol.
// See https://sw.kovidgoyal.net/kitty/graphics-protocol/
const kittyGraphicsOK = "_Gi=31;OK"

// imageProbeQuery asks for Foot tertiary DA, Kitty graphics support, then DA1.
// Terminals ignore sequences they do not implement; DA1 acts as a read sentinel.
const imageProbeQuery = "\033P!|?\033\\\033_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\033\\\033[c"

var (
	capsLocked      bool
	cellSizeReportRe = regexp.MustCompile(`\x1b\[6;(\d+);(\d+)t`)
)

// InitTerminalCapabilities probes the connected terminal when env-based
// detection did not find a native image protocol. Call once before raw mode.
func InitTerminalCapabilities(fd int) {
	if capsLocked || !term.IsTerminal(fd) {
		return
	}
	capsLocked = true

	base := detectCapabilities()
	if base.Images == ImageNone {
		if proto := probeImageProtocol(fd); proto != ImageNone {
			base.Images = proto
			base.TrueColor = true
			base.Hyperlinks = true
		}
	}

	lockCapabilities(base)
}

func lockCapabilities(c Capabilities) {
	locked := c
	capabilitiesFn = func() Capabilities { return locked }
}

func probeImageProtocol(fd int) ImageProtocol {
	tty := os.NewFile(uintptr(fd), "tty")
	if tty == nil {
		return ImageNone
	}
	defer tty.Close()

	if _, err := tty.WriteString(imageProbeQuery); err != nil {
		return ImageNone
	}

	deadline := time.Now().Add(250 * time.Millisecond)
	buf := make([]byte, 256)
	var out []byte
	for time.Now().Before(deadline) {
		_ = tty.SetReadDeadline(deadline)
		n, err := tty.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
			switch {
			case bytes.Contains(out, []byte(footTertiaryDA)):
				return ImageSixel
			case bytes.Contains(out, []byte("P>|foot(")):
				return ImageSixel
			case bytes.Contains(out, []byte(kittyGraphicsOK)):
				return ImageKitty
			}
		}
		if err != nil {
			break
		}
	}
	return ImageNone
}

// QueryCellDimensions asks the terminal for cell pixel size (CSI 16 t).
// The response CSI 6 ; height ; width t is swallowed by HandleTerminalResponse.
func QueryCellDimensions(w interface{ Write(string) }) {
	if currentCapabilities().Images == ImageNone {
		return
	}
	w.Write("\x1b[16t")
}

// HandleTerminalResponse consumes terminal query replies that must not reach
// the key handler (e.g. cell-size reports). Returns true when seq was handled.
func HandleTerminalResponse(seq []byte) bool {
	if dims := parseCellSizeReport(seq); dims != nil {
		SetCellDimensions(*dims)
		return true
	}
	return false
}

func parseCellSizeReport(data []byte) *CellDimensions {
	m := cellSizeReportRe.FindSubmatch(data)
	if len(m) != 3 || len(m[0]) != len(data) {
		return nil
	}
	heightPx, err1 := strconv.Atoi(string(m[1]))
	widthPx, err2 := strconv.Atoi(string(m[2]))
	if err1 != nil || err2 != nil || heightPx <= 0 || widthPx <= 0 {
		return nil
	}
	return &CellDimensions{WidthPx: widthPx, HeightPx: heightPx}
}
