package markdown

import (
	"strings"

	"github.com/yuin/goldmark/ast"
)

func isLocalImageURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	return strings.HasPrefix(rawURL, "data:") || strings.HasPrefix(rawURL, "file://")
}

func (r *renderer) renderImageURL(url, alt string, nextKind string) []renderLine {
	url = strings.TrimSpace(url)
	alt = strings.TrimSpace(alt)
	caps := currentCapabilities()

	if data, err, ok := globalImageCache.get(url); ok {
		if err != nil || data == nil {
			return r.imagePlaceholder(alt, url, nil, nextKind)
		}
		return r.imageLines(data, alt, url, nextKind)
	}

	// Pasted attachments and read_file previews are already local — render
	// synchronously so the first frame shows pixels, not a data-URL placeholder.
	if (caps.Images != ImageNone || caps.TrueColor) && isLocalImageURL(url) {
		if data, err := fetchImage(url); err == nil && data != nil {
			primeImageCache(url, data)
			return r.imageLines(data, alt, url, nextKind)
		}
	}

	if (caps.Images != ImageNone || caps.TrueColor) && isFetchableImageURL(url) {
		globalImageCache.load(url, r.opts.OnImageReady)
	}

	return r.imagePlaceholder(alt, url, nil, nextKind)
}

func (r *renderer) imageLines(data *ImageData, alt, url, nextKind string) []renderLine {
	maxWidth := r.width
	if maxWidth > 60 {
		maxWidth = 60
	}
	if maxWidth < 10 {
		maxWidth = 10
	}

	lines := renderImageSequence(data.Base64, data.MIME, data.Dimensions, maxWidth, alt)
	if len(lines) == 0 {
		return r.imagePlaceholder(alt, url, &data.Dimensions, nextKind)
	}

	out := make([]renderLine, 0, len(lines)+1)
	for _, line := range lines {
		out = append(out, rl(line, true))
	}
	if nextKind != "" && nextKind != "space" {
		out = append(out, rl("", false))
	}
	return out
}

func (r *renderer) imagePlaceholder(alt, url string, dims *ImageDimensions, nextKind string) []renderLine {
	label := r.theme.Image(imageFallbackLabel("", dims, alt, url))
	if link := hyperlinkImageURL(url); link != "" && currentCapabilities().Hyperlinks {
		label = Hyperlink(label, link)
	}
	out := []renderLine{rl(label, false)}
	if nextKind != "" && nextKind != "space" {
		out = append(out, rl("", false))
	}
	return out
}

func isFetchableImageURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	switch {
	case strings.HasPrefix(rawURL, "http://"), strings.HasPrefix(rawURL, "https://"):
		return true
	case strings.HasPrefix(rawURL, "data:image/"):
		return true
	case strings.HasPrefix(rawURL, "file://"):
		return true
	default:
		return false
	}
}

func soleImage(n ast.Node, source []byte) *ast.Image {
	var img *ast.Image
	found := 0
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Text:
			if strings.TrimSpace(string(t.Segment.Value(source))) != "" {
				return nil
			}
		case *ast.Image:
			img = t
			found++
			if found > 1 {
				return nil
			}
		default:
			return nil
		}
	}
	if found == 1 {
		return img
	}
	return nil
}

func imageAlt(img *ast.Image, source []byte) string {
	var r renderer
	r.theme = Theme{}.withDefaults()
	return strings.TrimSpace(r.renderInline(img, source))
}
