package markdown

import (
	"strings"
	"sync"
)

type imageCache struct {
	mu      sync.Mutex
	entries map[string]*imageCacheEntry
}

type imageCacheEntry struct {
	data  *ImageData
	err   error
	ready chan struct{}
}

var globalImageCache imageCache

func init() {
	globalImageCache.entries = make(map[string]*imageCacheEntry)
}

// ResetImageCache clears cached images. For tests only.
func ResetImageCache() {
	globalImageCache.mu.Lock()
	defer globalImageCache.mu.Unlock()
	globalImageCache.entries = make(map[string]*imageCacheEntry)
}

func (c *imageCache) get(url string) (*ImageData, error, bool) {
	c.mu.Lock()
	entry, ok := c.entries[url]
	c.mu.Unlock()
	if !ok {
		return nil, nil, false
	}
	<-entry.ready
	return entry.data, entry.err, true
}

func (c *imageCache) load(url string, onReady func()) {
	c.mu.Lock()
	entry, ok := c.entries[url]
	if !ok {
		entry = &imageCacheEntry{ready: make(chan struct{})}
		c.entries[url] = entry
		go func() {
			entry.data, entry.err = fetchImage(url)
			close(entry.ready)
			if onReady != nil {
				onReady()
			}
		}()
	} else if onReady != nil {
		go func() {
			<-entry.ready
			onReady()
		}()
	}
	c.mu.Unlock()
}

func primeImageCache(url string, data *ImageData) {
	entry := &imageCacheEntry{data: data, ready: make(chan struct{})}
	close(entry.ready)
	globalImageCache.mu.Lock()
	globalImageCache.entries[url] = entry
	globalImageCache.mu.Unlock()
}

// ImageReady reports whether url has finished loading into the image cache.
func ImageReady(url string) bool {
	globalImageCache.mu.Lock()
	entry, ok := globalImageCache.entries[url]
	globalImageCache.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case <-entry.ready:
		return entry.err == nil && entry.data != nil
	default:
		return false
	}
}

// RenderAttachmentImage renders a single image URL for chat attachments without
// parsing markdown (avoids embedding megabyte data URLs in goldmark input).
func RenderAttachmentImage(rawURL string, width int, theme Theme, opts RenderOptions) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if width < 10 {
		width = 10
	}
	theme = theme.withDefaults()
	r := &renderer{width: width, theme: theme, opts: opts}
	lines := r.renderImageURL(rawURL, "image", "")
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(wrapRenderLines(lines, width), "\n")
}
