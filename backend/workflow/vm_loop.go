package workflow

import (
	"context"
	"errors"
	"sync"

	"github.com/dop251/goja"
)

type vmLoop struct {
	vm       *goja.Runtime
	dispatch chan func()
	wg       sync.WaitGroup
}

func newVMLoop(vm *goja.Runtime) *vmLoop {
	return &vmLoop{vm: vm, dispatch: make(chan func(), 1024)}
}

func (l *vmLoop) async(ctx context.Context, work func() (any, error)) goja.Value {
	promise, resolve, reject := l.vm.NewPromise()
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		value, err := work()
		select {
		case <-ctx.Done():
			err = ctx.Err()
		default:
		}
		select {
		case l.dispatch <- func() {
			if err != nil {
				_ = reject(err.Error())
			} else {
				_ = resolve(jsonShape(value))
			}
		}:
		case <-ctx.Done():
		}
	}()
	return l.vm.ToValue(promise)
}

func (l *vmLoop) wait() {
	l.wg.Wait()
}

func (l *vmLoop) await(ctx context.Context, value goja.Value) (goja.Value, error) {
	if value == nil {
		return goja.Undefined(), nil
	}
	promise, ok := value.Export().(*goja.Promise)
	if !ok {
		return value, nil
	}
	for promise.State() == goja.PromiseStatePending {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case fn := <-l.dispatch:
			fn()
		}
	}
	switch promise.State() {
	case goja.PromiseStateFulfilled:
		return promise.Result(), nil
	case goja.PromiseStateRejected:
		return nil, errors.New(promise.Result().String())
	default:
		return nil, errors.New("promise remained pending")
	}
}
