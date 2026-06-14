package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)


var (
	upgrader     = websocket.Upgrader{}
	wsReceivedMu sync.Mutex
	wsReceived   []string
)

func handleMockWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var writeMu sync.Mutex
	writeMessage := func(messageType int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(messageType, data)
	}

	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			return
		}

		wsReceivedMu.Lock()
		wsReceived = append(wsReceived, string(p))
		wsReceivedMu.Unlock()

		var req struct {
			ID     int64                  `json:"id"`
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}
		if err := json.Unmarshal(p, &req); err != nil {
			continue
		}

		var resp struct {
			ID     int64       `json:"id"`
			Result interface{} `json:"result,omitempty"`
			Error  interface{} `json:"error,omitempty"`
		}
		resp.ID = req.ID

		if req.Method == "Runtime.evaluate" {
			expr, _ := req.Params["expression"].(string)
			if strings.Contains(expr, "document.title") {
				type evalResult struct {
					Result struct {
						Type  string      `json:"type"`
						Value interface{} `json:"value"`
					} `json:"result"`
				}
				var r evalResult
				r.Result.Type = "string"
				r.Result.Value = "hello"
				resp.Result = r
			} else {
				type evalResult struct {
					Result struct {
						Type  string      `json:"type"`
						Value interface{} `json:"value"`
					} `json:"result"`
				}
				var r evalResult
				r.Result.Type = "string"
				probeBytes, _ := json.Marshal(map[string]interface{}{
					"x":          120.0,
					"y":          240.0,
					"tag":        "BUTTON",
					"className":  "submit",
					"text":       "Download for Windows",
					"selector":   "button.submit",
					"index":      0,
					"matchCount": 1,
				})
				r.Result.Value = string(probeBytes)
				resp.Result = r
			}
		} else if req.Method == "Input.dispatchMouseEvent" {
			resp.Result = map[string]interface{}{}

			mType, _ := req.Params["type"].(string)
			if mType == "mouseReleased" {
				go func() {
					time.Sleep(50 * time.Millisecond)
					type eventMsg struct {
						Method string      `json:"method"`
						Params interface{} `json:"params"`
					}
					evt, _ := json.Marshal(eventMsg{
						Method: "Page.downloadWillBegin",
						Params: map[string]string{
							"suggestedFilename": "fdm_x64_setup.exe",
							"url":               "https://files2.freedownloadmanager.org/6/latest/fdm_x64_setup.exe",
						},
					})
					_ = writeMessage(websocket.TextMessage, evt)
				}()
			}
		} else {
			resp.Result = map[string]interface{}{}
		}

		respBytes, _ := json.Marshal(resp)
		_ = writeMessage(messageType, respBytes)
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

	mux.HandleFunc("/json/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		wsUrl := "ws://" + r.Host + "/devtools/page/TAB2"
		w.Write([]byte(fmt.Sprintf(`{
			"id": "TAB2",
			"title": "",
			"url": "about:blank",
			"type": "page",
			"webSocketDebuggerUrl": "%s"
		}`, wsUrl)))
	})

	mux.HandleFunc("/json/close/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Target closed"))
	})

	mux.HandleFunc("/json/activate/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Target activated"))
	})

	mux.HandleFunc("/devtools/page/", handleMockWS)

	return httptest.NewServer(mux)
}

func TestBrowserTool(t *testing.T) {
	server := setupTestServer()
	defer server.Close()

	os.Setenv("ENOUGH_BROWSER_AUTO_LAUNCH", "0")
	os.Setenv("ENOUGH_BROWSER_CDP_URL", server.URL)
	defer os.Unsetenv("ENOUGH_BROWSER_AUTO_LAUNCH")
	defer os.Unsetenv("ENOUGH_BROWSER_CDP_URL")

	ctx := context.Background()
	cwd, _ := os.Getwd()

	// 1. list
	out, details, err := ExecuteBrowser(ctx, cwd, BrowserArgs{Action: "list"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(out, "TAB1") {
		t.Errorf("expected TAB1 in list output, got: %s", out)
	}
	if len(details.Tabs) != 1 || details.Tabs[0].ID != "TAB1" {
		t.Errorf("expected 1 tab with ID TAB1, got: %v", details.Tabs)
	}

	// 2. open
	out, details, err = ExecuteBrowser(ctx, cwd, BrowserArgs{Action: "open", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	if !strings.Contains(out, "TAB2") {
		t.Errorf("expected TAB2 in open output, got: %s", out)
	}

	// 3. eval document.title
	out, _, err = ExecuteBrowser(ctx, cwd, BrowserArgs{Action: "eval", TabID: "TAB1", Expression: "document.title"})
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected hello in eval result, got: %s", out)
	}

	// 4. click selector with download feedback
	wsReceivedMu.Lock()
	wsReceived = nil
	wsReceivedMu.Unlock()

	out, _, err = ExecuteBrowser(ctx, cwd, BrowserArgs{Action: "eval", TabID: "TAB1", Selector: "button.submit"})
	if err != nil {
		t.Fatalf("click failed: %v", err)
	}
	if !strings.Contains(out, "Clicked element:") {
		t.Errorf("expected Clicked element in click output, got: %s", out)
	}
	if !strings.Contains(out, "Download for Windows") {
		t.Errorf("expected text in click output, got: %s", out)
	}
	if !strings.Contains(out, "download started: fdm_x64_setup.exe") {
		t.Errorf("expected download started info in click output, got: %s", out)
	}

	wsReceivedMu.Lock()
	foundDispatch := false
	for _, msg := range wsReceived {
		if strings.Contains(msg, "Input.dispatchMouseEvent") {
			foundDispatch = true
			break
		}
	}
	wsReceivedMu.Unlock()
	if !foundDispatch {
		t.Errorf("expected at least one Input.dispatchMouseEvent message, got none")
	}

	// 5. reject jQuery contains
	_, _, err = ExecuteBrowser(ctx, cwd, BrowserArgs{Action: "eval", TabID: "TAB1", Selector: "a:contains('Download')"})
	if err == nil {
		t.Errorf("expected contains selector to fail")
	} else if !strings.Contains(err.Error(), "jQuery syntax") {
		t.Errorf("expected jQuery syntax error, got: %v", err)
	}
}
