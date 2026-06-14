package imageutil

import (
	"encoding/base64"
	"encoding/binary"
	"testing"
)

const png1x1Base64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGD4DwABBAEAX+XDSwAAAABJRU5ErkJggg=="

func TestDetectSupportedImageMimeType(t *testing.T) {
	// 1. Valid PNG
	pngBytes, err := base64.StdEncoding.DecodeString(png1x1Base64)
	if err != nil {
		t.Fatalf("failed to decode png base64: %v", err)
	}
	if got := DetectSupportedImageMimeType(pngBytes); got != "image/png" {
		t.Errorf("expected image/png, got %q", got)
	}

	// 2. Valid JPEG
	jpegBytes := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}
	if got := DetectSupportedImageMimeType(jpegBytes); got != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %q", got)
	}

	// 3. Rejected JPEG-2000
	jpeg2000Bytes := []byte{0xff, 0xd8, 0xff, 0xf7}
	if got := DetectSupportedImageMimeType(jpeg2000Bytes); got != "" {
		t.Errorf("expected rejected JPEG-2000 (empty string), got %q", got)
	}

	// 4. Valid GIF
	gifBytes := []byte("GIF89a\x01\x00\x01\x00")
	if got := DetectSupportedImageMimeType(gifBytes); got != "image/gif" {
		t.Errorf("expected image/gif, got %q", got)
	}

	// 5. Valid WebP
	webpBytes := make([]byte, 12)
	copy(webpBytes[0:4], "RIFF")
	copy(webpBytes[8:12], "WEBP")
	if got := DetectSupportedImageMimeType(webpBytes); got != "image/webp" {
		t.Errorf("expected image/webp, got %q", got)
	}

	// 6. Text/Non-image
	textBytes := []byte("This is plain text with no image headers.")
	if got := DetectSupportedImageMimeType(textBytes); got != "" {
		t.Errorf("expected empty string for text, got %q", got)
	}
}

func TestRejectAnimatedPng(t *testing.T) {
	buf := append([]byte(nil), pngSignature...)

	writeChunk := func(t string, data []byte) {
		lenBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))
		buf = append(buf, lenBuf...)
		buf = append(buf, []byte(t)...)
		buf = append(buf, data...)
		buf = append(buf, 0, 0, 0, 0) // dummy CRC
	}

	writeChunk("IHDR", make([]byte, 13))
	writeChunk("acTL", []byte("animated control data"))
	writeChunk("IDAT", []byte("image data"))

	if got := DetectSupportedImageMimeType(buf); got != "" {
		t.Errorf("expected animated PNG to be rejected (empty string), got %q", got)
	}
}
