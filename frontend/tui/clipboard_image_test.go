package tui

import (
	"runtime"
	"testing"

	"github.com/enough/enough/backend/imageutil"
)

func TestReadClipboardImageNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("skipping on linux, test is for non-linux no-op")
	}

	data, mime, err := readClipboardImage()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil || mime != "" {
		t.Fatalf("expected nil/empty on non-linux, got data=%v, mime=%q", data, mime)
	}
}

func TestReadClipboardTextNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("skipping on linux")
	}
	text, err := readClipboardText()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" {
		t.Fatalf("expected empty text on non-linux, got %q", text)
	}
}

func TestMimeSniffOnFixtureBytes(t *testing.T) {
	// 1x1 transparent PNG fixture
	pngBytes := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG Signature
		0x00, 0x00, 0x00, 0x0d,                         // IHDR Length
		0x49, 0x48, 0x44, 0x52,                         // IHDR Chunk Type
		0x00, 0x00, 0x00, 0x01,                         // Width: 1
		0x00, 0x00, 0x00, 0x01,                         // Height: 1
		0x08, 0x06, 0x00, 0x00, 0x00,                   // Bit Depth, Color Type, etc.
		0x1f, 0x15, 0xc4, 0x89,                         // CRC
		0x00, 0x00, 0x00, 0x0a,                         // IDAT Length
		0x49, 0x44, 0x41, 0x54,                         // IDAT Chunk Type
		0x08, 0xd7, 0x63, 0xf8, 0x0f, 0x00, 0x01, 0x01, 0x01, 0x00, // IDAT Data
		0x18, 0xdd, 0x8d, 0xb0, // CRC
		0x00, 0x00, 0x00, 0x00, // IEND Length
		0x49, 0x45, 0x4e, 0x44, // IEND Chunk Type
		0xae, 0x42, 0x60, 0x82, // CRC
	}

	mime := imageutil.DetectSupportedImageMimeType(pngBytes)
	if mime != "image/png" {
		t.Fatalf("expected image/png, got %q", mime)
	}
}
