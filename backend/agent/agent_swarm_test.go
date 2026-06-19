package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/opencode"
)

func TestWorkerToolsIncludeCodingTools(t *testing.T) {
	names := toolNames(workerTools(0))
	for _, want := range []string{"read_file", "write_file", "edit_file", "bash", "web_search", "web_fetch", "browser", "agent_swarm"} {
		if !names[want] {
			t.Fatalf("worker at depth 0 missing %q", want)
		}
	}
}

func TestWorkerToolsCapNestingAtMaxDepth(t *testing.T) {
	names := toolNames(workerTools(maxSwarmDepth))
	if names["agent_swarm"] {
		t.Fatal("worker at max depth should not get agent_swarm")
	}
	for _, want := range []string{"read_file", "bash", "write_file"} {
		if !names[want] {
			t.Fatalf("worker at max depth missing %q", want)
		}
	}
}

func TestAgentSwarmToolRegistration(t *testing.T) {
	if !hasTool(nativeTools(config.Runtime{}), "agent_swarm") {
		t.Fatal("main agent is missing agent_swarm")
	}
	if hasTool(nativeTools(config.Runtime{}), "agent") {
		t.Fatal("main agent should not expose legacy agent tool")
	}
}

func TestSwarmArgsParseError(t *testing.T) {
	msg := swarmArgsParseError(fmt.Errorf("invalid character '\\n' in string literal"))
	if !strings.Contains(msg, "invalid JSON") {
		t.Fatalf("expected invalid JSON hint: %q", msg)
	}
	if !strings.Contains(msg, `\n`) {
		t.Fatalf("expected newline escape hint: %q", msg)
	}
}

func TestNativeToolsIncludeBrowser(t *testing.T) {
	if !hasTool(nativeTools(config.Runtime{}), "browser") {
		t.Fatal("nativeTools missing browser")
	}
}

func TestParsePlannedSwarmTasks(t *testing.T) {
	raw := `[{"id":"a","prompt":"do A"},{"id":"b","prompt":"do B","depends_on":["a"]}]`
	tasks := parsePlannedSwarmTasks(raw)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != "a" || tasks[1].DependsOn[0] != "a" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
}

func TestSwarmTaskIDDefaults(t *testing.T) {
	if got := swarmTaskID(swarmTask{}, 0); got != "agent-1" {
		t.Fatalf("expected agent-1, got %q", got)
	}
	if got := swarmTaskID(swarmTask{ID: "scout"}, 0); got != "scout" {
		t.Fatalf("expected scout, got %q", got)
	}
}

func TestAggregateSwarmOutput(t *testing.T) {
	out := aggregateSwarmOutput([]swarmWorkerResult{
		{ID: "a", Status: "ok", Output: "done", Turns: 2},
		{ID: "b", Status: "error", Error: "boom", Turns: 1},
	}, 2, "")
	if !strings.Contains(out, "1 ok") || !strings.Contains(out, "1 error") {
		t.Fatalf("unexpected header: %q", out)
	}
	if !strings.Contains(out, "## a [ok]") || !strings.Contains(out, "## b [error]") {
		t.Fatalf("missing worker sections: %q", out)
	}
}

func TestToolGlobAndGrep(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\nfunc Logger() {}\n")
	mustWrite(t, filepath.Join(dir, "sub", "util.go"), "package sub\nvar loggerName = 1\n")
	mustWrite(t, filepath.Join(dir, "readme.md"), "docs\n")

	a := &Agent{workDir: dir}

	glob := a.toolGlob(`{"pattern":"**/*.go"}`)
	if glob.isErr {
		t.Fatalf("glob error: %s", glob.output)
	}
	if !strings.Contains(glob.output, "main.go") || !strings.Contains(glob.output, filepath.ToSlash("sub/util.go")) {
		t.Fatalf("glob did not find go files: %q", glob.output)
	}

	grep := a.toolGrep(context.Background(), `{"pattern":"(?i)logger","include":"*.go"}`)
	if grep.isErr {
		t.Fatalf("grep error: %s", grep.output)
	}
	if !strings.Contains(grep.output, "main.go:2") || !strings.Contains(grep.output, "util.go:2") {
		t.Fatalf("grep missed matches: %q", grep.output)
	}
}

