package tui

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/enough/enough/backend/imageutil"
)

// readClipboardImage attempts to read an image from the clipboard.
// If not Linux or no image exists/tooling fails, it returns nil, "", nil.
func readClipboardImage() ([]byte, string, error) {
	if runtime.GOOS != "linux" {
		return nil, "", nil
	}

	isWayland := os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland"
	mimes := []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

	if isWayland {
		if _, err := exec.LookPath("wl-paste"); err == nil {
			for _, mime := range mimes {
				data, err := runClipboardCommand(3*time.Second, "wl-paste", "-t", mime)
				if err == nil && len(data) > 0 {
					detectedMime := imageutil.DetectSupportedImageMimeType(data)
					if detectedMime != "" {
						return data, detectedMime, nil
					}
				}
			}
		}
	}

	// X11 / Fallback: check xclip
	if _, err := exec.LookPath("xclip"); err == nil {
		for _, mime := range mimes {
			data, err := runClipboardCommand(3*time.Second, "xclip", "-selection", "clipboard", "-t", mime, "-o")
			if err == nil && len(data) > 0 {
				detectedMime := imageutil.DetectSupportedImageMimeType(data)
				if detectedMime != "" {
					return data, detectedMime, nil
				}
			}
		}
	}

	// Fallback to xsel
	if _, err := exec.LookPath("xsel"); err == nil {
		data, err := runClipboardCommand(3*time.Second, "xsel", "--clipboard", "--output")
		if err == nil && len(data) > 0 {
			detectedMime := imageutil.DetectSupportedImageMimeType(data)
			if detectedMime != "" {
				return data, detectedMime, nil
			}
		}
	}

	return nil, "", nil
}

// readClipboardText reads plain text from the clipboard on Linux.
// Returns ("", nil) when unavailable or empty.
func readClipboardText() (string, error) {
	if runtime.GOOS != "linux" {
		return "", nil
	}

	isWayland := os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland"
	if isWayland {
		if _, err := exec.LookPath("wl-paste"); err == nil {
			data, err := runClipboardCommand(3*time.Second, "wl-paste", "--no-newline")
			if err == nil && len(data) > 0 {
				return string(data), nil
			}
		}
	}

	if _, err := exec.LookPath("xclip"); err == nil {
		data, err := runClipboardCommand(3*time.Second, "xclip", "-selection", "clipboard", "-o")
		if err == nil && len(data) > 0 {
			return string(data), nil
		}
	}

	if _, err := exec.LookPath("xsel"); err == nil {
		data, err := runClipboardCommand(3*time.Second, "xsel", "--clipboard", "--output")
		if err == nil && len(data) > 0 {
			return string(data), nil
		}
	}

	return "", nil
}

func runClipboardCommand(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Limit to 50MB
	limitReader := io.LimitReader(stdout, 50*1024*1024)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}

	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return data, nil
}
