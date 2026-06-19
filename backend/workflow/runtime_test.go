package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
)

func writeWorkflow(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.js")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testRuntime(t *testing.T, path string) *Runtime {
	t.Helper()
	return NewRuntime(config.Runtime{Model: "fake"}, filepath.Dir(path))
}

func TestPipelineDynamicStagesAndConcurrency(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = { name: "dynamic", description: "test", phases: ["audit", "verify"], maxConcurrency: 3 };
export async function run(sdk) {
  return sdk.pipeline(
    [{id:1},{id:2},{id:3},{id:4},{id:5}],
    async ({input}) => input.map(x => ({
      key: "audit:" + x.id, role: "audit", prompt: String(x.id),
      responseSchema: {type:"object", required:["verify"], properties:{verify:{type:"boolean"}}}
    })),
    async ({previousResults}) => previousResults
      .filter(r => r.json.verify)
      .map(r => ({key:"verify:" + r.key, role:"verify", prompt:r.key}))
  );
}`)
	rt := testRuntime(t, path)
	var active atomic.Int32
	var maximum atomic.Int32
	var calls atomic.Int32
	rt.agentRunner = func(ctx context.Context, phase, key string, opts AgentOptions, emit func(core.Event)) AgentResult {
		calls.Add(1)
		now := active.Add(1)
		for {
			old := maximum.Load()
			if now <= old || maximum.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		if phase == "audit" {
			id := strings.TrimPrefix(key, "audit:")
			return AgentResult{Key: key, Role: opts.Role, Text: fmt.Sprintf(`{"verify":%v}`, id == "2" || id == "4")}
		}
		return AgentResult{Key: key, Role: opts.Role, Text: "verified"}
	}
	result, err := rt.Run(context.Background(), path, RunOptions{ID: "dynamic"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "done" {
		t.Fatalf("status = %s", result.Status)
	}
	if calls.Load() != 7 {
		t.Fatalf("calls = %d, want 7", calls.Load())
	}
	if maximum.Load() != 3 {
		t.Fatalf("max concurrent = %d, want 3", maximum.Load())
	}
	s := rt.Snapshot()
	if s.Done != 7 || s.Queued != 0 || s.Running != 0 {
		t.Fatalf("snapshot counts = %+v", s)
	}
}

func TestSpawnAgentPromisesRunConcurrently(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = { name: "promises", description: "test", maxConcurrency: 4 };
export async function run(sdk) {
  return Promise.all(Array.from({length:8}, (_,i) =>
    sdk.spawnAgent({key:"direct:" + i, role:"custom", prompt:String(i)})
  ));
}`)
	rt := testRuntime(t, path)
	var active atomic.Int32
	var maximum atomic.Int32
	rt.agentRunner = func(ctx context.Context, phase, key string, opts AgentOptions, emit func(core.Event)) AgentResult {
		now := active.Add(1)
		for {
			old := maximum.Load()
			if now <= old || maximum.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return AgentResult{Key: key, Role: opts.Role, Text: key}
	}
	if _, err := rt.Run(context.Background(), path, RunOptions{ID: "promises"}, nil); err != nil {
		t.Fatal(err)
	}
	if maximum.Load() != 4 {
		t.Fatalf("max concurrent = %d, want 4", maximum.Load())
	}
}

func TestSchemaRetryOnce(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = { name: "schema", description: "test" };
export async function run(sdk) {
  return sdk.spawnAgent({
    key:"one", role:"audit", prompt:"return JSON",
    responseSchema:{type:"object", required:["value"], properties:{value:{type:"integer"}}}
  });
}`)
	rt := testRuntime(t, path)
	var calls atomic.Int32
	rt.agentRunner = func(context.Context, string, string, AgentOptions, func(core.Event)) AgentResult {
		if calls.Add(1) == 1 {
			return AgentResult{Text: "not json"}
		}
		return AgentResult{Text: `{"value": 7}`}
	}
	result, err := rt.Run(context.Background(), path, RunOptions{ID: "schema"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	value := result.Value.(map[string]any)
	if value["ok"] != true {
		t.Fatalf("result = %#v", value)
	}
}

func TestDirectSpawnQuotaPausesWorkflow(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = { name:"direct-quota", description:"test" };
export async function run(sdk) {
  return sdk.spawnAgent({key:"one",role:"custom",prompt:"one"});
}`)
	rt := testRuntime(t, path)
	rt.agentRunner = func(context.Context, string, string, AgentOptions, func(core.Event)) AgentResult {
		return AgentResult{Error: "usage limit quota exceeded"}
	}
	if _, err := rt.Run(context.Background(), path, RunOptions{ID: "direct-quota"}, nil); err == nil {
		t.Fatal("expected quota pause")
	}
	if rt.Snapshot().Status != "paused" {
		t.Fatalf("status = %s", rt.Snapshot().Status)
	}
}

