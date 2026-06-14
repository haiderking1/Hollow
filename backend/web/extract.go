package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html"
)

var (
	metaDescRe = regexp.MustCompile(`(?i)<meta[^>]+name=["']description["'][^>]+content=["']([^"']+)["']`)
	ogDescRe   = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:description["'][^>]+content=["']([^"']+)["']`)
)

type htmlFetch struct {
	finalURL string
	status   int
	body     []byte
}

func downloadHTML(ctx context.Context, pageURL string) (htmlFetch, *FetchError) {
	timeout := time.Duration(fetchTimeoutSec) * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, pageURL, nil)
	if err != nil {
		return htmlFetch{}, &FetchError{Kind: FetchNetwork, Message: err.Error()}
	}
	req.Header.Set("User-Agent", userAgent())

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		if reqCtx.Err() != nil {
			return htmlFetch{}, &FetchError{Kind: FetchTimeout, Message: "request timed out"}
		}
		return htmlFetch{}, &FetchError{Kind: FetchNetwork, Message: err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return htmlFetch{}, &FetchError{Kind: FetchNetwork, Message: err.Error()}
	}

	status := resp.StatusCode
	if status == http.StatusTooManyRequests {
		return htmlFetch{}, &FetchError{Kind: FetchRateLimited, HTTPStatus: status, Message: "rate limited"}
	}
	if status == http.StatusForbidden || status == http.StatusUnauthorized {
		return htmlFetch{}, &FetchError{Kind: FetchBlocked, HTTPStatus: status, Message: "access denied"}
	}
	if status >= 400 {
		return htmlFetch{}, &FetchError{Kind: FetchHTTPError, HTTPStatus: status, Message: http.StatusText(status)}
	}

	finalURL := pageURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return htmlFetch{finalURL: finalURL, status: status, body: body}, nil
}

func ExtractPageContent(pageURL string, body []byte) (title, content string, err *FetchError) {
	if text, t, ok := extractYouTubeFromHTML(pageURL, body); ok {
		return t, text, nil
	}

	parsed, parseErr := neturl.Parse(pageURL)
	if parseErr != nil {
		parsed, _ = neturl.ParseRequestURI(pageURL)
	}

	article, readErr := readability.FromReader(bytes.NewReader(body), parsed)
	if readErr == nil && article.Node != nil {
		var buf bytes.Buffer
		if renderErr := article.RenderText(&buf); renderErr == nil {
			text := strings.TrimSpace(buf.String())
			if text != "" {
				return article.Title(), text, nil
			}
		}
	}

	if meta := extractMetaDescription(body); meta != "" {
		title := extractHTMLTitle(body)
		return title, meta, nil
	}

	if plain := stripHTMLText(body); len(plain) >= 120 {
		return extractHTMLTitle(body), plain, nil
	}

	if readErr == nil && article.Node == nil {
		return "", "", &FetchError{
			Kind:    FetchJSRendered,
			Message: "no article content in static HTML — page likely requires JavaScript",
		}
	}
	if readErr != nil && strings.Contains(readErr.Error(), "Node field is nil") {
		return "", "", &FetchError{
			Kind:    FetchJSRendered,
			Message: "no article content in static HTML — page likely requires JavaScript",
		}
	}

	return "", "", &FetchError{Kind: FetchNoContent, Message: "no readable content extracted"}
}

func extractMetaDescription(body []byte) string {
	s := string(body)
	if m := ogDescRe.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(htmlUnescape(m[1]))
	}
	if m := metaDescRe.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(htmlUnescape(m[1]))
	}
	return ""
}

func extractHTMLTitle(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if title != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return title
}

func stripHTMLText(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	removeNodes(doc, "script", "style", "noscript", "nav", "header", "footer")
	var buf strings.Builder
	collectText(doc, &buf)
	return strings.Join(strings.Fields(buf.String()), " ")
}

func removeNodes(n *html.Node, tags ...string) {
	if n == nil {
		return
	}
	tagSet := map[string]bool{}
	for _, t := range tags {
		tagSet[t] = true
	}
	var prune func(*html.Node)
	prune = func(node *html.Node) {
		for c := node.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.ElementNode && tagSet[c.Data] {
				node.RemoveChild(c)
			} else {
				prune(c)
			}
			c = next
		}
	}
	prune(n)
}

func collectText(n *html.Node, buf *strings.Builder) {
	if n.Type == html.TextNode {
		t := strings.TrimSpace(n.Data)
		if t != "" {
			if buf.Len() > 0 {
				buf.WriteByte(' ')
			}
			buf.WriteString(t)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, buf)
	}
}

func htmlUnescape(s string) string {
	return html.UnescapeString(s)
}

func extractYouTubeFromHTML(pageURL string, body []byte) (text, title string, ok bool) {
	u, err := neturl.Parse(pageURL)
	if err != nil || !isYouTubeHost(u.Hostname()) {
		return "", "", false
	}
	title = extractHTMLTitle(body)
	desc := extractMetaDescription(body)
	if title == "" && desc == "" {
		return "", "", false
	}
	var b strings.Builder
	if title != "" {
		b.WriteString("Title: ")
		b.WriteString(title)
		b.WriteByte('\n')
	}
	if desc != "" {
		b.WriteString("Description: ")
		b.WriteString(desc)
		b.WriteByte('\n')
	}
	b.WriteString("\nNote: Full YouTube transcripts and comments require the watch page API or a browser; static fetch returns metadata only.")
	return strings.TrimSpace(b.String()), title, true
}

func isYouTubeHost(host string) bool {
	host = strings.ToLower(host)
	return host == "youtube.com" || host == "www.youtube.com" || host == "m.youtube.com" || host == "youtu.be"
}

func fetchYouTubeOEmbed(ctx context.Context, pageURL string) (PageHit, error) {
	oembed := "https://www.youtube.com/oembed?format=json&url=" + neturl.QueryEscape(pageURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, oembed, nil)
	if err != nil {
		return PageHit{}, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return PageHit{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PageHit{}, fmt.Errorf("youtube oembed: %s", resp.Status)
	}
	var data struct {
		Title      string `json:"title"`
		AuthorName string `json:"author_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return PageHit{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", data.Title)
	if data.AuthorName != "" {
		fmt.Fprintf(&b, "Channel: %s\n", data.AuthorName)
	}
	b.WriteString("\nNote: YouTube descriptions and comments are not available via static fetch.")
	return PageHit{Title: data.Title, URL: pageURL, Content: b.String()}, nil
}
