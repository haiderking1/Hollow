package workflow

import (
	"encoding/json"
	"time"
)

const (
	DefaultMaxConcurrency = 16
	DefaultMaxTotalAgents = 1000
)

type Meta struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Phases         []string `json:"phases,omitempty"`
	MaxConcurrency int      `json:"maxConcurrency,omitempty"`
	MaxTotalAgents int      `json:"maxTotalAgents,omitempty"`
}

type AgentOptions struct {
	Key            string         `json:"key,omitempty"`
	Role           string         `json:"role"`
	Prompt         string         `json:"prompt"`
	SystemPrompt   string         `json:"systemPrompt,omitempty"`
	Tools          []string       `json:"tools,omitempty"`
	Model          string         `json:"model,omitempty"`
	ResponseSchema map[string]any `json:"responseSchema,omitempty"`
	MaxTurns       int            `json:"maxTurns,omitempty"` // ignored; agents run until done or cancelled
	Readonly       bool           `json:"readonly,omitempty"`
}

type AgentResult struct {
	Key        string `json:"key,omitempty"`
	Role       string `json:"role,omitempty"`
	OK         bool   `json:"ok"`
	Text       string `json:"text"`
	JSON       any    `json:"json,omitempty"`
	Error      string `json:"error,omitempty"`
	TokensUsed int    `json:"tokensUsed,omitempty"`
	TurnCount  int    `json:"turnCount,omitempty"`
}

type PipelineResult struct {
	Input   any                    `json:"input,omitempty"`
	Stages  []StageResult          `json:"stages"`
	Results map[string]AgentResult `json:"results"`
}

type StageResult struct {
	Name    string        `json:"name"`
	Results []AgentResult `json:"results"`
}

type BashResult struct {
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	ExitCode       int    `json:"exitCode"`
	Truncated      bool   `json:"truncated,omitempty"`
	FullOutputPath string `json:"fullOutputPath,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
}

type AgentSnapshot struct {
	Key       string    `json:"key"`
	Phase     string    `json:"phase"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	Prompt    string    `json:"prompt"`
	LastTools []string  `json:"lastTools,omitempty"`
	Result    string    `json:"result,omitempty"`
	JSON      any       `json:"json,omitempty"`
	Error     string    `json:"error,omitempty"`
	Tokens    int       `json:"tokens,omitempty"`
	Turns     int       `json:"turns,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
}

type PhaseSnapshot struct {
	Name    string `json:"name"`
	Total   int    `json:"total"`
	Queued  int    `json:"queued"`
	Running int    `json:"running"`
	Done    int    `json:"done"`
	Failed  int    `json:"failed"`
	Tokens  int    `json:"tokens"`
}

type Snapshot struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	ScriptPath  string                   `json:"scriptPath"`
	Status      string                   `json:"status"`
	Phase       string                   `json:"phase"`
	Phases      []PhaseSnapshot          `json:"phases"`
	Agents      map[string]AgentSnapshot `json:"agents"`
	Queued      int                      `json:"queued"`
	Running     int                      `json:"running"`
	Done        int                      `json:"done"`
	Failed      int                      `json:"failed"`
	Tokens      int                      `json:"tokens"`
	StartedAt   time.Time                `json:"startedAt"`
	EndedAt     time.Time                `json:"endedAt,omitempty"`
	Message     string                   `json:"message,omitempty"`
}

type RunOptions struct {
	ID    string
	Args  string
	Force bool
}

type RunResult struct {
	ID     string `json:"id"`
	Meta   Meta   `json:"meta"`
	Value  any    `json:"value,omitempty"`
	Status string `json:"status"`
}

func cloneJSON[T any](v T) T {
	data, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(data, &out)
	return out
}
