package cutline

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/abhisek343/cutline/internal/control"
	"github.com/abhisek343/cutline/internal/model"
)

var pointNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

// Point declares a stable cancellation-sensitive boundary. In an active run it
// blocks until the coordinator releases it or injects cancellation.
func Point(ctx context.Context, name string) error {
	if !pointNamePattern.MatchString(strings.TrimSpace(name)) {
		return ErrInvalidPoint
	}
	scope, active := scopeFromContext(ctx)
	if !active {
		return context.Cause(ctx)
	}
	visitValue, err := scope.session.nextEntity("visit", string(scope.id), name)
	if err != nil {
		return err
	}
	decision, err := scope.session.emit(ctx, model.EventCheckpointReached, visitValue, string(scope.id), map[string]string{
		"point":   name,
		"scopeId": string(scope.id),
	})
	if err != nil {
		return err
	}

	switch decision.Action {
	case control.ActionRelease:
		_, err = scope.session.emit(context.WithoutCancel(ctx), model.EventCheckpointReleased, visitValue, string(scope.id), map[string]string{
			"point": name,
		})
		return err
	case control.ActionCancel:
		cause := cancellationError(decision.CancellationID)
		scope.cancel(cause)
		_, emitErr := scope.session.emit(context.WithoutCancel(ctx), model.EventCancelObserved, string(decision.CancellationID), string(scope.id), map[string]string{
			"point":   name,
			"scopeId": string(scope.id),
		})
		if emitErr != nil {
			return errors.Join(cause, emitErr)
		}
		return cause
	default:
		return ErrControlUnavailable
	}
}
