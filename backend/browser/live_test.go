//go:build livebrowser

package browser

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/enough/enough/backend/web"
)

// ENOUGH_LIVE_BROWSER=1 go test -tags livebrowser ./backend/browser/... -run Live -v
func TestLiveScrapeExample(t *testing.T) {
	if os.Getenv("ENOUGH_LIVE_BROWSER") != "1" {
		t.Skip("skipping live browser test; set ENOUGH_LIVE_BROWSER=1 to run")
	}

	ctx := context.Background()
	hit, err := ScrapeURL(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("ScrapeURL failed: %v", err)
	}
	if !strings.Contains(hit.Content, "Example Domain") && !strings.Contains(hit.Content, "documentation examples") {
		t.Errorf("expected example.com content, got:\n%s", hit.Content)
	}
}

func TestLiveScrapeFandom(t *testing.T) {
	if os.Getenv("ENOUGH_LIVE_BROWSER") != "1" {
		t.Skip("skipping live browser test; set ENOUGH_LIVE_BROWSER=1 to run")
	}

	ctx := context.Background()
	hit, err := ScrapeURL(ctx, "https://minecraft.fandom.com/wiki/Creeper")
	if err != nil {
		t.Fatalf("ScrapeURL failed: %v", err)
	}
	if IsCloudflareChallengeText(hit.Content) || IsCloudflareChallengeText(hit.Title) {
		t.Fatalf("still on cloudflare challenge after wait:\ntitle=%q\ncontent=%q", hit.Title, hit.Content)
	}
	if !strings.Contains(hit.Content, "Creeper") {
		t.Errorf("expected scraped text to contain 'Creeper', got:\n%s", hit.Content)
	}
}

func TestLiveWebFetchFandom(t *testing.T) {
	if os.Getenv("ENOUGH_LIVE_BROWSER") != "1" {
		t.Skip("skipping live browser test; set ENOUGH_LIVE_BROWSER=1 to run")
	}

	old := web.BrowserFallback
	defer func() { web.BrowserFallback = old }()
	web.BrowserFallback = ScrapeURL

	ctx := context.Background()
	hit, err := web.FetchPage(ctx, "https://minecraft.fandom.com/wiki/Creeper")
	if err != nil {
		t.Fatalf("web.FetchPage failed: %v", err)
	}
	if IsCloudflareChallengeText(hit.Content) || IsCloudflareChallengeText(hit.Title) {
		t.Fatalf("still on cloudflare challenge after wait:\ntitle=%q\ncontent=%q", hit.Title, hit.Content)
	}
	if !strings.Contains(hit.Title, "Creeper") && !strings.Contains(hit.Content, "Creeper") {
		t.Errorf("expected Creeper in title or content, got title=%q content=%q", hit.Title, hit.Content)
	}
}
