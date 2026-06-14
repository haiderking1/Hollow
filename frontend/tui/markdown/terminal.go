package markdown

import (
	"os"
	"strings"
)

// Capabilities describes optional terminal features for markdown rendering.
type Capabilities struct {
	Hyperlinks bool
	Images     ImageProtocol
	TrueColor  bool
}

var capabilitiesFn = detectCapabilities

func CapabilitiesForTest(c Capabilities) func() {
	prev := capabilitiesFn
	capabilitiesFn = func() Capabilities { return c }
	return func() { capabilitiesFn = prev }
}

func currentCapabilities() Capabilities {
	return capabilitiesFn()
}

func detectCapabilities() Capabilities {
	termProgram := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	term := strings.ToLower(os.Getenv("TERM"))
	colorTerm := strings.ToLower(os.Getenv("COLORTERM"))
	trueColor := colorTerm == "truecolor" || colorTerm == "24bit"

	inTmuxOrScreen := os.Getenv("TMUX") != "" || strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "screen")
	// Kitty passthrough in tmux still exposes KITTY_WINDOW_ID on the host.
	if inTmuxOrScreen && os.Getenv("KITTY_WINDOW_ID") == "" && os.Getenv("ITERM_SESSION_ID") == "" {
		return Capabilities{Hyperlinks: false, Images: ImageNone, TrueColor: trueColor}
	}

	switch {
	case os.Getenv("KITTY_WINDOW_ID") != "" || termProgram == "kitty":
		return Capabilities{Hyperlinks: true, Images: ImageKitty, TrueColor: true}
	case termProgram == "ghostty" || strings.Contains(term, "ghostty") || os.Getenv("GHOSTTY_RESOURCES_DIR") != "":
		return Capabilities{Hyperlinks: true, Images: ImageKitty, TrueColor: true}
	case os.Getenv("WEZTERM_PANE") != "" || termProgram == "wezterm":
		return Capabilities{Hyperlinks: true, Images: ImageKitty, TrueColor: true}
	case os.Getenv("ITERM_SESSION_ID") != "" || termProgram == "iterm.app":
		return Capabilities{Hyperlinks: true, Images: ImageITerm2, TrueColor: true}
	// Foot renders images via sixel, not Kitty graphics. Foot does not set
	// identifying env vars; TERM is usually "foot" when foot's terminfo is installed.
	// https://codeberg.org/dnkl/foot#sixel-image-support
	case strings.Contains(term, "foot"):
		return Capabilities{Hyperlinks: true, Images: ImageSixel, TrueColor: true}
	case strings.Contains(term, "mlterm"), strings.Contains(term, "contour"):
		return Capabilities{Hyperlinks: true, Images: ImageSixel, TrueColor: true}
	case termProgram == "vscode":
		return Capabilities{Hyperlinks: true, Images: ImageNone, TrueColor: true}
	case termProgram == "alacritty":
		return Capabilities{Hyperlinks: true, Images: ImageNone, TrueColor: trueColor || true}
	case trueColor:
		return Capabilities{Hyperlinks: true, Images: ImageNone, TrueColor: true}
	default:
		return Capabilities{Hyperlinks: false, Images: ImageNone, TrueColor: false}
	}
}

// Hyperlink wraps visible text in an OSC 8 hyperlink sequence.
func Hyperlink(text, url string) string {
	if text == "" || url == "" {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

func displayImageURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "data:") {
		if semi := strings.Index(rawURL, ";"); semi > 5 {
			return rawURL[:semi] + ";…"
		}
		return "data:…"
	}
	if len(rawURL) > 120 {
		return rawURL[:117] + "…"
	}
	return rawURL
}

func imageFallback(alt, url string) string {
	alt = strings.TrimSpace(alt)
	url = displayImageURL(url)
	switch {
	case alt != "" && url != "":
		return "[Image: " + alt + " (" + url + ")]"
	case alt != "":
		return "[Image: " + alt + "]"
	case url != "":
		return "[Image: " + url + "]"
	default:
		return "[Image]"
	}
}

func hyperlinkImageURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || strings.HasPrefix(rawURL, "data:") || len(rawURL) > 512 {
		return ""
	}
	return rawURL
}
