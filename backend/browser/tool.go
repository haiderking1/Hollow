package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enough/enough/backend/web"
)

type BrowserArgs struct {
	Action       string                 `json:"action"`
	TabID        string                 `json:"tabId,omitempty"`
	URL          string                 `json:"url,omitempty"`
	Expression   string                 `json:"expression,omitempty"`
	Method       string                 `json:"method,omitempty"`
	Params       map[string]interface{} `json:"params,omitempty"`
	Selector     string                 `json:"selector,omitempty"`
	Index        *int                   `json:"index,omitempty"`
	Format       string                 `json:"format,omitempty"`
	SavePath     string                 `json:"savePath,omitempty"`
	AwaitPromise *bool                  `json:"awaitPromise,omitempty"`
}

type BrowserToolDetails struct {
	Action    string               `json:"action"`
	TabID     string               `json:"tabId,omitempty"`
	URL       string               `json:"url,omitempty"`
	Selector  string               `json:"selector,omitempty"`
	SavePath  string               `json:"savePath,omitempty"`
	Tabs      []BrowserTabSummary  `json:"tabs,omitempty"`
	Result    interface{}          `json:"result,omitempty"`
	Bytes     int                  `json:"bytes,omitempty"`
	Truncated bool                 `json:"truncated,omitempty"`
}

type BrowserTabSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Type  string `json:"type"`
}

func downloadViaPageFetch(cwd string, tabId string, downloadUrl string, savePath string, baseUrl string) (string, int, error) {
	destination := savePath
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(cwd, savePath)
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return "", 0, err
	}

	rawPayload, err := withCdpSession(tabId, baseUrl, func(session *CdpSession, tab CdpTab) (interface{}, error) {
		urlJson, _ := json.Marshal(downloadUrl)
		expr := fmt.Sprintf(`(async () => {
			const res = await fetch(%s, { credentials: "include" });
			const contentType = res.headers.get("content-type") || "";
			const ab = await res.arrayBuffer();
			if (ab.byteLength > %d) {
				throw new Error("Download exceeds %d bytes");
			}
			const bytes = Array.from(new Uint8Array(ab));
			return { ok: res.ok, status: res.status, contentType, bytes };
		})()`, string(urlJson), MaxDownloadBytes, MaxDownloadBytes)

		return EvaluateExpression(session, expr, true)
	})
	if err != nil {
		return "", 0, err
	}

	if rawPayload == nil {
		return "", 0, fmt.Errorf("Browser download returned no data")
	}

	var result struct {
		OK     bool    `json:"ok"`
		Status int     `json:"status"`
		Bytes  []uint8 `json:"bytes"`
	}

	payloadBytes, err := json.Marshal(rawPayload)
	if err != nil {
		return "", 0, err
	}
	if err := json.Unmarshal(payloadBytes, &result); err != nil {
		return "", 0, fmt.Errorf("Browser download returned invalid payload")
	}

	if !result.OK {
		return "", 0, fmt.Errorf("Browser download failed with HTTP %d", result.Status)
	}

	if err := os.WriteFile(destination, result.Bytes, 0600); err != nil {
		return "", 0, err
	}

	return destination, len(result.Bytes), nil
}