func TestResolveWorkerOutput(t *testing.T) {
	nested := "Ran 1 agent(s) at concurrency 1 — 1 ok.\n\n## child [ok] (1 turn)\nPAYLOAD:deep"
	parallel := strings.Join([]string{
		"Ran 2 agent(s) at concurrency 2 — 2 ok.",
		"",
		"## a [ok] (1 turn)",
		"PAYLOAD:a",
		"## b [ok] (1 turn)",
		"PAYLOAD:b",
	}, "\n")
	cases := map[string]struct {
		finalText string
		swarmOut  string
		want      string
	}{
		"final text wins":                   {"summary here", "PAYLOAD:deep", "summary here"},
		"empty falls back to swarm":         {"   ", nested, "PAYLOAD:deep"},
		"stub falls back to swarm":          {"Task complete.", nested, "PAYLOAD:deep"},
		"stub keeps parallel aggregate":     {"Done.", parallel, parallel},
		"empty keeps parallel aggregate":    {"", parallel, parallel},
		"both empty":                        {"", "", ""},
		"stub final trims plain swarm text": {"  done  ", "PAYLOAD:deep", "PAYLOAD:deep"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := resolveWorkerOutput(tc.finalText, tc.swarmOut); got != tc.want {
				t.Fatalf("resolveWorkerOutput(%q, %q) = %q, want %q", tc.finalText, tc.swarmOut, got, tc.want)
			}
		})
	}
}

func TestResolveSwarmReturnOutputCollapsesOnlySingleWorker(t *testing.T) {
	single := "Ran 1 agent(s) at concurrency 1 — 1 ok.\n\n## child [ok] (1 turn)\nPAYLOAD:one"
	multi := strings.Join([]string{
		"Ran 2 agent(s) at concurrency 2 — 2 ok.",
		"",
		"## first [ok] (1 turn)",
		"PAYLOAD:first",
		"",
		"## second [ok] (1 turn)",
		"PAYLOAD:second",
	}, "\n")
	if got := resolveSwarmReturnOutput(single); got != "PAYLOAD:one" {
		t.Fatalf("single worker should collapse to payload, got %q", got)
	}
	if got := resolveSwarmReturnOutput(multi); got != multi {
		t.Fatalf("multi-worker swarm should preserve aggregate:\n%s", got)
	}
}

func TestExtractSwarmPayload(t *testing.T) {
	out := strings.Join([]string{
		"agent_swarm: 1/3 agents finished",
		"Ran 3 agent(s) at concurrency 3 — 1 ok, 1 error, 1 aborted.",
		"",
		"## parent [ok] (1 turn ×2)",
		"Ran 2 agent(s) at concurrency 2 — 1 ok, 1 aborted.",
		"",
		"## child [ok] (1 turn)",
		"PAYLOAD:inner",
		"",
		"## sibling [aborted] (0 turns)",
		"Error: skipped: dependency failed",
		"",
		"## other [error] (1 turn)",
		"Error: boom",
	}, "\n")
	if got := extractSwarmPayload(out); got != "PAYLOAD:inner" {
		t.Fatalf("extractSwarmPayload = %q", got)
	}
	if got := extractSwarmPayload("plain text"); got != "" {
		t.Fatalf("plain text should not be treated as swarm payload: %q", got)
	}
}

// TestNestedSwarmPropagatesDeepPayload drives a real (mocked-transport) worker
// loop through several levels of nested agent_swarm. Each intermediate worker
// spawns one child and then finishes with EMPTY text, simulating a model that
// forgets to echo its child's result. The innermost worker returns a known
// payload; the test asserts that payload survives the trip all the way back to
// the outermost aggregated output.
func TestLinkedSwarmContextRespectsParentAbort(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	ctx, cancel := linkedSwarmContext(parent)
	defer cancel()

	parentCancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("swarm context did not cancel when parent aborted")
	}
}

func TestNestedSwarmPropagatesDeepPayloadStress(t *testing.T) {
	for i := 0; i < 100; i++ {
		t.Run(fmt.Sprintf("run-%d", i), func(t *testing.T) {
			testNestedSwarmPropagatesDeepPayload(t)
		})
	}
}

