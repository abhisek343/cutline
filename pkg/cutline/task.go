package cutline

import (
	"context"
	"errors"

	"github.com/abhisek343/cutline/internal/model"
)

type Task struct {
	done chan struct{}
	err  error
}

// Spawn registers a child before launching it. The child owns its checkpoint,
// effect, resource, and descendant evidence while sharing its parent's
// cancellation scope. Run drains registered descendants after the root returns.
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
	childScope := &scopeState{
		session:  scope.session,
		id:       model.TaskID(value),
		parentID: scope.id,
		cancel:   scope.cancel,
	}
	if err := scope.session.reserveTask(childScope.id, scope.id); err != nil {
		return nil, err
	}
	if _, err := scope.session.emit(ctx, model.EventTaskRegistered, value, string(scope.id), map[string]string{
		"name": name,
		"kind": "native.child",
	}); err != nil {
		scope.session.finishTask(childScope.id, err)
		return nil, err
	}
	childCtx := context.WithValue(ctx, scopeContextKey{}, childScope)
	go task.run(childCtx, fn, childScope, value)
	return task, nil
}

func (t *Task) run(ctx context.Context, fn func(context.Context) error, scope *scopeState, entityID string) {
	defer close(t.done)
	if scope != nil {
		if _, err := scope.session.emit(context.WithoutCancel(ctx), model.EventTaskStarted, entityID, string(scope.parentID), nil); err != nil {
			t.err = err
			scope.session.finishTask(scope.id, err)
			return
		}
	}
	t.err = fn(ctx)
	if scope != nil {
		scope.session.closeTaskScope(scope.id)
		attributes := map[string]string{"status": classifyTerminal(t.err, ctx)}
		if t.err != nil {
			attributes["error"] = t.err.Error()
		}
		_, evidenceErr := scope.session.emit(context.WithoutCancel(ctx), model.EventTaskFinished, entityID, string(scope.parentID), attributes)
		if evidenceErr != nil {
			t.err = errors.Join(t.err, evidenceErr)
		}
		scope.session.finishTask(scope.id, evidenceErr)
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
