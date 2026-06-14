package markdown

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

type ImageProtocol string

const (
	ImageKitty  ImageProtocol = "kitty"
	ImageITerm2 ImageProtocol = "iterm2"
	ImageSixel  ImageProtocol = "sixel"
	ImageNone   ImageProtocol = ""
)

const (
	kittyPrefix  = "\x1b_G"
	iterm2Prefix = "\x1b]1337;File="

	// ImageReservedRow holds vertical space for direct-placement graphics.
	// The image sequence is emitted on the last row with cursor-up so sixel
	// or kitty output is not wiped by incremental EL on spacer lines below.
	ImageReservedRow = "\x1b[0m"
)

// CellDimensions holds terminal cell size in pixels.
type CellDimensions struct {
	WidthPx  int
	HeightPx int
}

var cellDimensions = CellDimensions{WidthPx: 9, HeightPx: 18}

// SetCellDimensions updates the assumed terminal cell size for image scaling.
func SetCellDimensions(d CellDimensions) {
	if d.WidthPx > 0 && d.HeightPx > 0 {
		cellDimensions = d
	}
}

// GetCellDimensions returns the current assumed terminal cell size.
func GetCellDimensions() CellDimensions {
	return cellDimensions
}

// IsImageReservedRow reports a buffer line reserved for direct-placement height.
func IsImageReservedRow(line string) bool {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	return line[i:] == ImageReservedRow
}

// IsImageLayoutRow reports lines that must not be wrapped, truncated, or padded.
func IsImageLayoutRow(line string) bool {
	return IsImageLine(line) || IsImageReservedRow(line)
}

// IsImageLine reports whether a rendered line contains inline terminal image data.
func IsImageLine(line string) bool {
	if strings.HasPrefix(line, kittyPrefix) || strings.HasPrefix(line, iterm2Prefix) {
		return true
	}
	if strings.Contains(line, kittyPrefix) || strings.Contains(line, iterm2Prefix) {
		return true
	}
	if isSixelLine(line) {
		return true
	}
	return isBlockImageLine(line)
}

type ImageDimensions struct {
	WidthPx  int
	HeightPx int
}

type imageCellSize struct {
	Columns int
	Rows    int
}

func allocateImageID() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 1
	}
	id := binary.BigEndian.Uint32(b[:])
	if id == 0 {
		return 1
	}
	return id
}

func encodeKitty(base64Data string, columns, rows int, imageID uint32, moveCursor bool) string {
	const chunkSize = 4096
	params := []string{"a=T", "f=100", "q=2"}
	if !moveCursor {
		params = append(params, "C=1")
	}
	if columns > 0 {
		params = append(params, fmt.Sprintf("c=%d", columns))
	}
	if rows > 0 {
		params = append(params, fmt.Sprintf("r=%d", rows))
	}
	if imageID > 0 {
		params = append(params, fmt.Sprintf("i=%d", imageID))
	}

	if len(base64Data) <= chunkSize {
		return kittyPrefix + strings.Join(params, ",") + ";" + base64Data + "\x1b\\"
	}

	var chunks []string
	offset := 0
	first := true
	for offset < len(base64Data) {
		end := offset + chunkSize
		if end > len(base64Data) {
			end = len(base64Data)
		}
		chunk := base64Data[offset:end]
		last := end >= len(base64Data)
		switch {
		case first:
			chunks = append(chunks, kittyPrefix+strings.Join(params, ",")+",m=1;"+chunk+"\x1b\\")
			first = false
		case last:
			chunks = append(chunks, kittyPrefix+"m=0;"+chunk+"\x1b\\")
		default:
			chunks = append(chunks, kittyPrefix+"m=1;"+chunk+"\x1b\\")
		}
		offset = end
	}
	return strings.Join(chunks, "")
}

func encodeITerm2(base64Data string, widthCells int, name string) string {
	params := []string{"inline=1", fmt.Sprintf("width=%d", widthCells), "height=auto", "preserveAspectRatio=1"}
	if name != "" {
		params = append(params, "name="+base64EncodeString(name))
	}
	return iterm2Prefix + strings.Join(params, ";") + ":" + base64Data + "\x07"
}