func TestNestedSwarmPropagatesWhenModelCallsSwarmAndReadFileSameTurn(t *testing.T) {
	const payload = "PAYLOAD:mixed-tools"
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "note.txt"), "side read")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req opencode.ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		last := req.Messages[len(req.Messages)-1]

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		writeChunk := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if last.Role == "tool" {
			writeChunk(streamChunkJSON(payload, nil))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		if strings.Contains(opencode.ContentString(last), "CHILD") {
			writeChunk(streamChunkJSON(payload, nil))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		args := `{"tasks":[{"id":"child","prompt":"CHILD"}]}`
		writeChunk(streamChunkJSON("", []toolCallJSON{
			{Index: 0, ID: "call-swarm", Type: "function", Function: toolFnJSON{Name: "agent_swarm", Arguments: args}},
			{Index: 1, ID: "call-read", Type: "function", Function: toolFnJSON{Name: "read_file", Arguments: `{"path":"note.txt"}`}},
		}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := &Agent{
		cfg:     config.Runtime{Model: "test-model"},
		client:  opencode.NewClient(srv.URL, "test-key", "test-model"),
		workDir: dir,
	}
	res := a.toolAgentSwarm(context.Background(), "outer", `{"tasks":[{"id":"root","prompt":"ROOT"}]}`, 0)
	if !strings.Contains(res.output, payload) {
		t.Fatalf("payload lost when swarm shared a turn with read_file:\n%s", res.output)
	}
}

func TestNestedSwarmPayloadSurvivesAbortedSibling(t *testing.T) {
	const payload = "PAYLOAD:partial"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req opencode.ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		last := req.Messages[len(req.Messages)-1]

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		writeChunk := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if strings.Contains(opencode.ContentString(last), "PAYLOAD-WORKER") {
			writeChunk(streamChunkJSON(payload, nil))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		if last.Role == "tool" {
			writeChunk(streamChunkJSON("Task complete.", nil))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		args := `{"tasks":[{"id":"payload","prompt":"PAYLOAD-WORKER"},{"id":"a","prompt":"blocked","depends_on":["b"]},{"id":"b","prompt":"blocked","depends_on":["a"]}],"max_concurrency":1}`
		writeChunk(streamChunkJSON("", []toolCallJSON{{
			Index: 0, ID: "call-swarm", Type: "function",
			Function: toolFnJSON{Name: "agent_swarm", Arguments: args},
		}}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := &Agent{
		cfg:     config.Runtime{Model: "test-model"},
		client:  opencode.NewClient(srv.URL, "test-key", "test-model"),
		workDir: t.TempDir(),
	}
	res := a.toolAgentSwarm(context.Background(), "outer", `{"tasks":[{"id":"root","prompt":"ROOT"}]}`, 0)
	if !strings.Contains(res.output, payload) {
		t.Fatalf("payload from ok sibling was lost:\n%s", res.output)
	}
}

func TestNestedSwarmMultiChildAllowsFinalModelProcessing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req opencode.ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		last := req.Messages[len(req.Messages)-1]

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		writeChunk := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}

		if last.Role == "tool" {
			text := opencode.ContentString(last)
			if strings.Contains(text, "PAYLOAD:a") && strings.Contains(text, "PAYLOAD:b") {
				writeChunk(streamChunkJSON("COMBINED: PAYLOAD:a, PAYLOAD:b", nil))
			} else {
				writeChunk(streamChunkJSON("missing payload", nil))
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

		switch text := opencode.ContentString(last); {
		case strings.Contains(text, "ROOT"):
			args := `{"tasks":[{"id":"a","prompt":"CHILD_A"},{"id":"b","prompt":"CHILD_B"}],"max_concurrency":2}`
			writeChunk(streamChunkJSON("", []toolCallJSON{{
				Index:    0,
				ID:       "call-swarm",
				Type:     "function",
				Function: toolFnJSON{Name: "agent_swarm", Arguments: args},
			}}))
		case strings.Contains(text, "CHILD_A"):
			writeChunk(streamChunkJSON("PAYLOAD:a", nil))
		case strings.Contains(text, "CHILD_B"):
			writeChunk(streamChunkJSON("PAYLOAD:b", nil))
		default:
			writeChunk(streamChunkJSON("", nil))
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := &Agent{
		cfg:     config.Runtime{Model: "test-model"},
		client:  opencode.NewClient(srv.URL, "test-key", "test-model"),
		workDir: t.TempDir(),
	}
	res := a.toolAgentSwarm(context.Background(), "outer", `{"tasks":[{"id":"root","prompt":"ROOT"}]}`, 0)
	if !strings.Contains(res.output, "COMBINED: PAYLOAD:a, PAYLOAD:b") {
		t.Fatalf("parent did not process parallel child output:\n%s", res.output)
	}
}

func TestNestedSwarmPropagatesDeepPayload(t *testing.T) {
	testNestedSwarmPropagatesDeepPayload(t)
}

func testNestedSwarmPropagatesDeepPayload(t *testing.T) {
	t.Helper()
	const payload = "PAYLOAD:level-deep"
	depthRe := regexp.MustCompile(`DEPTH=(\d+)`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req opencode.ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		last := req.Messages[len(req.Messages)-1]

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		writeChunk := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}

		// A tool result just came back: finish with empty text. This exercises
		// the resolveWorkerOutput fallback to the nested swarm output.
		if last.Role == "tool" {
			writeChunk(streamChunkJSON("", nil))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

		depth := 0
		if m := depthRe.FindStringSubmatch(opencode.ContentString(last)); len(m) == 2 {
			depth, _ = strconv.Atoi(m[1])
		}

		if depth <= 0 {
			// Innermost worker: hand back the payload directly.
			writeChunk(streamChunkJSON(payload, nil))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

		// Spawn exactly one child one level shallower via a nested swarm.
		args := fmt.Sprintf(`{"tasks":[{"id":"child","prompt":"DEPTH=%d"}]}`, depth-1)
		writeChunk(streamChunkJSON("", []toolCallJSON{{
			Index:    0,
			ID:       "call-swarm",
			Type:     "function",
			Function: toolFnJSON{Name: "agent_swarm", Arguments: args},
		}}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := &Agent{
		cfg:     config.Runtime{Model: "test-model"},
		client:  opencode.NewClient(srv.URL, "test-key", "test-model"),
		workDir: t.TempDir(),
	}

	// depth=3 is the deepest a worker chain can nest under maxSwarmDepth=3:
	// workers at swarmDepth 0,1,2 each spawn one child, the swarmDepth-3 worker
	// returns the payload. This matches the user-observed "5 levels" (main +
	// four worker levels).
	args := `{"tasks":[{"id":"root","prompt":"DEPTH=3"}]}`
	res := a.toolAgentSwarm(context.Background(), "outer", args, 0)

	if res.isErr {
		t.Fatalf("outer swarm errored: %s", res.output)
	}
	if !strings.Contains(res.output, payload) {
		t.Fatalf("deep payload %q did not propagate to outer output:\n%s", payload, res.output)
	}
}

// TestNestedSwarmPropagatesWhenModelReturnsStub verifies that even when the
// model would reply with a useless summary after a nested swarm (instead of
// echoing the payload), the short-circuit path still returns the child output.
func TestNestedSwarmPropagatesWhenModelReturnsStub(t *testing.T) {
	const payload = "PAYLOAD:stub-test"
	depthRe := regexp.MustCompile(`DEPTH=(\d+)`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req opencode.ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		last := req.Messages[len(req.Messages)-1]

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		writeChunk := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}

		if last.Role == "tool" {
			// Bad model: summarizes instead of echoing nested output.
			writeChunk(streamChunkJSON("Task complete.", nil))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

		depth := 0
		if m := depthRe.FindStringSubmatch(opencode.ContentString(last)); len(m) == 2 {
			depth, _ = strconv.Atoi(m[1])
		}
		if depth <= 0 {
			writeChunk(streamChunkJSON(payload, nil))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

		args := fmt.Sprintf(`{"tasks":[{"id":"child","prompt":"DEPTH=%d"}]}`, depth-1)
		writeChunk(streamChunkJSON("", []toolCallJSON{{
			Index:    0,
			ID:       "call-swarm",
			Type:     "function",
			Function: toolFnJSON{Name: "agent_swarm", Arguments: args},
		}}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := &Agent{
		cfg:     config.Runtime{Model: "test-model"},
		client:  opencode.NewClient(srv.URL, "test-key", "test-model"),
		workDir: t.TempDir(),
	}

	args := `{"tasks":[{"id":"root","prompt":"DEPTH=2"}]}`
	res := a.toolAgentSwarm(context.Background(), "outer", args, 0)
	if res.isErr {
		t.Fatalf("outer swarm errored: %s", res.output)
	}
	if !strings.Contains(res.output, payload) {
		t.Fatalf("payload lost when model returns stub summary:\n%s", res.output)
	}
}

// TestSwarmProgressCallbackIsRaceFree fans a single swarm call out to many
// parallel workers. The per-worker progress callback (onEach) used to append to
// a shared slice without synchronization, corrupting memory under load and
// surfacing as nondeterministic hangs at higher nesting depth. Run with -race
// to guard against a regression.
func TestSwarmProgressCallbackIsRaceFree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		b, _ := json.Marshal(streamChunkJSON("done", nil))
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := &Agent{
		cfg:     config.Runtime{Model: "test-model"},
		client:  opencode.NewClient(srv.URL, "test-key", "test-model"),
		workDir: t.TempDir(),
	}

	var tasks []string
	for i := 0; i < 24; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id":"t%d","prompt":"do %d"}`, i, i))
	}
	args := fmt.Sprintf(`{"tasks":[%s],"max_concurrency":12}`, strings.Join(tasks, ","))

	res := a.toolAgentSwarm(context.Background(), "outer", args, 0)
	if res.isErr {
		t.Fatalf("swarm errored: %s", res.output)
	}
	if !strings.Contains(res.output, "24 ok") {
		t.Fatalf("expected 24 ok workers, got:\n%s", res.output)
	}
}

func TestSwarmStreamErrorThenSuccessOnRetry(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			http.Error(w, "temporary overload", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		b, _ := json.Marshal(streamChunkJSON("recovered", nil))
		fmt.Fprintf(w, "data: %s\n\n", b)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := &Agent{
		cfg:     config.Runtime{Model: "test-model"},
		client:  opencode.NewClient(srv.URL, "test-key", "test-model"),
		workDir: t.TempDir(),
	}
	res := a.toolAgentSwarm(context.Background(), "outer", `{"tasks":[{"id":"flaky","prompt":"retry me"}],"retry":0}`, 0)
	if !strings.Contains(res.output, "recovered") {
		t.Fatalf("expected retry recovery, got:\n%s", res.output)
	}
	if requests < 2 {
		t.Fatalf("expected stream retry, got %d request(s)", requests)
	}
}

func TestAgentSwarmPathConflictDetection(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{workDir: dir}
	args := `{"tasks":[{"id":"a","prompt":"edit ` + "`same.go`" + `"},{"id":"b","prompt":"also edit same.go"}]}`
	res := a.toolAgentSwarm(context.Background(), "outer", args, 0)
	if !res.isErr || !strings.Contains(res.output, "both target path") {
		t.Fatalf("expected path conflict error, got err=%v output=%q", res.isErr, res.output)
	}
}

func TestAgentSwarmDifferentPathsAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		b, _ := json.Marshal(streamChunkJSON("done", nil))
		fmt.Fprintf(w, "data: %s\n\n", b)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := &Agent{
		cfg:     config.Runtime{Model: "test-model"},
		client:  opencode.NewClient(srv.URL, "test-key", "test-model"),
		workDir: t.TempDir(),
	}
	args := `{"tasks":[{"id":"a","prompt":"edit a.go"},{"id":"b","prompt":"edit b.go"}],"max_concurrency":1}`
	res := a.toolAgentSwarm(context.Background(), "outer", args, 0)
	if res.isErr || !strings.Contains(res.output, "2 ok") {
		t.Fatalf("expected different paths to run, got err=%v output=%q", res.isErr, res.output)
	}
}

func TestAgentSwarmWorktreeIsolationKeepsDirtyWorktrees(t *testing.T) {
	repo := initTempGitRepo(t)
	var mu sync.Mutex
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req opencode.ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		last := req.Messages[len(req.Messages)-1]
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		writeChunk := func(v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if last.Role == "tool" {
			writeChunk(streamChunkJSON("wrote file", nil))
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		prompt := opencode.ContentString(last)
		path := "a.txt"
		if strings.Contains(prompt, "b.txt") {
			path = "b.txt"
		}
		mu.Lock()
		seen[path] = true
		mu.Unlock()
		writeChunk(streamChunkJSON("", []toolCallJSON{{
			Index: 0, ID: "write", Type: "function",
			Function: toolFnJSON{Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q,"content":"%s"}`, path, path)},
		}}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := &Agent{
		cfg:     config.Runtime{Model: "test-model"},
		client:  opencode.NewClient(srv.URL, "test-key", "test-model"),
		workDir: repo,
	}
	args := `{"isolate":"worktree","tasks":[{"id":"one","prompt":"write a.txt"},{"id":"two","prompt":"write b.txt"}],"max_concurrency":2}`
	res := a.toolAgentSwarm(context.Background(), "outer", args, 0)
	if res.isErr || !strings.Contains(res.output, "worktree:") || !strings.Contains(res.output, "branch:") {
		t.Fatalf("expected dirty worktrees to be reported, got err=%v output=\n%s", res.isErr, res.output)
	}
	if _, err := os.Stat(filepath.Join(repo, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("root repo should not contain isolated a.txt, stat err=%v", err)
	}
	for _, file := range []string{"a.txt", "b.txt"} {
		mu.Lock()
		ok := seen[file]
		mu.Unlock()
		if !ok {
			t.Fatalf("server never saw write for %s", file)
		}
	}
	for _, line := range strings.Split(res.output, "\n") {
		if !strings.Contains(line, "worktree:") {
			continue
		}
		start := strings.Index(line, "worktree: ")
		end := strings.Index(line[start:], " · branch:")
		if start < 0 || end < 0 {
			t.Fatalf("malformed worktree line: %s", line)
		}
		wt := line[start+len("worktree: ") : start+end]
		if _, err := os.Stat(wt); err != nil {
			t.Fatalf("reported worktree missing: %s: %v", wt, err)
		}
	}
}

func TestAgentSwarmWorktreeIsolationFallsBackOutsideGit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		b, _ := json.Marshal(streamChunkJSON("ran in cwd", nil))
		fmt.Fprintf(w, "data: %s\n\n", b)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	a := &Agent{
		cfg:     config.Runtime{Model: "test-model"},
		client:  opencode.NewClient(srv.URL, "test-key", "test-model"),
		workDir: t.TempDir(),
	}
	res := a.toolAgentSwarm(context.Background(), "outer", `{"isolate":"worktree","tasks":[{"id":"x","prompt":"x"}]}`, 0)
	if res.isErr || !strings.Contains(res.output, "ran in cwd") || strings.Contains(res.output, "worktree:") {
		t.Fatalf("expected non-git fallback, got err=%v output=\n%s", res.isErr, res.output)
	}
}

func initTempGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, "README.md"), "root\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// streamChunk/toolCall JSON shims mirror the subset of the OpenAI-style SSE
// schema that ChatStream parses, so the test server can emit valid deltas.
type streamDeltaJSON struct {
	Role      string         `json:"role,omitempty"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []toolCallJSON `json:"tool_calls,omitempty"`
}

type toolCallJSON struct {
	Index    int        `json:"index"`
	ID       string     `json:"id,omitempty"`
	Type     string     `json:"type,omitempty"`
	Function toolFnJSON `json:"function"`
}

type toolFnJSON struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

func streamChunkJSON(content string, calls []toolCallJSON) map[string]any {
	return map[string]any{
		"choices": []map[string]any{{
			"delta": streamDeltaJSON{Role: "assistant", Content: content, ToolCalls: calls},
		}},
	}
}

func toolNames(tools []opencode.Tool) map[string]bool {
	m := make(map[string]bool, len(tools))
	for _, tl := range tools {
		m[tl.Function.Name] = true
	}
	return m
}

func hasTool(tools []opencode.Tool, name string) bool {
	return toolNames(tools)[name]
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