func ExecuteBrowser(ctx context.Context, cwd string, args BrowserArgs) (string, BrowserToolDetails, error) {
	baseUrl := getBrowserCdpBaseUrl()

	switch args.Action {
	case "list":
		tabs, err := ListCdpTabs(baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}
		var summaries []BrowserTabSummary
		for _, t := range tabs {
			summaries = append(summaries, BrowserTabSummary{
				ID:    t.ID,
				Title: t.Title,
				URL:   t.URL,
				Type:  t.Type,
			})
		}
		output := "No tabs."
		if len(summaries) > 0 {
			var lines []string
			for _, s := range summaries {
				lines = append(lines, fmt.Sprintf("%s [%s] %s - %s", s.ID, s.Type, s.Title, s.URL))
			}
			output = strings.Join(lines, "\n")
		}
		return output, BrowserToolDetails{
			Action: "list",
			Tabs:   summaries,
		}, nil

	case "open":
		tab, err := OpenCdpTab(args.URL, baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}
		summary := BrowserTabSummary{
			ID:    tab.ID,
			Title: tab.Title,
			URL:   tab.URL,
			Type:  tab.Type,
		}
		output := fmt.Sprintf("Opened tab %s: %s", tab.ID, tab.URL)
		return output, BrowserToolDetails{
			Action: "open",
			TabID:  tab.ID,
			URL:    tab.URL,
			Tabs:   []BrowserTabSummary{summary},
		}, nil

	case "close":
		tab, err := ResolveCdpTab(args.TabID, baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}
		err = CloseCdpTab(tab.ID, baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}
		output := fmt.Sprintf("Closed tab %s", tab.ID)
		return output, BrowserToolDetails{
			Action: "close",
			TabID:  tab.ID,
			URL:    tab.URL,
		}, nil

	case "activate":
		tab, err := ResolveCdpTab(args.TabID, baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}
		err = ActivateCdpTab(tab.ID, baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}
		output := fmt.Sprintf("Activated tab %s: %s", tab.ID, tab.URL)
		return output, BrowserToolDetails{
			Action: "activate",
			TabID:  tab.ID,
			URL:    tab.URL,
		}, nil

	case "cdp":
		if args.Method == "" {
			return "", BrowserToolDetails{}, fmt.Errorf("method is required for cdp action")
		}
		rawRes, err := withCdpSession(args.TabID, baseUrl, func(session *CdpSession, tab CdpTab) (interface{}, error) {
			if args.Method == "Page.navigate" {
				navigateUrl := args.URL
				if navigateUrl == "" {
					if u, ok := args.Params["url"].(string); ok {
						navigateUrl = u
					}
				}
				if navigateUrl == "" {
					return nil, fmt.Errorf("url is required for Page.navigate")
				}
				params := make(map[string]interface{})
				for k, v := range args.Params {
					params[k] = v
				}
				params["url"] = navigateUrl
				return session.Send(args.Method, params)
			}
			params := args.Params
			if params == nil {
				params = make(map[string]interface{})
			}
			return session.Send(args.Method, params)
		})
		if err != nil {
			return "", BrowserToolDetails{}, err
		}
		tab, err := ResolveCdpTab(args.TabID, baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}
		jsonBytes, _ := json.MarshalIndent(rawRes, "", "  ")
		return string(jsonBytes), BrowserToolDetails{
			Action: "cdp",
			TabID:  tab.ID,
			URL:    tab.URL,
			Result: rawRes,
		}, nil

	case "eval":
		plan, err := ResolveClickPlan(&args.Expression, &args.Selector, args.Index)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}

		rawRes, err := withCdpSession(args.TabID, baseUrl, func(session *CdpSession, tab CdpTab) (interface{}, error) {
			if plan != nil {
				return clickElementWithFeedback(session, *plan)
			}
			awaitPromise := true
			if args.AwaitPromise != nil {
				awaitPromise = *args.AwaitPromise
			}
			expr, err := ResolveEvalExpression(&args.Expression, &args.Selector, args.Index)
			if err != nil {
				return nil, err
			}
			return EvaluateExpression(session, expr, awaitPromise)
		})
		if err != nil {
			return "", BrowserToolDetails{}, err
		}

		tab, err := ResolveCdpTab(args.TabID, baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}

		text := FormatEvalResultText(rawRes)
		return text, BrowserToolDetails{
			Action:   "eval",
			TabID:    tab.ID,
			URL:      tab.URL,
			Selector: args.Selector,
			Result:   rawRes,
		}, nil

	case "scrape":
		format := args.Format
		if format == "" {
			format = "text"
		}
		if args.Selector != "" && format == "elements" {
			if err := ValidateCssSelector(args.Selector); err != nil {
				return "", BrowserToolDetails{}, err
			}
		}

		var selPtr *string
		if args.Selector != "" {
			selPtr = &args.Selector
		}

		expr, err := buildScrapeExpression(selPtr, format)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}

		rawScraped, err := withCdpSession(args.TabID, baseUrl, func(session *CdpSession, tab CdpTab) (interface{}, error) {
			return EvaluateExpression(session, expr, true)
		})
		if err != nil {
			return "", BrowserToolDetails{}, err
		}

		tab, err := ResolveCdpTab(args.TabID, baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}

		if rawScraped == nil {
			if args.Selector != "" {
				return "", BrowserToolDetails{
					Action:   "scrape",
					TabID:    tab.ID,
					URL:      tab.URL,
					Selector: args.Selector,
				}, fmt.Errorf("Scrape selector not found: %s", args.Selector)
			}
			return "", BrowserToolDetails{
				Action: "scrape",
				TabID:  tab.ID,
				URL:    tab.URL,
			}, fmt.Errorf("Scrape returned no content")
		}

		if format == "elements" {
			var list []interface{}
			bytes, err := json.Marshal(rawScraped)
			if err == nil {
				_ = json.Unmarshal(bytes, &list)
			}
			if len(list) == 0 {
				if args.Selector != "" {
					return "", BrowserToolDetails{
						Action:   "scrape",
						TabID:    tab.ID,
						URL:      tab.URL,
						Selector: args.Selector,
					}, fmt.Errorf("Selector matched no elements: %s", args.Selector)
				}
				return "", BrowserToolDetails{
					Action: "scrape",
					TabID:  tab.ID,
					URL:    tab.URL,
				}, fmt.Errorf("No clickable elements found on page")
			}
		}

		var rawText string
		if str, ok := rawScraped.(string); ok {
			rawText = str
		} else {
			bytes, err := json.MarshalIndent(rawScraped, "", "  ")
			if err == nil {
				rawText = string(bytes)
			} else {
				rawText = fmt.Sprintf("%v", rawScraped)
			}
		}

		truncatedText, isTruncated := truncateScrape(rawText)

		return truncatedText, BrowserToolDetails{
			Action:    "scrape",
			TabID:     tab.ID,
			URL:       tab.URL,
			Selector:  args.Selector,
			Truncated: isTruncated,
		}, nil

	case "download":
		if args.URL == "" {
			return "", BrowserToolDetails{}, fmt.Errorf("url is required for download action")
		}
		if args.SavePath == "" {
			return "", BrowserToolDetails{}, fmt.Errorf("savePath is required for download action")
		}

		savedPath, bytesCount, err := downloadViaPageFetch(cwd, args.TabID, args.URL, args.SavePath, baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}

		tab, err := ResolveCdpTab(args.TabID, baseUrl)
		if err != nil {
			return "", BrowserToolDetails{}, err
		}

		output := fmt.Sprintf("Downloaded %d bytes to %s", bytesCount, savedPath)
		return output, BrowserToolDetails{
			Action:   "download",
			TabID:    tab.ID,
			URL:      args.URL,
			SavePath: savedPath,
			Bytes:    bytesCount,
		}, nil
	}

	return "", BrowserToolDetails{}, fmt.Errorf("Unsupported browser action: %s", args.Action)
}
const MaxDownloadBytes = 10 * 1024 * 1024

