package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFetchPageUsesBrowserFallbackOnBlocked(t *testing.T) {
	os.Setenv("ENOUGH_WEB_ALLOW_PRIVATE", "1")
	defer os.Unsetenv("ENOUGH_WEB_ALLOW_PRIVATE")

	// 1. Mock server that returns 403 Forbidden (Blocked)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Forbidden by Cloudflare"))
	}))
	defer srv.Close()

	// 2. Set up BrowserFallback mock
	old := BrowserFallback
	defer func() { BrowserFallback = old }()

	fallbackCalled := false
	BrowserFallback = func(ctx context.Context, url string) (PageHit, error) {
		fallbackCalled = true
		return PageHit{
			URL:     url,
			Title:   "Fallback Title",
			Content: "Fallback content on blocked site",
		}, nil
	}

	ctx := context.Background()
	hit, err := FetchPage(ctx, srv.URL+"/wiki/Creeper")
	if err != nil {
		t.Fatalf("FetchPage failed: %v", err)
	}

	if !fallbackCalled {
		t.Errorf("expected BrowserFallback to be called, but it was not")
	}

	if hit.Title != "Fallback Title" || !strings.Contains(hit.Content, "Fallback content") {
		t.Errorf("expected fallback content, got %+v", hit)
	}
}

func TestFetchPageUsesBrowserFallbackOnJSRendered(t *testing.T) {
	os.Setenv("ENOUGH_WEB_ALLOW_PRIVATE", "1")
	defer os.Unsetenv("ENOUGH_WEB_ALLOW_PRIVATE")

	// 1. Mock server that returns 200 but minimal HTML triggering FetchJSRendered
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		// Empty body with only scripts, so readability and text-stripping fails
		w.Write([]byte(`<html><head><script>eval("something");</script></head><body></body></html>`))
	}))
	defer srv.Close()

	// 2. Set up BrowserFallback mock
	old := BrowserFallback
	defer func() { BrowserFallback = old }()

	fallbackCalled := false
	BrowserFallback = func(ctx context.Context, url string) (PageHit, error) {
		fallbackCalled = true
		return PageHit{
			URL:     url,
			Title:   "JS Fallback Title",
			Content: "JS fallback rendered content",
		}, nil
	}

	ctx := context.Background()
	hit, err := FetchPage(ctx, srv.URL+"/wiki/Creeper")
	if err != nil {
		t.Fatalf("FetchPage failed: %v", err)
	}

	if !fallbackCalled {
		t.Errorf("expected BrowserFallback to be called, but it was not")
	}

	if hit.Title != "JS Fallback Title" || !strings.Contains(hit.Content, "JS fallback") {
		t.Errorf("expected fallback content, got %+v", hit)
	}
}
