package workflow

import (
	"context"
	"fmt"

	"github.com/dop251/goja"
	"github.com/enough/enough/backend/core"
)

type queuedJob struct {
	Key     string
	Phase   string
	Options AgentOptions
}

func (r *Runtime) runPipeline(ctx context.Context, vm *goja.Runtime, input any, stages []goja.Callable) (PipelineResult, error) {
	out := PipelineResult{
		Input: input, Results: map[string]AgentResult{},
	}
	var previous []AgentResult
	for index, stage := range stages {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		phase := r.phaseName(index)
		r.setPhase(phase, index)
		stageCtx := map[string]any{
			"input":           input,
			"previousResults": previous,
			"results":         out.Results,
			"stageIndex":      index,
			"phase":           phase,
		}
		value, err := stage(goja.Undefined(), vm.ToValue(jsonShape(stageCtx)))
		if err != nil {
			return out, fmt.Errorf("pipeline stage %s: %w", phase, err)
		}
		value, err = unwrapPromise(value)
		if err != nil {
			return out, fmt.Errorf("pipeline stage %s: %w", phase, err)
		}
		jobs, err := normalizeJobs(vm, value, phase)
		if err != nil {
			return out, fmt.Errorf("pipeline stage %s: %w", phase, err)
		}
		results, err := r.runPool(ctx, phase, jobs)
		if err != nil {
			return out, err
		}
		previous = results
		out.Stages = append(out.Stages, StageResult{Name: phase, Results: results})
		for _, result := range results {
			out.Results[result.Key] = result
		}
	}
	return out, nil
}

func normalizeJobs(vm *goja.Runtime, value goja.Value, phase string) ([]queuedJob, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, nil
	}
	var raw []map[string]any
	if err := exportJSONValue(value, &raw); err != nil {
		return nil, fmt.Errorf("stage must return an array of subjobs: %w", err)
	}
	jobs := make([]queuedJob, 0, len(raw))
	for index, item := range raw {
		optionsMap := item
		if nested, ok := item["options"].(map[string]any); ok {
			optionsMap = nested
			if _, exists := optionsMap["key"]; !exists {
				optionsMap["key"] = item["key"]
			}
		}
		var opts AgentOptions
		dataValue := vm.ToValue(optionsMap)
		if err := exportJSONValue(dataValue, &opts); err != nil {
			return nil, fmt.Errorf("subjob %d: %w", index, err)
		}
		if opts.Role == "" {
			opts.Role = phase
		}
		if opts.Key == "" {
			opts.Key = fmt.Sprintf("%s:%d", phase, index+1)
		}
		if opts.Prompt == "" {
			return nil, fmt.Errorf("subjob %s has no prompt", opts.Key)
		}
		jobs = append(jobs, queuedJob{Key: opts.Key, Phase: phase, Options: opts})
	}
	return jobs, nil
}

func (r *Runtime) phaseName(index int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if index >= 0 && index < len(r.meta.Phases) && r.meta.Phases[index] != "" {
		return r.meta.Phases[index]
	}
	return fmt.Sprintf("stage-%d", index+1)
}

func (r *Runtime) setPhase(phase string, index int) {
	r.mu.Lock()
	r.snapshot.Phase = phase
	if r.state != nil {
		r.state.Phase = phase
		r.state.StageIndex = index
	}
	r.ensurePhaseLocked(phase)
	r.mu.Unlock()
	r.persist()
	r.emitRun(core.EventWorkflowPhase, phase)
}