func ScrapeURL(ctx context.Context, targetUrl string) (web.PageHit, error) {
	baseUrl := getBrowserCdpBaseUrl()

	_, err := ensureBrowserLaunched(baseUrl)
	if err != nil {
		return web.PageHit{}, err
	}

	tab, err := OpenCdpTab(targetUrl, baseUrl)
	if err != nil {
		return web.PageHit{}, err
	}
	defer CloseCdpTab(tab.ID, baseUrl)

	session, err := connectCdpSession(tab)
	if err != nil {
		return web.PageHit{}, err
	}

	waitForPageReady(session, 5*time.Second)

	htmlExpr, err := buildScrapeExpression(nil, "html")
	if err != nil {
		return web.PageHit{}, err
	}

	var htmlStr string
	for attempt := 0; attempt < 2; attempt++ {
		if err := waitForCloudflareClearance(session); err != nil {
			return web.PageHit{}, err
		}

		htmlVal, err := EvaluateExpression(session, htmlExpr, true)
		if err != nil {
			return web.PageHit{}, err
		}

		var ok bool
		htmlStr, ok = htmlVal.(string)
		if !ok || htmlStr == "" {
			return web.PageHit{}, fmt.Errorf("browser returned empty html")
		}

		if IsCloudflareChallengeText(htmlStr) {
			if attempt == 0 {
				time.Sleep(CloudflareWaitTimeout)
				continue
			}
			return web.PageHit{}, fmt.Errorf("cloudflare challenge still present after wait")
		}

		title, content, extractErr := web.ExtractPageContent(targetUrl, []byte(htmlStr))
		if extractErr == nil {
			return web.PageHit{
				Title:   title,
				URL:     targetUrl,
				Content: content,
			}, nil
		}

		if attempt == 0 && extractErr != nil {
			if extractErr.Kind == web.FetchJSRendered || IsCloudflareChallengeText(htmlStr) {
				time.Sleep(CloudflareWaitTimeout)
				if err := waitForCloudflareClearance(session); err != nil {
					return web.PageHit{}, err
				}
				continue
			}
		}
		return web.PageHit{URL: targetUrl, Title: title}, extractErr
	}

	return web.PageHit{}, fmt.Errorf("browser scrape failed after cloudflare wait")
}
