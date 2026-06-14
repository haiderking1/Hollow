package web

import (
	"context"
	"fmt"
	"strings"
)

// SearchWeb runs a meta-search and returns snippets without fetching pages.
func SearchWeb(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	query = trimInput(query)
	if query == "" {
		return nil, ErrEmptyInput
	}
	if isHTTPURL(query) {
		return []SearchResult{{
			Title: query,
			URL:   query,
		}}, nil
	}

	provider, err := NewSearchProvider(ctx)
	if err != nil {
		return nil, err
	}
	return provider.Search(ctx, query, opts)
}

// FetchURLs extracts readable text from one or more http(s) URLs.
func FetchURLs(ctx context.Context, urls []string) []PageHit {
	return fetchURLsParallel(ctx, urls)
}

// FormatSearchResults renders search hits for web_search.
func FormatSearchResults(results []SearchResult) string {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		title := r.Title
		if title == "" {
			title = r.URL
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, title)
		fmt.Fprintf(&b, "   URL: %s\n", r.URL)
		if r.Engine != "" {
			fmt.Fprintf(&b, "   Engine: %s\n", r.Engine)
		}
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   Snippet: %s\n", r.Snippet)
		}
	}
	out := b.String()
	if len(out) > maxOutputBytes {
		out = out[:maxOutputBytes] + "\n\n... truncated ..."
	}
	return out
}

// FormatPages renders fetched pages for web_fetch.
func FormatPages(hits []PageHit) string {
	var b strings.Builder
	for i, hit := range hits {
		if i > 0 {
			b.WriteString("\n\n")
		}
		title := hit.Title
		if title == "" {
			title = hit.URL
		}
		fmt.Fprintf(&b, "=== %s ===\n", title)
		fmt.Fprintf(&b, "URL: %s\n\n", hit.URL)
		if hit.Fetch != nil {
			fmt.Fprintf(&b, "Error %s\n", hit.Fetch.Error())
			continue
		}
		b.WriteString(hit.Content)
	}
	out := b.String()
	if len(out) > maxOutputBytes {
		out = out[:maxOutputBytes] + "\n\n... truncated ..."
	}
	return out
}
