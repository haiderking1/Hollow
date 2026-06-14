package web

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestLiveFetchSites(t *testing.T) {
	if os.Getenv("ENOUGH_LIVE_WEB") != "1" {
		t.Skip("set ENOUGH_LIVE_WEB=1 to run")
	}
	urls := []string{
		"https://minecraft.fandom.com/wiki/Creeper",
		"https://gamefaqs.gamespot.com/",
	}
	for _, u := range urls {
		hit, err := FetchPage(context.Background(), u)
		t.Logf("URL: %s", u)
		if err != nil {
			if fe, ok := err.(*FetchError); ok {
				t.Logf("  err: %s", fe.Error())
			} else {
				t.Logf("  err: %v", err)
			}
		}
		if hit.Fetch != nil {
			t.Logf("  fetch: %s", hit.Fetch.Error())
		} else {
			preview := hit.Content
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			t.Logf("  ok: title=%q len=%d preview=%q", hit.Title, len(hit.Content), preview)
		}
		fmt.Println()
	}
}
