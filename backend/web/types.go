package web

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrEmptyInput       = errors.New("query cannot be empty")
	ErrNoSearchProvider = errors.New("web search unavailable")
)

// FetchFailureKind classifies page fetch failures for the agent.
type FetchFailureKind string

const (
	FetchTimeout     FetchFailureKind = "timeout"
	FetchBlocked     FetchFailureKind = "blocked"
	FetchRateLimited FetchFailureKind = "rate_limited"
	FetchHTTPError   FetchFailureKind = "http_error"
	FetchNetwork     FetchFailureKind = "network"
	FetchNoContent   FetchFailureKind = "no_content"
	FetchJSRendered  FetchFailureKind = "js_rendered"
	FetchInvalidURL  FetchFailureKind = "invalid_url"
)

// FetchError is a structured fetch failure.
type FetchError struct {
	Kind       FetchFailureKind
	HTTPStatus int
	Message    string
}

func (e *FetchError) Error() string {
	if e == nil {
		return ""
	}
	if e.HTTPStatus > 0 {
		return fmt.Sprintf("[%s] HTTP %d: %s", e.Kind, e.HTTPStatus, e.Message)
	}
	return fmt.Sprintf("[%s] %s", e.Kind, e.Message)
}

// SearchResult is a lightweight search hit from SearXNG.
type SearchResult struct {
	Title   string
	URL     string
	Snippet string
	Engine  string
}

// SearchOptions controls meta-search behavior.
type SearchOptions struct {
	MaxResults   int
	Site         string
	ExcludeSites []string
	Engines      string
}

// Provider finds result URLs for a query.
type Provider interface {
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
}

// PageHit is extracted page content from web_fetch.
type PageHit struct {
	Title   string
	URL     string
	Content string
	Fetch   *FetchError
}

// FetchOptions controls URL extraction.
type FetchOptions struct {
	MaxURLs int
}

const (
	defaultMaxResults = 8
	maxResultsCap     = 15
	defaultMaxFetch   = 3
	maxFetchCap       = 5
	maxOutputBytes    = 96_000
	fetchTimeoutSec   = 20
)
