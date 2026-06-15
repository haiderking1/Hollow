package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/session"
	"github.com/gorilla/websocket"
)

// WsClientAttachment matches user prompt base64 mime layout
type WsClientAttachment struct {
	MIME string `json:"mime"`
	Data string `json:"data"` // base64
}

// WsClientMessage represents messages incoming from client
type WsClientMessage struct {
	Type        string               `json:"type"`
	Text        string               `json:"text,omitempty"`
	ID          string               `json:"id,omitempty"`
	Attachments []WsClientAttachment `json:"attachments,omitempty"`
}

// WsServerMessage represents client responses (extensible structure)
type WsServerMessage struct {
	Type      string      `json:"type,omitempty"`
	Kind      string      `json:"Kind,omitempty"`
	Data      interface{} `json:"Data,omitempty"`
	Text      string      `json:"text,omitempty"`
	ID        string      `json:"id,omitempty"`
	Name      string      `json:"name,omitempty"`
	Arguments string      `json:"arguments,omitempty"`
	Status    string      `json:"status,omitempty"`
	Result    string      `json:"result,omitempty"`
	Sessions  interface{} `json:"sessions,omitempty"`
	SessionID string      `json:"sessionId,omitempty"`
	Messages  interface{} `json:"messages,omitempty"`
	Message   string      `json:"message,omitempty"`
}

// SessionResponse mirrors frontend Session object
type SessionResponse struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt"`
}

// WsHistoryMessage matches chat message shape loaded in picking picker
type WsHistoryMessage struct {
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Timestamp string          `json:"timestamp"`
	Tools     []WsHistoryTool `json:"tools,omitempty"`
}

