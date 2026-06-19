package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dop251/goja"
	"github.com/enough/enough/backend/core"
)

func (r *Runtime) sdkObject(vm *goja.Runtime, loop *vmLoop, ctx context.Context) *goja.Object {
	sdk := vm.NewObject()
	_ = sdk.Set("today", func(goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().Format("2006-01-02"))
	})
	_ = sdk.Set("log", func(call goja.FunctionCall) goja.Value {
		level := call.Argument(0).String()
		message := call.Argument(1).String()
		r.emitRun(core.EventWorkflowPhase, level+": "+message)
		return goja.Undefined()
	})
	_ = sdk.Set("emit", func(call goja.FunctionCall) goja.Value {
		message := call.Argument(0).String()
		r.emitRun(core.EventWorkflowPhase, message)
		return goja.Undefined()
	})
	_ = sdk.Set("runBash", func(call goja.FunctionCall) goja.Value {
		command := call.Argument(0).String()
		return loop.async(ctx, func() (any, error) {
			return r.runBash(ctx, command)
		})
	})
	_ = sdk.Set("fetchJSON", func(call goja.FunctionCall) goja.Value {
		command := call.Argument(0).String()
		return loop.async(ctx, func() (any, error) {
			result, err := r.runBash(ctx, command)
			if err != nil {
				return nil, err
			}
			if result.ExitCode != 0 {
				return nil, fmt.Errorf("command exited %d: %s", result.ExitCode, result.Stderr)
			}
			var value any
			if err := json.Unmarshal([]byte(result.Stdout), &value); err != nil {
				return nil, fmt.Errorf("fetchJSON: %w", err)
			}
			return value, nil
		})
	})
	_ = sdk.Set("spawnAgent", func(call goja.FunctionCall) goja.Value {
		var opts AgentOptions
		if err := exportJSONValue(call.Argument(0), &opts); err != nil {
			panic(vm.NewGoError(fmt.Errorf("spawnAgent options: %w", err)))
		}
		if opts.Key == "" {
			opts.Key = fmt.Sprintf("%s:%d", defaultRole(opts.Role), time.Now().UnixNano())
		}
		return loop.async(ctx, func() (any, error) {
			if err := r.acquireDirect(ctx); err != nil {
				return nil, err
			}
			defer r.releaseDirect()
			result := r.executeJob(ctx, r.currentPhase(), opts.Key, opts)
			if isQuotaError(result.Error) {
				r.pauseForQuota()
				r.cancelActiveAgents()
				r.cancelRun()
				return nil, ErrQuotaPaused
			}
			return result, nil
		})
	})
	_ = sdk.Set("pipeline", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(fmt.Errorf("pipeline requires input and at least one stage")))
		}
		input := call.Argument(0).Export()
		stages := make([]goja.Callable, 0, len(call.Arguments)-1)
		for _, value := range call.Arguments[1:] {
			fn, ok := goja.AssertFunction(value)
			if !ok {
				panic(vm.NewGoError(fmt.Errorf("pipeline stage is not a function")))
			}
			stages = append(stages, fn)
		}
		result, err := r.runPipeline(ctx, vm, input, stages)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return resolvedPromise(vm, result)
	})
	return sdk
}

func (r *Runtime) cancelRun() {
	r.mu.RLock()
	cancel := r.cancel
	r.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

func resolvedPromise(vm *goja.Runtime, value any) goja.Value {
	promise, resolve, _ := vm.NewPromise()
	if err := resolve(jsonShape(value)); err != nil {
		panic(vm.NewGoError(err))
	}
	return vm.ToValue(promise)
}

func (r *Runtime) currentPhase() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.snapshot.Phase != "" {
		return r.snapshot.Phase
	}
	return "agent"
}

func (r *Runtime) acquireDirect(ctx context.Context) error {
	r.mu.RLock()
	sem := r.directSem
	r.mu.RUnlock()
	if sem == nil {
		return nil
	}
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) releaseDirect() {
	r.mu.RLock()
	sem := r.directSem
	r.mu.RUnlock()
	if sem != nil {
		<-sem
	}
}