func TestInvalidScriptSpawnsZeroAgents(t *testing.T) {
	path := writeWorkflow(t, `export const meta = {name:"bad",description:"bad"}; export async function run(sdk) {`)
	rt := testRuntime(t, path)
	var calls atomic.Int32
	rt.agentRunner = func(context.Context, string, string, AgentOptions, func(core.Event)) AgentResult {
		calls.Add(1)
		return AgentResult{}
	}
	if _, err := rt.Run(context.Background(), path, RunOptions{ID: "bad"}, nil); err == nil {
		t.Fatal("expected parse error")
	}
	if calls.Load() != 0 {
		t.Fatalf("spawned %d agents", calls.Load())
	}
}

func TestCancellationInterruptsInfiniteJavaScript(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = {name:"infinite",description:"test"};
export async function run(sdk) { while (true) {} }
`)
	rt := testRuntime(t, path)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := rt.Run(ctx, path, RunOptions{ID: "infinite"}, nil); err == nil {
		t.Fatal("expected cancellation")
	}
	if time.Since(start) > time.Second {
		t.Fatal("JavaScript VM did not interrupt promptly")
	}
}

func TestGojaSandboxAndArgs(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = { name:"sandbox", description:"test" };
export async function run(sdk) {
  return {require:typeof require, process:typeof process, fetch:typeof fetch, args};
}`)
	rt := testRuntime(t, path)
	result, err := rt.Run(context.Background(), path, RunOptions{ID: "sandbox", Args: `[1,2,3]`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	value := result.Value.(map[string]any)
	for _, key := range []string{"require", "process", "fetch"} {
		if value[key] != "undefined" {
			t.Fatalf("%s = %v", key, value[key])
		}
	}
	if got := len(value["args"].([]any)); got != 3 {
		t.Fatalf("args length = %d", got)
	}
}

func TestCheckpointResumeSkipsCompletedKeys(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = { name:"resume", description:"test", phases:["audit"], maxConcurrency:1 };
export async function run(sdk) {
  return sdk.pipeline({}, async () => [
    {key:"audit:a",role:"audit",prompt:"a"},
    {key:"audit:b",role:"audit",prompt:"b"}
  ]);
}`)
	first := testRuntime(t, path)
	first.agentRunner = func(ctx context.Context, phase, key string, opts AgentOptions, emit func(core.Event)) AgentResult {
		if key == "audit:b" {
			return AgentResult{Key: key, Role: opts.Role, Error: "status 429: quota exceeded"}
		}
		return AgentResult{Key: key, Role: opts.Role, Text: "done"}
	}
	if _, err := first.Run(context.Background(), path, RunOptions{ID: "resume"}, nil); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("expected quota pause, got %v", err)
	}
	state, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Completed["audit:a"]; !ok {
		t.Fatalf("checkpoint missing completed key: %#v", state.Completed)
	}

	second := testRuntime(t, path)
	var mu sync.Mutex
	var keys []string
	second.agentRunner = func(ctx context.Context, phase, key string, opts AgentOptions, emit func(core.Event)) AgentResult {
		mu.Lock()
		keys = append(keys, key)
		mu.Unlock()
		return AgentResult{Key: key, Role: opts.Role, Text: "done"}
	}
	if _, err := second.Run(context.Background(), path, RunOptions{ID: "resume"}, nil); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(keys) != "[audit:b]" {
		t.Fatalf("resumed keys = %v, want only audit:b", keys)
	}
}

func TestCancelMidPool(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = { name:"cancel", description:"test", phases:["work"], maxConcurrency:2 };
export async function run(sdk) {
  return sdk.pipeline({}, async () => Array.from({length:8}, (_,i) => ({
    key:"work:" + i, role:"custom", prompt:String(i)
  })));
}`)
	rt := testRuntime(t, path)
	started := make(chan struct{}, 8)
	rt.agentRunner = func(ctx context.Context, phase, key string, opts AgentOptions, emit func(core.Event)) AgentResult {
		started <- struct{}{}
		<-ctx.Done()
		return AgentResult{Key: key, Role: opts.Role, Error: ctx.Err().Error()}
	}
	done := make(chan error, 1)
	go func() {
		_, err := rt.Run(context.Background(), path, RunOptions{ID: "cancel"}, nil)
		done <- err
	}()
	<-started
	rt.Cancel()
	if err := <-done; err == nil {
		t.Fatal("expected cancellation")
	}
	if rt.Snapshot().Status != "cancelled" {
		t.Fatalf("status = %s", rt.Snapshot().Status)
	}
}