func calculateImageCellSize(dim ImageDimensions, maxWidthCells int, maxHeightCells *int) imageCellSize {
	cellW := max(1, cellDimensions.WidthPx)
	cellH := max(1, cellDimensions.HeightPx)
	imageW := max(1, dim.WidthPx)
	imageH := max(1, dim.HeightPx)

	var maxColumns, maxRows *int
	if maxWidthCells > 0 {
		v := max(1, maxWidthCells)
		maxColumns = &v
	}
	if maxHeightCells != nil && *maxHeightCells > 0 {
		v := max(1, *maxHeightCells)
		maxRows = &v
	}

	if maxColumns == nil && maxRows == nil {
		columns := max(1, int(float64(imageW)/float64(cellW)+0.999999))
		rows := max(1, int(float64(imageH)/float64(cellH)+0.999999))
		return imageCellSize{Columns: columns, Rows: rows}
	}

	maxWidthPx := math.MaxFloat64
	maxHeightPx := math.MaxFloat64
	if maxColumns != nil {
		maxWidthPx = float64(*maxColumns * cellW)
	}
	if maxRows != nil {
		maxHeightPx = float64(*maxRows * cellH)
	}
	scale := min(maxWidthPx/float64(imageW), maxHeightPx/float64(imageH))
	fittedWidthPx := float64(imageW) * scale
	fittedHeightPx := float64(imageH) * scale

	columns := max(1, int(fittedWidthPx/float64(cellW)))
	rows := max(1, int(fittedHeightPx/float64(cellH)+0.999999))
	if maxColumns != nil && columns > *maxColumns {
		columns = *maxColumns
	}
	if maxRows != nil && rows > *maxRows {
		rows = *maxRows
	}
	return imageCellSize{Columns: columns, Rows: rows}
}

// layoutDirectPlacementImage returns rows of buffer lines for cursor-up image
// placement (oh-my-pi Image component pattern).
func layoutDirectPlacementImage(rows int, sequence string) []string {
	if rows < 1 {
		rows = 1
	}
	if rows == 1 {
		return []string{sequence}
	}
	lines := make([]string, rows)
	for i := 0; i < rows-1; i++ {
		lines[i] = ImageReservedRow
	}
	moveUp := fmt.Sprintf("\x1b[%dA", rows-1)
	lines[rows-1] = moveUp + sequence
	return lines
}

func renderImageSequence(base64Data string, mime string, dims ImageDimensions, maxWidthCells int, alt string) []string {
	caps := currentCapabilities()

	maxHeight := max(1, (maxWidthCells*cellDimensions.WidthPx)/max(1, cellDimensions.HeightPx))
	size := calculateImageCellSize(dims, maxWidthCells, &maxHeight)

	switch caps.Images {
	case ImageKitty:
		seq := encodeKitty(base64Data, size.Columns, size.Rows, allocateImageID(), false)
		return layoutDirectPlacementImage(size.Rows, seq)
	case ImageITerm2:
		seq := encodeITerm2(base64Data, size.Columns, alt)
		return layoutDirectPlacementImage(size.Rows, seq)
	case ImageSixel:
		if seq, ok := encodeImageSixel(base64Data, dims, maxWidthCells); ok {
			return layoutDirectPlacementImage(size.Rows, seq)
		}
	}

	if caps.TrueColor {
		if lines := renderImageBlocks(base64Data, dims, maxWidthCells); len(lines) > 0 {
			return lines
		}
	}
	return nil
}

func imageFallbackLabel(mime string, dims *ImageDimensions, alt, url string) string {
	if mime != "" || dims != nil {
		parts := []string{}
		if alt != "" {
			parts = append(parts, alt)
		}
		if mime != "" {
			parts = append(parts, "["+mime+"]")
		}
		if dims != nil {
			parts = append(parts, fmt.Sprintf("%dx%d", dims.WidthPx, dims.HeightPx))
		}
		if len(parts) > 0 {
			return "[Image: " + strings.Join(parts, " ") + "]"
		}
	}
	return imageFallback(alt, url)
}

func base64EncodeString(s string) string {
	return encodeBase64([]byte(s))
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
