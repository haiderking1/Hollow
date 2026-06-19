package workflow

import (
	"context"
	"strings"

	"github.com/enough/enough/backend/core"
)

type poolResult struct {
	job    queuedJob
	result AgentResult
}

func (r *Runtime) runPool(ctx context.Context, phase string, jobs []queuedJob) ([]AgentResult, error) {
	if len(jobs) == 0 {
		return nil, nil
	}
	r.registerJobs(phase, jobs)
	pending := append([]queuedJob(nil), jobs...)
	results := make([]AgentResult, 0, len(jobs))
	resultCh := make(chan poolResult, len(jobs)+r.maxConcurrency)
	running := 0

	for len(pending) > 0 || running > 0 {
		if err := ctx.Err(); err != nil {
			r.cancelActiveAgents()
			drainPoolResults(resultCh, running)
			return results, err
		}

		r.mu.RLock()
		paused := r.paused
		concurrency := r.maxConcurrency
		r.mu.RUnlock()

		for !paused && running < concurrency && len(pending) > 0 {
			job := pending[0]
			pending = pending[1:]
			if completed, ok := r.completedResult(job.Key); ok {
				results = append(results, completed)
				r.markCompletedFromCheckpoint(job, completed)
				continue
			}
			running++
			go func(job queuedJob) {
				resultCh <- poolResult{job: job, result: r.executeJob(ctx, phase, job.Key, job.Options)}
			}(job)
		}

		if running == 0 {
			if len(pending) == 0 {
				break
			}
			select {
			case <-ctx.Done():
				r.cancelActiveAgents()
				return results, ctx.Err()
			case <-r.wake:
				continue
			}
		}

		select {
		case <-ctx.Done():
			r.cancelActiveAgents()
			drainPoolResults(resultCh, running)
			return results, ctx.Err()
		case <-r.wake:
			continue
		case item := <-resultCh:
			running--
			if r.takeRestart(item.job.Key) {
				r.resetQueued(item.job)
				pending = append([]queuedJob{item.job}, pending...)
				continue
			}
			if isQuotaError(item.result.Error) {
				r.pauseForQuota()
				r.cancelActiveAgents()
				drainPoolResults(resultCh, running)
				return results, ErrQuotaPaused
			}
			results = append(results, item.result)
		}
	}
	return results, nil
}

func drainPoolResults(resultCh <-chan poolResult, count int) {
	for count > 0 {
		<-resultCh
		count--
	}
}

func (r *Runtime) registerJobs(phase string, jobs []queuedJob) {
	r.mu.Lock()
	r.ensurePhaseLocked(phase)
	for _, job := range jobs {
		if _, exists := r.snapshot.Agents[job.Key]; exists {
			continue
		}
		r.snapshot.Agents[job.Key] = AgentSnapshot{
			Key: job.Key, Phase: phase, Role: defaultRole(job.Options.Role),
			Status: "queued", Prompt: job.Options.Prompt,
		}
	}
	r.recountLocked()
	r.mu.Unlock()
	r.persist()
	r.emitRun(core.EventWorkflowPhase, phase)
}

func (r *Runtime) completedResult(key string) (AgentResult, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result, ok := r.completed[key]
	return result, ok
}

func (r *Runtime) markCompletedFromCheckpoint(job queuedJob, result AgentResult) {
	r.mu.Lock()
	s := r.snapshot.Agents[job.Key]
	s.Status = "done"
	if !result.OK {
		s.Status = "failed"
	}
	s.Result, s.JSON, s.Error = result.Text, result.JSON, result.Error
	s.Tokens, s.Turns = result.TokensUsed, result.TurnCount
	r.snapshot.Agents[job.Key] = s
	r.recountLocked()
	r.mu.Unlock()
}

func (r *Runtime) takeRestart(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	restart := r.restartRequested[key]
	delete(r.restartRequested, key)
	return restart
}

func (r *Runtime) resetQueued(job queuedJob) {
	r.mu.Lock()
	s := r.snapshot.Agents[job.Key]
	s.Status = "queued"
	s.Error = ""
	s.Result = ""
	s.JSON = nil
	r.snapshot.Agents[job.Key] = s
	delete(r.completed, job.Key)
	r.recountLocked()
	r.mu.Unlock()
	r.persist()
}

func (r *Runtime) pauseForQuota() {
	r.mu.Lock()
	r.paused = true
	r.pauseReason = "provider quota exceeded"
	r.snapshot.Status = "paused"
	r.snapshot.Message = "provider quota exceeded"
	if r.state != nil {
		r.state.Status = "paused"
		r.state.PauseReason = "provider quota exceeded"
	}
	r.mu.Unlock()
	r.persist()
}

func (r *Runtime) cancelActiveAgents() {
	r.mu.RLock()
	var cancels []context.CancelFunc
	for _, control := range r.active {
		cancels = append(cancels, control.cancel)
	}
	r.mu.RUnlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (r *Runtime) ensurePhaseLocked(name string) {
	for _, phase := range r.snapshot.Phases {
		if phase.Name == name {
			return
		}
	}
	r.snapshot.Phases = append(r.snapshot.Phases, PhaseSnapshot{Name: name})
}

func (r *Runtime) recountLocked() {
	r.snapshot.Queued, r.snapshot.Running, r.snapshot.Done, r.snapshot.Failed, r.snapshot.Tokens = 0, 0, 0, 0, 0
	phases := map[string]*PhaseSnapshot{}
	for i := range r.snapshot.Phases {
		p := &r.snapshot.Phases[i]
		p.Total, p.Queued, p.Running, p.Done, p.Failed, p.Tokens = 0, 0, 0, 0, 0, 0
		phases[p.Name] = p
	}
	for _, agent := range r.snapshot.Agents {
		phase := phases[agent.Phase]
		if phase == nil {
			continue
		}
		phase.Total++
		phase.Tokens += agent.Tokens
		r.snapshot.Tokens += agent.Tokens
		switch agent.Status {
		case "queued":
			phase.Queued++
			r.snapshot.Queued++
		case "running":
			phase.Running++
			r.snapshot.Running++
		case "done":
			phase.Done++
			r.snapshot.Done++
		case "failed", "stopped":
			phase.Failed++
			r.snapshot.Failed++
		}
	}
}

func isQuotaError(text string) bool {
	if text == "" {
		return false
	}
	for _, needle := range []string{"rate limit", "rate_limit", "quota", "usage limit", "too many requests", "status 429", "http 429"} {
		if containsFold(text, needle) {
			return true
		}
	}
	return false
}

func containsFold(s, needle string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(needle))
}
