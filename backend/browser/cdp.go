package browser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type CdpTab struct {
	ID                   string `json:"id"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	Type                 string `json:"type"`
	WebSocketDebuggerUrl string `json:"webSocketDebuggerUrl"`
}

type cdpResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
}

type cdpError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type cdpMessage struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
}

type eventSub struct {
	id      int64
	handler func(interface{})
}

type CdpSession struct {
	ws              *websocket.Conn
	nextId          int64
	pending         map[int64]chan cdpResponse
	pendingMu       sync.Mutex
	eventHandlers   map[string][]eventSub
	nextSubID       int64
	eventHandlersMu sync.Mutex
	closed          bool
	closeOnce       sync.Once
	tabID           string
}

var (
	sessionCache   = make(map[string]*CdpSession)
	sessionCacheMu sync.Mutex
)

func getBrowserCdpBaseUrl() string {
	u := strings.TrimSpace(os.Getenv("ENOUGH_BROWSER_CDP_URL"))
	if u == "" {
		return "http://127.0.0.1:9222"
	}
	return u
}

func assertAllowedCdpUrl(baseUrl string) (*url.URL, error) {
	parsed, err := url.Parse(baseUrl)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("Browser CDP URL must be http(s): %s", baseUrl)
	}
	allowRemote := os.Getenv("ENOUGH_BROWSER_ALLOW_REMOTE") == "1"
	host := strings.ToLower(parsed.Hostname())
	isLocal := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if !isLocal && !allowRemote {
		return nil, fmt.Errorf("Browser CDP URL must be localhost (set ENOUGH_BROWSER_ALLOW_REMOTE=1 for remote debugging): %s", baseUrl)
	}
	return parsed, nil
}

func cdpHttpRequest(baseUrl, path, method string, allowLaunch bool) (*http.Response, error) {
	_, err := assertAllowedCdpUrl(baseUrl)
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, err
	}
	rel, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	fullUrl := u.ResolveReference(rel).String()

	req, err := http.NewRequest(method, fullUrl, nil)
	if err != nil {
		return nil, err
	}
	if method == "PUT" {
		req.Header.Set("Content-Length", "0")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if allowLaunch && ShouldAutoLaunchBrowser() && isCdpConnectionError(err) {
			launched, lerr := ensureBrowserLaunched(baseUrl)
			if lerr != nil {
				return nil, fmt.Errorf("%s", formatCdpConnectionError(baseUrl, lerr, true))
			}
			if launched {
				return cdpHttpRequest(baseUrl, path, method, false)
			}
		}
		return nil, fmt.Errorf("%s", formatCdpConnectionError(baseUrl, err, false))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("Browser CDP HTTP %d for %s", resp.StatusCode, path)
	}
	return resp, nil
}

func ListCdpTabs(baseUrl string) ([]CdpTab, error) {
	resp, err := cdpHttpRequest(baseUrl, "/json/list", "GET", true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tabs []CdpTab
	if err := json.NewDecoder(resp.Body).Decode(&tabs); err != nil {
		return nil, err
	}
	var filtered []CdpTab
	for _, t := range tabs {
		if t.ID != "" && t.WebSocketDebuggerUrl != "" {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

func OpenCdpTab(rawUrl string, baseUrl string) (CdpTab, error) {
	path := "/json/new"
	if rawUrl != "" {
		path += "?" + url.QueryEscape(rawUrl)
	}
	resp, err := cdpHttpRequest(baseUrl, path, "PUT", true)
	if err != nil {
		return CdpTab{}, err
	}
	defer resp.Body.Close()
	var tab CdpTab
	if err := json.NewDecoder(resp.Body).Decode(&tab); err != nil {
		return CdpTab{}, err
	}
	if tab.ID == "" || tab.WebSocketDebuggerUrl == "" {
		return CdpTab{}, fmt.Errorf("Browser CDP did not return a new tab descriptor")
	}
	return tab, nil
}

func CloseCdpTab(tabId string, baseUrl string) error {
	sessionCacheMu.Lock()
	if session, ok := sessionCache[tabId]; ok {
		session.Close()
		delete(sessionCache, tabId)
	}
	sessionCacheMu.Unlock()

	resp, err := cdpHttpRequest(baseUrl, "/json/close/"+url.PathEscape(tabId), "GET", true)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func ActivateCdpTab(tabId string, baseUrl string) error {
	resp, err := cdpHttpRequest(baseUrl, "/json/activate/"+url.PathEscape(tabId), "GET", true)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func ResolveCdpTab(tabId string, baseUrl string) (CdpTab, error) {
	tabs, err := ListCdpTabs(baseUrl)
	if err != nil {
		return CdpTab{}, err
	}
	if len(tabs) == 0 {
		return CdpTab{}, fmt.Errorf("No browser tabs are available on the CDP endpoint")
	}
	if tabId == "" {
		for _, t := range tabs {
			if t.Type == "page" {
				return t, nil
			}
		}
		return tabs[0], nil
	}
	for _, t := range tabs {
		if t.ID == tabId {
			return t, nil
		}
	}
	var available []string
	for _, t := range tabs {
		available = append(available, t.ID)
	}
	return CdpTab{}, fmt.Errorf("Tab %s was not found. Available tabs: %s", tabId, strings.Join(available, ", "))
}

func clearCdpSessionCache() {
	sessionCacheMu.Lock()
	defer sessionCacheMu.Unlock()
	for _, s := range sessionCache {
		s.Close()
	}
	sessionCache = make(map[string]*CdpSession)
}

func connectCdpSession(tab CdpTab) (*CdpSession, error) {
	sessionCacheMu.Lock()
	cached, exists := sessionCache[tab.ID]
	if exists && !cached.closed {
		sessionCacheMu.Unlock()
		return cached, nil
	}
	sessionCacheMu.Unlock()

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}
	ws, _, err := dialer.Dial(tab.WebSocketDebuggerUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to browser tab %s: %w", tab.ID, err)
	}

	session := &CdpSession{
		ws:            ws,
		nextId:        1,
		pending:       make(map[int64]chan cdpResponse),
		eventHandlers: make(map[string][]eventSub),
		tabID:         tab.ID,
	}

	go session.readLoop()

	sessionCacheMu.Lock()
	sessionCache[tab.ID] = session
	sessionCacheMu.Unlock()

	return session, nil
}

func (s *CdpSession) readLoop() {
	for {
		_, msgBytes, err := s.ws.ReadMessage()
		if err != nil {
			s.failPending(err)
			s.closeOnce.Do(func() {
				s.closed = true
				s.ws.Close()
			})
			return
		}

		var msg cdpMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			continue
		}

		if msg.ID != nil {
			id := *msg.ID
			s.pendingMu.Lock()
			ch, exists := s.pending[id]
			if exists {
				delete(s.pending, id)
			}
			s.pendingMu.Unlock()

			if exists {
				ch <- cdpResponse{
					ID:     id,
					Result: msg.Result,
					Error:  msg.Error,
				}
				close(ch)
			}
		} else if msg.Method != "" {
			s.eventHandlersMu.Lock()
			handlers, exists := s.eventHandlers[msg.Method]
			var handlersCopy []func(interface{})
			if exists {
				for _, h := range handlers {
					handlersCopy = append(handlersCopy, h.handler)
				}
			}
			s.eventHandlersMu.Unlock()

			var params interface{}
			if len(msg.Params) > 0 {
				_ = json.Unmarshal(msg.Params, &params)
			}
			for _, h := range handlersCopy {
				go h(params)
			}
		}
	}
}

func (s *CdpSession) Close() {
	s.closeOnce.Do(func() {
		s.closed = true
		s.ws.Close()
		s.failPending(fmt.Errorf("Browser CDP session closed"))
	})
}

func (s *CdpSession) Send(method string, params interface{}) (json.RawMessage, error) {
	s.pendingMu.Lock()
	if s.closed {
		s.pendingMu.Unlock()
		return nil, fmt.Errorf("Browser CDP session is closed")
	}
	id := s.nextId
	s.nextId++
	ch := make(chan cdpResponse, 1)
	s.pending[id] = ch
	s.pendingMu.Unlock()

	type requestPayload struct {
		ID     int64       `json:"id"`
		Method string      `json:"method"`
		Params interface{} `json:"params,omitempty"`
	}

	reqBytes, err := json.Marshal(requestPayload{
		ID:     id,
		Method: method,
		Params: params,
	})
	if err != nil {
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return nil, err
	}

	if err := s.ws.WriteMessage(websocket.TextMessage, reqBytes); err != nil {
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return nil, err
	}

	resp := <-ch
	if resp.Error != nil {
		return nil, fmt.Errorf("%s", resp.Error.Message)
	}
	return resp.Result, nil
}

func (s *CdpSession) OnEvent(method string, handler func(interface{})) func() {
	s.eventHandlersMu.Lock()
	subID := s.nextSubID
	s.nextSubID++
	s.eventHandlers[method] = append(s.eventHandlers[method], eventSub{id: subID, handler: handler})
	s.eventHandlersMu.Unlock()

	return func() {
		s.eventHandlersMu.Lock()
		defer s.eventHandlersMu.Unlock()
		subs := s.eventHandlers[method]
		for i, sub := range subs {
			if sub.id == subID {
				s.eventHandlers[method] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}
}

func (s *CdpSession) failPending(err error) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	for id, ch := range s.pending {
		ch <- cdpResponse{
			ID:    id,
			Error: &cdpError{Message: err.Error()},
		}
		close(ch)
	}
	s.pending = make(map[int64]chan cdpResponse)
}

func withCdpSession(tabId string, baseUrl string, fn func(session *CdpSession, tab CdpTab) (interface{}, error)) (interface{}, error) {
	tab, err := ResolveCdpTab(tabId, baseUrl)
	if err != nil {
		return nil, err
	}
	session, err := connectCdpSession(tab)
	if err != nil {
		return nil, err
	}
	return fn(session, tab)
}

func EvaluateExpression(session *CdpSession, expression string, awaitPromise bool) (interface{}, error) {
	_, err := session.Send("Runtime.enable", nil)
	if err != nil {
		return nil, err
	}

	type evalParams struct {
		Expression    string `json:"expression"`
		AwaitPromise  bool   `json:"awaitPromise"`
		ReturnByValue bool   `json:"returnByValue"`
		UserGesture   bool   `json:"userGesture"`
	}

	params := evalParams{
		Expression:    expression,
		AwaitPromise:  awaitPromise,
		ReturnByValue: true,
		UserGesture:   true,
	}

	resRaw, err := session.Send("Runtime.evaluate", params)
	if err != nil {
		return nil, err
	}

	type exceptionDesc struct {
		Description string `json:"description"`
	}
	type exceptionDetails struct {
		Text      string         `json:"text"`
		Exception *exceptionDesc `json:"exception,omitempty"`
	}
	type evalResult struct {
		Result struct {
			Type        string      `json:"type"`
			Value       interface{} `json:"value,omitempty"`
			Description string      `json:"description,omitempty"`
		} `json:"result"`
		ExceptionDetails *exceptionDetails `json:"exceptionDetails,omitempty"`
	}

	var evalRes evalResult
	if err := json.Unmarshal(resRaw, &evalRes); err != nil {
		return nil, err
	}

	if evalRes.ExceptionDetails != nil {
		detail := evalRes.ExceptionDetails.Text
		if evalRes.ExceptionDetails.Exception != nil && evalRes.ExceptionDetails.Exception.Description != "" {
			detail = evalRes.ExceptionDetails.Exception.Description
		}
		if detail == "" {
			detail = "eval failed"
		}
		return nil, fmt.Errorf("%s", detail)
	}

	remote := evalRes.Result
	if remote.Type == "undefined" {
		return nil, nil
	}
	if remote.Value != nil {
		return remote.Value, nil
	}
	if remote.Type == "object" && remote.Description != "" {
		var parsed interface{}
		if err := json.Unmarshal([]byte(remote.Description), &parsed); err == nil {
			return parsed, nil
		}
	}
	if remote.Description != "" {
		return remote.Description, nil
	}
	return nil, nil
}
