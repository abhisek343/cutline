package cutline

import (
	"context"
	"errors"
	"fmt"

	"github.com/abhisek343/cutline/internal/model"
)

// Run creates a coordinator-cancellable root scope and executes fn. In an
// active run it then waits for registered Spawn descendants within the campaign
// drain timeout. Incomplete drainage returns ErrDrainTimeout or an evidence
// error along with fn's error. Targets should wrap each independently testable
// operation in Run.
func Run(parent context.Context, name string, fn func(context.Context) error) (returnErr error) {
	if fn == nil {
		return errors.New("cutline: run function is nil")
	}
	session, active, err := sessionFromEnvironment(parent)
	if err != nil {
		return err
	}
	if !active {
		return fn(parent)
	}
	defer session.close()

	taskIDValue, err := session.nextEntity("task", name)
	if err != nil {
		return err
	}
	taskID := model.TaskID(taskIDValue)
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	scope := &scopeState{session: session, id: taskID, cancel: cancel}
	ctx = context.WithValue(ctx, scopeContextKey{}, scope)
	if err := session.reserveTask(taskID, ""); err != nil {
		return err
	}

	if _, err := session.emit(ctx, model.EventTaskRegistered, string(taskID), "", map[string]string{
		"name": name,
		"kind": "native.root",
	}); err != nil {
		cancel(err)
		return err
	}
	if _, err := session.emit(ctx, model.EventTaskStarted, string(taskID), "", nil); err != nil {
		cancel(err)
		return err
	}

	runErr := fn(ctx)
	session.closeTaskScope(taskID)
	terminal := classifyTerminal(runErr, ctx)
	attributes := map[string]string{"status": terminal}
	if runErr != nil {
		attributes["error"] = runErr.Error()
	}
	if cause := context.Cause(ctx); cause != nil {
		attributes["contextCause"] = cause.Error()
	}
	emitCtx := context.WithoutCancel(ctx)
	if _, err := session.emit(emitCtx, model.EventTaskFinished, string(taskID), "", attributes); err != nil {
		return errors.Join(runErr, err)
	}
	if _, err := session.emit(emitCtx, model.EventTargetReturned, string(taskID), "", attributes); err != nil {
		return errors.Join(runErr, err)
	}
	session.finishTask(taskID, nil)
	if drainErr := session.waitForDrain(); drainErr != nil {
		code := "task_evidence"
		if errors.Is(drainErr, ErrDrainTimeout) {
			code = "drain_timeout"
		}
		_, emitErr := session.emit(emitCtx, model.EventEvidenceIncomplete, string(taskID), "", map[string]string{
			"code":   code,
			"reason": drainErr.Error(),
		})
		return errors.Join(runErr, drainErr, emitErr)
	}
	if _, err := session.emit(emitCtx, model.EventDrainCompleted, string(taskID), "", map[string]string{"registeredTasks": "terminal"}); err != nil {
		return errors.Join(runErr, err)
	}
	if _, err := session.emit(emitCtx, model.EventSessionEnded, string(session.sessionID), "", nil); err != nil {
		return errors.Join(runErr, err)
	}
	return runErr
}

func cancellationError(id model.CancellationID) error {
	if id == "" {
		return ErrInjectedCancellation
	}
	return fmt.Errorf("%w: %s", ErrInjectedCancellation, id)
}
