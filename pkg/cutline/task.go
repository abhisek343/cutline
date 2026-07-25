package cutline

import (
	"context"
	"errors"
	"sync"

	"github.com/abhisek343/cutline/internal/model"
)

type Task struct {
	done chan struct{}
	once sync.Once
	err  error
}

// Spawn registers a child before launching it. The child shares its parent's
// cancellation scope and is therefore observable during drain.
func Spawn(ctx context.Context, name string, fn func(context.Context) error) (*Task, error) {
	if fn == nil {
		return nil, errors.New("cutline: task function is nil")
	}
	task := &Task{done: make(chan struct{})}
	scope, active := scopeFromContext(ctx)
	if !active {
		go task.run(ctx, fn, nil, "")
		return task, nil
	}
	value, err := scope.session.nextEntity("task", string(scope.id), name)
	if err != nil {
		return nil, err
	}
	if _, err := scope.session.emit(ctx, model.EventTaskRegistered, value, string(scope.id), map[string]string{
		"name": name,
		"kind": "native.child",
	}); err != nil {
		return nil, err
	}
	go task.run(ctx, fn, scope, value)
	return task, nil
}

func (t *Task) run(ctx context.Context, fn func(context.Context) error, scope *scopeState, entityID string) {
	defer close(t.done)
	if scope != nil {
		if _, err := scope.session.emit(context.WithoutCancel(ctx), model.EventTaskStarted, entityID, string(scope.id), nil); err != nil {
			t.err = err
			return
		}
	}
	t.err = fn(ctx)
	if scope != nil {
		attributes := map[string]string{"status": classifyTerminal(t.err, ctx)}
		if t.err != nil {
			attributes["error"] = t.err.Error()
		}
		if _, err := scope.session.emit(context.WithoutCancel(ctx), model.EventTaskFinished, entityID, string(scope.id), attributes); err != nil {
			t.err = errors.Join(t.err, err)
		}
	}
}

func (t *Task) Wait(ctx context.Context) error {
	select {
	case <-t.done:
		return t.err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}