// WsHistoryTool matches tool schemas mapped in client panels
type WsHistoryTool struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Status    string `json:"status"`
	Result    string `json:"result,omitempty"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // allow cli tools (websocat)
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "" {
			return true
		}
		return false
	},
}

func mapSessionInfo(info session.Info) SessionResponse {
	return SessionResponse{
		ID:        info.ID,
		Title:     info.FirstMessage,
		CreatedAt: session.FormatRelative(info.Modified),
	}
}

func runServeCLI() {
	addr := "127.0.0.1:8754"
	for i := 2; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--ws" {
			if i+1 < len(os.Args) {
				addr = os.Args[i+1]
				i++
			} else {
				fmt.Fprintln(os.Stderr, "Error: --ws option requires an argument")
				os.Exit(1)
			}
		} else if strings.HasPrefix(arg, "--ws=") {
			addr = strings.TrimPrefix(arg, "--ws=")
		} else {
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", arg)
			os.Exit(1)
		}
	}

	// Restrict to localhost only
	if !strings.HasPrefix(addr, "127.0.0.1:") && !strings.HasPrefix(addr, "localhost:") && !strings.HasPrefix(addr, "[::1]:") {
		parts := strings.Split(addr, ":")
		port := "8754"
		if len(parts) > 1 {
			port = parts[len(parts)-1]
		}
		addr = "127.0.0.1:" + port
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	server := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handleConnection(w, r)
		}),
	}

	go func() {
		<-stopChan
		fmt.Fprintln(os.Stderr, "\nShutting down WebSocket server...")
		server.Close()
		os.Exit(0)
	}()

	// Print exact listener port format requested
	fmt.Printf("listening on ws://%s\n", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
		os.Exit(1)
	}
}

func handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	connCtx, cancelConn := context.WithCancel(context.Background())
	defer cancelConn()

	sendCh := make(chan interface{}, 256)

	// Writer goroutine: serialize socket writes
	go func() {
		defer conn.Close()
		for {
			select {
			case <-connCtx.Done():
				return
			case msg, ok := <-sendCh:
				if !ok {
					return
				}
				err := conn.WriteJSON(msg)
				if err != nil {
					cancelConn()
					return
				}
			}
		}
	}()

	sendCh <- WsServerMessage{Type: "ready"}

	cfg, err := config.LoadRuntime()
	if err != nil {
		sendCh <- WsServerMessage{Type: "error", Message: err.Error()}
		return
	}

	sm, err := session.ContinueRecent("")
	if err != nil {
		sendCh <- WsServerMessage{Type: "error", Message: err.Error()}
		return
	}

	ag := agent.New(cfg, "", sm)

	var promptingMu sync.Mutex
	var prompting bool
	var promptCtx context.Context
	var cancelPrompt context.CancelFunc

	// Cleanup prompt/agent states on connection termination
	defer func() {
		promptingMu.Lock()
		if cancelPrompt != nil {
			cancelPrompt()
		}
		promptingMu.Unlock()
		ag.Abort()
		close(sendCh)
	}()

	// Reader loop
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var msg WsClientMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			sendCh <- WsServerMessage{Type: "error", Message: "Invalid JSON payload"}
			continue
		}

		switch msg.Type {
		case "listSessions":
			infos, err := session.ListAll()
			if err != nil {
				sendCh <- WsServerMessage{Type: "error", Message: err.Error()}
				continue
			}
			var list []SessionResponse
			for _, info := range infos {
				list = append(list, mapSessionInfo(info))
			}
			sendCh <- WsServerMessage{Type: "session.list", Sessions: list}

		case "openSession":
			infos, _ := session.ListAll()
			var targetPath string
			for _, info := range infos {
				if info.ID == msg.ID || info.Path == msg.ID {
					targetPath = info.Path
					break
				}
			}
			if targetPath == "" {
				targetPath = msg.ID
			}

			newSm, err := session.Open(targetPath)
			if err != nil {
				sendCh <- WsServerMessage{Type: "error", Message: err.Error()}
				continue
			}

			ag.LoadSession(newSm)

			var history []WsHistoryMessage
			for _, line := range newSm.ChatLines() {
				if line.Role == "user" {
					history = append(history, WsHistoryMessage{
						ID:        fmt.Sprintf("msg-%d", len(history)),
						Role:      "user",
						Content:   line.Text,
						Timestamp: "Just now",
					})
				} else if line.Role == "assistant" {
					history = append(history, WsHistoryMessage{
						ID:        fmt.Sprintf("msg-%d", len(history)),
						Role:      "assistant",
						Content:   line.Text,
						Timestamp: "Just now",
					})
				} else if line.Role == "tool" {
					if len(history) > 0 && history[len(history)-1].Role == "assistant" {
						lastIdx := len(history) - 1
						status := "completed"
						if line.ToolError {
							status = "failed"
						}
						history[lastIdx].Tools = append(history[lastIdx].Tools, WsHistoryTool{
							ID:        fmt.Sprintf("tool-%d", len(history[lastIdx].Tools)),
							Name:      line.ToolName,
							Arguments: line.ToolArgs,
							Status:    status,
							Result:    line.ToolResult,
						})
					}
				} else if line.Role == "system" || line.Role == "error" {
					history = append(history, WsHistoryMessage{
						ID:        fmt.Sprintf("msg-%d", len(history)),
						Role:      "system",
						Content:   line.Text,
						Timestamp: "Just now",
					})
				}
			}

			sendCh <- WsServerMessage{
				Type:      "session.history",
				SessionID: newSm.SessionID(),
				Messages:  history,
			}

		case "prompt":
			var userAtts []agent.UserAttachment
			for _, att := range msg.Attachments {
				dec, err := base64.StdEncoding.DecodeString(att.Data)
				if err != nil {
					continue
				}
				userAtts = append(userAtts, agent.UserAttachment{
					MIMEType: att.MIME,
					Data:     dec,
				})
			}

			promptingMu.Lock()
			if prompting {
				promptingMu.Unlock()
				sendCh <- WsServerMessage{Type: "error", Message: "agent is already processing"}
				continue
			}
			prompting = true
			promptCtx, cancelPrompt = context.WithCancel(connCtx)
			promptingMu.Unlock()

			// Run prompt in a separate goroutine so reads keep flowing
			go func(text string, atts []agent.UserAttachment, pCtx context.Context) {
				defer func() {
					promptingMu.Lock()
					prompting = false
					cancelPrompt = nil
					promptingMu.Unlock()
					sendCh <- WsServerMessage{Type: "done"}
				}()

				err := ag.Prompt(pCtx, cfg, text, atts, func(e core.Event) {
					var wsMsg WsServerMessage
					wsMsg.Kind = e.Kind
					wsMsg.Data = e.Data

					switch e.Kind {
					case core.EventAssistantStart:
						wsMsg.Type = "token"
					case core.EventAssistantThinkingDelta, core.EventAssistantDelta:
						wsMsg.Type = "token"
						if delta, ok := e.Data.(string); ok {
							wsMsg.Text = delta
						}
					case core.EventToolStart:
						wsMsg.Type = "tool"
						if ev, ok := e.Data.(core.ToolCallEvent); ok {
							wsMsg.ID = ev.ID
							wsMsg.Name = ev.Name
							wsMsg.Arguments = ev.Args
							wsMsg.Status = "running"
						}
					case core.EventToolDelta:
						wsMsg.Type = "tool"
						if ev, ok := e.Data.(core.ToolCallEvent); ok {
							wsMsg.ID = ev.ID
							wsMsg.Name = ev.Name
							wsMsg.Arguments = ev.Args
							wsMsg.Status = "running"
							wsMsg.Result = ev.Result
						}
					case core.EventToolResult:
						wsMsg.Type = "tool"
						if ev, ok := e.Data.(core.ToolCallEvent); ok {
							wsMsg.ID = ev.ID
							wsMsg.Name = ev.Name
							wsMsg.Arguments = ev.Args
							wsMsg.Status = "completed"
							if ev.Error {
								wsMsg.Status = "failed"
							}
							wsMsg.Result = ev.Result
						}
					case core.EventError:
						wsMsg.Type = "error"
						if txt, ok := e.Data.(string); ok {
							wsMsg.Message = txt
						}
					}

					select {
					case sendCh <- wsMsg:
					case <-pCtx.Done():
					}
				})

				if err != nil {
					if err.Error() != "context canceled" {
						select {
						case sendCh <- WsServerMessage{Type: "error", Message: err.Error()}:
						case <-pCtx.Done():
						}
					}
				}
			}(msg.Text, userAtts, promptCtx)

		case "interrupt":
			promptingMu.Lock()
			if cancelPrompt != nil {
				cancelPrompt()
			}
			promptingMu.Unlock()
			ag.Abort()
		}
	}
}
