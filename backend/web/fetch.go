package web

import (
	"context"
	"strings"
)

var BrowserFallback func(ctx context.Context, url string) (PageHit, error)

// FetchPage downloads a URL and extracts readable text with fallbacks.
func FetchPage(ctx context.Context, rawURL string) (PageHit, error) {
	if err := ctx.Err(); err != nil {
		return PageHit{}, err
	}

	u, err := validateFetchURL(rawURL)
	if err != nil {
		return PageHit{URL: rawURL}, &FetchError{Kind: FetchInvalidURL, Message: err.Error()}
	}

	pageURL := u.String()
	if isYouTubeHost(u.Hostname()) {
		if hit, err := fetchYouTubeOEmbed(ctx, pageURL); err == nil {
			return hit, nil
		}
	}

	fetched, ferr := downloadHTML(ctx, pageURL)
	if ferr != nil {
		if ferr.Kind == FetchBlocked && BrowserFallback != nil {
			if hit, err := BrowserFallback(ctx, pageURL); err == nil {
				return hit, nil
			}
		}
		return PageHit{URL: pageURL}, ferr
	}

	title, content, extractErr := ExtractPageContent(fetched.finalURL, fetched.body)
	if extractErr != nil {
		if extractErr.Kind == FetchJSRendered && BrowserFallback != nil {
			if hit, err := BrowserFallback(ctx, pageURL); err == nil {
				return hit, nil
			}
		}
		return PageHit{URL: fetched.finalURL, Title: title}, extractErr
	}

	return PageHit{
		Title:   title,
		URL:     fetched.finalURL,
		Content: content,
	}, nil
}

func NormalizeFetchURLs(raw []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range raw {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		if !isHTTPURL(u) {
			continue
		}
		seen[u] = true
		out = append(out, u)
		if len(out) >= maxFetchCap {
			break
		}
	}
	return out
}