func TestPauseStopsBackfillUntilResume(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = { name:"pause", description:"test", phases:["work"], maxConcurrency:1 };
export async function run(sdk) {
  return sdk.pipeline({}, async () => [1,2,3].map(i => ({key:"work:"+i,role:"custom",prompt:String(i)})));
}`)
	rt := testRuntime(t, path)
	started := make(chan string, 3)
	release := make(chan struct{})
	rt.agentRunner = func(ctx context.Context, phase, key string, opts AgentOptions, emit func(core.Event)) AgentResult {
		started <- key
		if key == "work:1" {
			<-release
		}
		return AgentResult{Key: key, Role: opts.Role, Text: "done"}
	}
	done := make(chan error, 1)
	go func() {
		_, err := rt.Run(context.Background(), path, RunOptions{ID: "pause"}, nil)
		done <- err
	}()
	if key := <-started; key != "work:1" {
		t.Fatalf("first key = %s", key)
	}
	rt.Pause()
	close(release)
	select {
	case key := <-started:
		t.Fatalf("started %s while paused", key)
	case <-time.After(50 * time.Millisecond):
	}
	rt.Resume()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRestartRunningAgentUsesSameKey(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = { name:"restart", description:"test", phases:["work"], maxConcurrency:1 };
export async function run(sdk) {
  return sdk.pipeline({}, async () => [{key:"work:1",role:"custom",prompt:"one"}]);
}`)
	rt := testRuntime(t, path)
	started := make(chan struct{}, 2)
	var calls atomic.Int32
	rt.agentRunner = func(ctx context.Context, phase, key string, opts AgentOptions, emit func(core.Event)) AgentResult {
		attempt := calls.Add(1)
		started <- struct{}{}
		if attempt == 1 {
			<-ctx.Done()
			return AgentResult{Key: key, Role: opts.Role, Error: ctx.Err().Error()}
		}
		return AgentResult{Key: key, Role: opts.Role, Text: "done"}
	}
	done := make(chan error, 1)
	go func() {
		_, err := rt.Run(context.Background(), path, RunOptions{ID: "restart"}, nil)
		done <- err
	}()
	<-started
	if !rt.RestartAgent("work:1") {
		t.Fatal("restart did not find running agent")
	}
	<-started
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || rt.Snapshot().Done != 1 {
		t.Fatalf("calls=%d snapshot=%+v", calls.Load(), rt.Snapshot())
	}
}

func TestCheckpointAndStopDoesNotCompleteCancelledAgent(t *testing.T) {
	path := writeWorkflow(t, `
export const meta = { name:"checkpoint-stop", description:"test", phases:["work"], maxConcurrency:1 };
export async function run(sdk) {
  return sdk.pipeline({}, async () => [{key:"work:1",role:"custom",prompt:"one"}]);
}`)
	rt := testRuntime(t, path)
	started := make(chan struct{})
	rt.agentRunner = func(ctx context.Context, phase, key string, opts AgentOptions, emit func(core.Event)) AgentResult {
		close(started)
		<-ctx.Done()
		return AgentResult{Key: key, Role: opts.Role, Error: ctx.Err().Error()}
	}
	done := make(chan error, 1)
	go func() {
		_, err := rt.Run(context.Background(), path, RunOptions{ID: "checkpoint-stop"}, nil)
		done <- err
	}()
	<-started
	rt.CheckpointAndStop("user")
	<-done
	state, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "paused" {
		t.Fatalf("status = %s", state.Status)
	}
	if _, ok := state.Completed["work:1"]; ok {
		t.Fatal("cancelled in-flight agent was incorrectly checkpointed as completed")
	}
}

func TestRunBashTruncatesAndPersistsFullOutput(t *testing.T) {
	path := writeWorkflow(t, `export const meta={name:"bash",description:"test"}; export async function run(sdk){return 1}`)
	rt := testRuntime(t, path)
	rt.snapshot.ScriptPath = path
	result, err := rt.runBash(context.Background(), "head -c 100000 /dev/zero | tr '\\0' x")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || result.FullOutputPath == "" || result.SHA256 == "" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(result.FullOutputPath); err != nil {
		t.Fatal(err)
	}
}
