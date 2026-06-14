package web

import (
	"context"
	"sync"
)

// FetchURLs extracts readable text from one or more http(s) URLs (parallel, max 3).
func fetchURLsParallel(ctx context.Context, urls []string) []PageHit {
	if len(urls) == 0 {
		return nil
	}
	if len(urls) > maxFetchCap {
		urls = urls[:maxFetchCap]
	}

	results := make([]PageHit, len(urls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)

	for i, rawURL := range urls {
		wg.Add(1)
		go func(i int, rawURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = fetchPage(ctx, rawURL)
		}(i, rawURL)
	}
	wg.Wait()
	return results
}

func fetchPage(ctx context.Context, rawURL string) PageHit {
	page, err := FetchPage(ctx, rawURL)
	if err != nil {
		var fe *FetchError
		if e, ok := err.(*FetchError); ok {
			fe = e
		} else {
			fe = &FetchError{Kind: FetchNetwork, Message: err.Error()}
		}
		return PageHit{URL: rawURL, Fetch: fe}
	}
	return page
}
