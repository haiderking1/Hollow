package markdown

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/draw"
	"strings"

	"github.com/mattn/go-sixel"
	xdraw "golang.org/x/image/draw"
)

const (
	sixelPrefix    = "\x1bP0;0;8q"
	sixelPrefixP2T = "\x1bP0;1;8q" // P2=1: preserve terminal background for unset pixels
)

func isSixelLine(line string) bool {
	return strings.Contains(line, "\x1bP") && strings.Contains(line, "q")
}

// GetSixelLineMask marks lines belonging to a multi-line sixel payload.
func GetSixelLineMask(lines []string) []bool {
	mask := make([]bool, len(lines))
	inSequence := false
	for i, line := range lines {
		if strings.Contains(line, "\x1bP") && strings.Contains(line, "q") {
			inSequence = true
		}
		mask[i] = inSequence
		if inSequence && (strings.Contains(line, "\x1b\\") || strings.Contains(line, "\x07")) {
			inSequence = false
		}
	}
	return mask
}

// encodeImageSixel encodes an image as a DEC sixel sequence. Foot and several
// other terminals render images natively via sixel (not the Kitty protocol).
func encodeImageSixel(base64Data string, dims ImageDimensions, maxWidthCells int) (string, bool) {
	if maxWidthCells > 60 {
		maxWidthCells = 60
	}
	if maxWidthCells < 4 {
		maxWidthCells = 4
	}

	raw, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", false
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", false
	}

	cellW := max(1, cellDimensions.WidthPx)
	cellH := max(1, cellDimensions.HeightPx)
	maxHeight := max(1, (maxWidthCells*cellW)/max(1, cellH))
	size := calculateImageCellSize(dims, maxWidthCells, &maxHeight)

	targetW := max(1, size.Columns*cellW)
	targetH := max(1, size.Rows*cellH)
	scaled := resizeImageExact(src, targetW, targetH)

	var buf bytes.Buffer
	enc := sixel.NewEncoder(&buf)
	enc.Width = targetW
	enc.Height = targetH
	enc.Dither = false
	if err := enc.Encode(scaled); err != nil || buf.Len() == 0 {
		return "", false
	}

	seq := buf.String()
	// Foot: P2=0 fills unset pixels with the current ANSI background (often black).
	// P2=1 leaves the terminal background visible — matches icy_sixel defaults.
	seq = strings.Replace(seq, sixelPrefix, sixelPrefixP2T, 1)
	return seq, true
}

func resizeImageExact(src image.Image, w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return src
	}
	bounds := src.Bounds()
	if bounds.Dx() == w && bounds.Dy() == h {
		if _, ok := src.(*image.RGBA); ok {
			return src
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	return dst
}
