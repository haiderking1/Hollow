package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type State struct {
	Version     int                      `json:"version"`
	ID          string                   `json:"id"`
	ScriptPath  string                   `json:"scriptPath"`
	Args        string                   `json:"args,omitempty"`
	Meta        Meta                     `json:"meta"`
	Status      string                   `json:"status"`
	PauseReason string                   `json:"pauseReason,omitempty"`
	Phase       string                   `json:"phase,omitempty"`
	StageIndex  int                      `json:"stageIndex,omitempty"`
	Completed   map[string]AgentResult   `json:"completed"`
	Agents      map[string]AgentSnapshot `json:"agents,omitempty"`
	StartedAt   time.Time                `json:"startedAt"`
	UpdatedAt   time.Time                `json:"updatedAt"`
}

func StatePath(scriptPath string) string {
	return filepath.Join(filepath.Dir(scriptPath), "state.json")
}

func LoadState(scriptPath string) (*State, error) {
	data, err := os.ReadFile(StatePath(scriptPath))
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Completed == nil {
		state.Completed = map[string]AgentResult{}
	}
	if state.Agents == nil {
		state.Agents = map[string]AgentSnapshot{}
	}
	return &state, nil
}

func SaveState(state *State) error {
	if state == nil || state.ScriptPath == "" {
		return errors.New("workflow state has no script path")
	}
	state.Version = 1
	state.UpdatedAt = time.Now()
	if state.Completed == nil {
		state.Completed = map[string]AgentResult{}
	}
	if state.Agents == nil {
		state.Agents = map[string]AgentSnapshot{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := StatePath(state.ScriptPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func FindResumable(workDir, id string) (string, error) {
	root := filepath.Join(workDir, ".enough", "workflows")
	if id != "" {
		if filepath.Base(id) != id || id == "." || id == ".." {
			return "", errors.New("invalid workflow id")
		}
		path := filepath.Join(root, id, "workflow.js")
		state, err := LoadState(path)
		if err != nil {
			return "", err
		}
		if state.Status != "paused" {
			return "", errors.New("workflow is not paused")
		}
		return path, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var newest string
	var newestAt time.Time
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "saved" {
			continue
		}
		path := filepath.Join(root, entry.Name(), "workflow.js")
		state, err := LoadState(path)
		if err == nil && state.Status == "paused" && state.UpdatedAt.After(newestAt) {
			newest, newestAt = path, state.UpdatedAt
		}
	}
	if newest == "" {
		return "", errors.New("no paused workflow found")
	}
	return newest, nil
}

func ListStates(workDir string) []State {
	root := filepath.Join(workDir, ".enough", "workflows")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var states []State
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "saved" {
			continue
		}
		path := filepath.Join(root, entry.Name(), "workflow.js")
		if state, err := LoadState(path); err == nil {
			states = append(states, *state)
		}
	}
	sort.Slice(states, func(i, j int) bool { return states[i].UpdatedAt.After(states[j].UpdatedAt) })
	return states
}

func SnapshotFromState(state State) Snapshot {
	s := Snapshot{
		ID: state.ID, Name: state.Meta.Name, Description: state.Meta.Description,
		ScriptPath: state.ScriptPath, Status: state.Status, Phase: state.Phase,
		Agents: cloneJSON(state.Agents), StartedAt: state.StartedAt, Message: state.PauseReason,
	}
	if s.Agents == nil {
		s.Agents = map[string]AgentSnapshot{}
	}
	for _, name := range state.Meta.Phases {
		s.Phases = append(s.Phases, PhaseSnapshot{Name: name})
	}
	known := map[string]bool{}
	for _, phase := range s.Phases {
		known[phase.Name] = true
	}
	for _, item := range s.Agents {
		if !known[item.Phase] {
			s.Phases = append(s.Phases, PhaseSnapshot{Name: item.Phase})
			known[item.Phase] = true
		}
	}
	phaseByName := map[string]*PhaseSnapshot{}
	for i := range s.Phases {
		phaseByName[s.Phases[i].Name] = &s.Phases[i]
	}
	for _, item := range s.Agents {
		phase := phaseByName[item.Phase]
		phase.Total++
		phase.Tokens += item.Tokens
		s.Tokens += item.Tokens
		switch item.Status {
		case "queued":
			phase.Queued++
			s.Queued++
		case "running":
			phase.Running++
			s.Running++
		case "done":
			phase.Done++
			s.Done++
		case "failed", "stopped":
			phase.Failed++
			s.Failed++
		}
	}
	return s
}

type approvalFile struct {
	Names []string `json:"names"`
}

func projectApprovalPath(workDir string) string {
	return filepath.Join(workDir, ".enough", "workflows", "approvals.json")
}

func IsAlwaysApproved(workDir, name string) bool {
	data, err := os.ReadFile(projectApprovalPath(workDir))
	if err != nil {
		return false
	}
	var approvals approvalFile
	if json.Unmarshal(data, &approvals) != nil {
		return false
	}
	for _, item := range approvals.Names {
		if item == name {
			return true
		}
	}
	return false
}

func SetAlwaysApproved(workDir, name string) error {
	path := projectApprovalPath(workDir)
	var approvals approvalFile
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &approvals)
	}
	for _, item := range approvals.Names {
		if item == name {
			return nil
		}
	}
	approvals.Names = append(approvals.Names, name)
	sort.Strings(approvals.Names)
	data, err := json.MarshalIndent(approvals, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
