package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

var (
	testUpgrader = websocket.Upgrader{}
)

func handleMockWS(w http.ResponseWriter, r *http.Request) {
	conn, err := testUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
	}
}

func setupTestServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		wsUrl := "ws://" + r.Host + "/devtools/page/TAB1"
		w.Write([]byte(fmt.Sprintf(`[
			{
				"id": "TAB1",
				"title": "Example",
				"url": "https://example.com",
				"type": "page",
				"webSocketDebuggerUrl": "%s"
			}
		]`, wsUrl)))
	})
	mux.HandleFunc("/devtools/page/", handleMockWS)
	return httptest.NewServer(mux)
}

func TestToolBrowserOutputExcludesMetadata(t *testing.T) {
	server := setupTestServer()
	defer server.Close()

	os.Setenv("ENOUGH_BROWSER_AUTO_LAUNCH", "0")
	os.Setenv("ENOUGH_BROWSER_CDP_URL", server.URL)
	defer os.Unsetenv("ENOUGH_BROWSER_AUTO_LAUNCH")
	defer os.Unsetenv("ENOUGH_BROWSER_CDP_URL")

	a := &Agent{workDir: "."}
	res := a.toolBrowser(context.Background(), `{"action":"list"}`)
	if res.isErr {
		t.Fatalf("toolBrowser failed: %s", res.output)
	}

	if strings.Contains(res.output, "--METADATA--") {
		t.Errorf("output should not contain legacy metadata marker, got: %s", res.output)
	}

	if len(res.details) == 0 {
		t.Errorf("details should be populated and non-empty")
	}

	var details struct {
		Action string `json:"action"`
		Tabs   []struct {
			ID string `json:"id"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(res.details, &details); err != nil {
		t.Fatalf("failed to unmarshal details: %v", err)
	}

	if details.Action != "list" {
		t.Errorf("expected action 'list', got: %s", details.Action)
	}
	if len(details.Tabs) != 1 || details.Tabs[0].ID != "TAB1" {
		t.Errorf("expected 1 tab with ID TAB1, got: %v", details.Tabs)
	}
}
