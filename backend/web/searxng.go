package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SearXNGProvider queries a self-hosted SearXNG instance (JSON format).
type SearXNGProvider struct {
	BaseURL string
	Client  *http.Client
}

func (p *SearXNGProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (p *SearXNGProvider) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}
	if maxResults > maxResultsCap {
		maxResults = maxResultsCap
	}

	query = buildSearchQuery(query, opts.Site)

	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	u, err := url.Parse(base + "/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	if engines := strings.TrimSpace(opts.Engines); engines != "" {
		q.Set("engines", engines)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client().Do(req)
	if err != nil {
		return nil, classifySearchError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
			Engine  string `json:"engine"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("searxng: parse response: %w", err)
	}

	var out []SearchResult
	for _, r := range parsed.Results {
		if r.URL == "" || urlExcluded(r.URL, opts.ExcludeSites) {
			continue
		}
		out = append(out, SearchResult{
			Title:   strings.TrimSpace(r.Title),
			URL:     r.URL,
			Snippet: strings.TrimSpace(r.Content),
			Engine:  strings.TrimSpace(r.Engine),
		})
		if len(out) >= maxResults {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("searxng: no results for %q", query)
	}
	return out, nil
}

func buildSearchQuery(query, site string) string {
	query = strings.TrimSpace(query)
	site = strings.TrimSpace(site)
	if site == "" {
		return query
	}
	site = strings.TrimPrefix(strings.TrimPrefix(site, "site:"), "SITE:")
	if strings.Contains(strings.ToLower(query), "site:") {
		return query
	}
	return "site:" + site + " " + query
}

func urlExcluded(rawURL string, excludes []string) bool {
	if len(excludes) == 0 {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, ex := range excludes {
		ex = strings.ToLower(strings.TrimSpace(ex))
		ex = strings.TrimPrefix(ex, "www.")
		if ex == "" {
			continue
		}
		if host == ex || strings.HasSuffix(host, "."+ex) {
			return true
		}
	}
	return false
}

func classifySearchError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
		return fmt.Errorf("searxng: timeout: %w", err)
	}
	return err
}
