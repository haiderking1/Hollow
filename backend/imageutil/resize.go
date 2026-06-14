package imageutil

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

func init() {
	image.RegisterFormat("webp", "RIFF????WEBP", webp.Decode, webp.DecodeConfig)
}

const maxBytes = int(4.5 * 1024 * 1024)

// ResizeImage resizes the image data to fit within 2000x2000 and 4.5MB base64 size limit.
// If no resizing is needed, it returns the original data.
// Returns an error if the image cannot be resized below the limit.
func ResizeImage(originalData []byte, mimeType string) (resizedData []byte, width, height, originalWidth, originalHeight int, wasResized bool, err error) {
	decodedImg, _, err := image.Decode(bytes.NewReader(originalData))
	if err != nil {
		return nil, 0, 0, 0, 0, false, err
	}

	originalWidth = decodedImg.Bounds().Dx()
	originalHeight = decodedImg.Bounds().Dy()

	origBase64Len := (len(originalData) + 2) / 3 * 4
	if originalWidth <= 2000 && originalHeight <= 2000 && origBase64Len < maxBytes {
		return originalData, originalWidth, originalHeight, originalWidth, originalHeight, false, nil
	}

	// Calculate initial target dimensions respecting 2000x2000 limits
	targetWidth := originalWidth
	targetHeight := originalHeight

	if targetWidth > 2000 {
		targetHeight = int(math.Round(float64(targetHeight) * 2000.0 / float64(targetWidth)))
		targetWidth = 2000
	}
	if targetHeight > 2000 {
		targetWidth = int(math.Round(float64(targetWidth) * 2000.0 / float64(targetHeight)))
		targetHeight = 2000
	}
	if targetWidth < 1 {
		targetWidth = 1
	}
	if targetHeight < 1 {
		targetHeight = 1
	}

	currentWidth := targetWidth
	currentHeight := targetHeight

	for {
		// Create the target image and scale it using bilinear interpolation
		dst := image.NewRGBA(image.Rect(0, 0, currentWidth, currentHeight))
		xdraw.BiLinear.Scale(dst, dst.Bounds(), decodedImg, decodedImg.Bounds(), xdraw.Src, nil)

		// Try encoding to PNG
		var pngBuf bytes.Buffer
		var pngBase64 string
		if err := png.Encode(&pngBuf, dst); err == nil {
			pngBase64 = base64.StdEncoding.EncodeToString(pngBuf.Bytes())
		}

		// Try encoding to JPEG at quality 80
		var jpegBuf bytes.Buffer
		var jpegBase64 string
		if err := jpeg.Encode(&jpegBuf, dst, &jpeg.Options{Quality: 80}); err == nil {
			jpegBase64 = base64.StdEncoding.EncodeToString(jpegBuf.Bytes())
		}

		// Choose the best candidate that is under the limit
		var bestBase64 string
		var bestMime string

		if pngBase64 != "" && len(pngBase64) < maxBytes && jpegBase64 != "" && len(jpegBase64) < maxBytes {
			if len(pngBase64) < len(jpegBase64) {
				bestBase64 = pngBase64
				bestMime = "image/png"
			} else {
				bestBase64 = jpegBase64
				bestMime = "image/jpeg"
			}
		} else if pngBase64 != "" && len(pngBase64) < maxBytes {
			bestBase64 = pngBase64
			bestMime = "image/png"
		} else if jpegBase64 != "" && len(jpegBase64) < maxBytes {
			bestBase64 = jpegBase64
			bestMime = "image/jpeg"
		}

		if bestBase64 != "" {
			decodedBest, _ := base64.StdEncoding.DecodeString(bestBase64)
			_ = bestMime
			return decodedBest, currentWidth, currentHeight, originalWidth, originalHeight, true, nil
		}

		// Try progressively lower JPEG qualities
		foundJPEG := false
		for _, q := range []int{70, 55, 40} {
			var qBuf bytes.Buffer
			if err := jpeg.Encode(&qBuf, dst, &jpeg.Options{Quality: q}); err == nil {
				qBase64 := base64.StdEncoding.EncodeToString(qBuf.Bytes())
				if len(qBase64) < maxBytes {
					bestBase64 = qBase64
					foundJPEG = true
					break
				}
			}
		}

		if foundJPEG {
			decodedBest, _ := base64.StdEncoding.DecodeString(bestBase64)
			return decodedBest, currentWidth, currentHeight, originalWidth, originalHeight, true, nil
		}

		if currentWidth == 1 && currentHeight == 1 {
			break
		}

		nextW := currentWidth * 3 / 4
		if nextW < 1 {
			nextW = 1
		}
		nextH := currentHeight * 3 / 4
		if nextH < 1 {
			nextH = 1
		}

		if nextW == currentWidth && nextH == currentHeight {
			break
		}
		currentWidth = nextW
		currentHeight = nextH
	}

	return nil, 0, 0, 0, 0, false, errors.New("could not resize image below maxBytes limit")
}

func FormatDimensionNote(originalWidth, originalHeight, width, height int, wasResized bool) string {
	if !wasResized {
		return ""
	}
	scale := float64(originalWidth) / float64(width)
	return fmt.Sprintf("[Image: original %dx%d, displayed at %dx%d. Multiply coordinates by %.2f to map to original image.]", originalWidth, originalHeight, width, height, scale)
}
