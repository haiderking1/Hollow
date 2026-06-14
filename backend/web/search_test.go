package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormatSearchResults(t *testing.T) {
	out := FormatSearchResults([]SearchResult{{
		Title:   "Example",
		URL:     "https://example.com",
		Snippet: "A snippet.",
		Engine:  "duckduckgo",
	}})
	if !strings.Contains(out, "Snippet: A snippet.") {
		t.Fatalf("missing snippet: %q", out)
	}
	if !strings.Contains(out, "Engine: duckduckgo") {
		t.Fatalf("missing engine: %q", out)
	}
}

func TestFormatPagesStructuredError(t *testing.T) {
	out := FormatPages([]PageHit{{
		URL:   "https://fandom.com/x",
		Fetch: &FetchError{Kind: FetchJSRendered, Message: "needs JavaScript"},
	}})
	if !strings.Contains(out, "[js_rendered]") {
		t.Fatalf("expected js_rendered: %q", out)
	}
}

func TestFetchPageStaticHTML(t *testing.T) {
	t.Setenv("ENOUGH_WEB_ALLOW_PRIVATE", "1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Docs</title></head><body><article><h1>Hello</h1><p>` +
			strings.Repeat("Readable article body. ", 40) + `</p></article></body></html>`))
	}))
	defer srv.Close()

	hit, err := FetchPage(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if hit.Fetch != nil {
		t.Fatalf("fetch error: %v", hit.Fetch)
	}
	if !strings.Contains(hit.Content, "Readable article body") {
		t.Fatalf("missing content: %q", hit.Content)
	}
}

func TestFetchPageMetaFallback(t *testing.T) {
	t.Setenv("ENOUGH_WEB_ALLOW_PRIVATE", "1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head>
			<title>Stub</title>
			<meta property="og:description" content="Summary from meta tags." />
		</head><body><div id="app"></div></body></html>`))
	}))
	defer srv.Close()

	hit, err := FetchPage(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(hit.Content, "Summary from meta tags") {
		t.Fatalf("expected meta fallback, got %q err=%v", hit.Content, hit.Fetch)
	}
}

func TestSearXNGParsesSnippet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Hit","url":"https://example.com/a","content":"snippet text","engine":"google"}]}`))
	}))
	defer srv.Close()

	p := &SearXNGProvider{BaseURL: srv.URL}
	results, err := p.Search(context.Background(), "test", SearchOptions{MaxResults: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Snippet != "snippet text" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestBuildSearchQuerySite(t *testing.T) {
	got := buildSearchQuery("golang channels", "reddit.com")
	if got != "site:reddit.com golang channels" {
		t.Fatalf("got %q", got)
	}
}

func TestURLExcluded(t *testing.T) {
	if !urlExcluded("https://www.fandom.com/wiki/X", []string{"fandom.com"}) {
		t.Fatal("expected exclusion")
	}
}

func TestValidateFetchURLBlocksLocalhost(t *testing.T) {
	t.Setenv("ENOUGH_WEB_ALLOW_PRIVATE", "0")
	if _, err := validateFetchURL("http://localhost/"); err == nil {
		t.Fatal("expected localhost to be blocked")
	}
}
