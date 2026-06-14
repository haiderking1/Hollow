package markdown

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/draw"
)

const (
	blockUpperHalf = '▀'
	blockLowerHalf = '▄'
	maxBlockRows   = 24
)

// renderImageBlocks draws a downscaled preview using truecolor half-block
// characters. Works in terminals without Kitty/iTerm2 graphics (e.g. VS Code).
func renderImageBlocks(base64Data string, dims ImageDimensions, maxWidthCells int) []string {
	if maxWidthCells > 60 {
		maxWidthCells = 60
	}
	if maxWidthCells < 4 {
		maxWidthCells = 4
	}

	raw, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}

	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil
	}
	if dims.WidthPx > 0 && dims.HeightPx > 0 {
		srcW, srcH = dims.WidthPx, dims.HeightPx
	}

	cols := maxWidthCells
	rows := int(float64(cols)*float64(srcH)/float64(srcW) + 0.5)
	if rows < 1 {
		rows = 1
	}
	maxPixelRows := maxBlockRows * 2
	if rows > maxPixelRows {
		rows = maxPixelRows
		cols = int(float64(rows)*float64(srcW)/float64(srcH) + 0.5)
		if cols < 1 {
			cols = 1
		}
		if cols > maxWidthCells {
			cols = maxWidthCells
		}
	}

	dst := image.NewRGBA(image.Rect(0, 0, cols, rows))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

	out := make([]string, 0, (rows+1)/2)
	for y := 0; y < rows; y += 2 {
		var b strings.Builder
		for x := 0; x < cols; x++ {
			top := dst.RGBAAt(x, y)
			if y+1 >= rows {
				b.WriteString(trueColorFG(top))
				b.WriteRune(' ')
				b.WriteString(resetANSI)
				continue
			}
			bottom := dst.RGBAAt(x, y+1)
			if top.A < 16 && bottom.A < 16 {
				b.WriteString(" ")
				continue
			}
			b.WriteString(trueColorFG(top))
			b.WriteString(trueColorBG(bottom))
			b.WriteRune(blockUpperHalf)
			b.WriteString(resetANSI)
		}
		out = append(out, b.String())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const resetANSI = "\x1b[0m"

func trueColorFG(c color.RGBA) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B)
}

func trueColorBG(c color.RGBA) string {
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.R, c.G, c.B)
}

func isBlockImageLine(line string) bool {
	return strings.ContainsRune(line, blockUpperHalf) || strings.ContainsRune(line, blockLowerHalf)
}
