package cutline

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/abhisek343/cutline/internal/model"
)

type EffectSpec struct {
	Kind           string
	IdempotencyKey string
	EvidenceSource string
}

// EffectHandle records one effect lifecycle. Methods are safe for sequential
// target use and reject contradictory terminal transitions.
type EffectHandle struct {
	mu    sync.Mutex
	scope *scopeState
	id    model.EffectID
	spec  EffectSpec
	state model.EffectState
}

func BeginEffect(ctx context.Context, spec EffectSpec) (*EffectHandle, error) {
	if strings.TrimSpace(spec.Kind) == "" || strings.TrimSpace(spec.EvidenceSource) == "" {
		return nil, ErrInvalidEffect
	}
	scope, active := scopeFromContext(ctx)
	if !active {
		value, err := model.NewID("effect")
		if err != nil {
			return nil, err
		}
		return &EffectHandle{id: model.EffectID(value), spec: spec, state: model.EffectDeclared}, nil
	}
	value, err := scope.session.nextEntity("effect", string(scope.id), spec.Kind, spec.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	handle := &EffectHandle{
		scope: scope,
		id:    model.EffectID(value),
		spec:  spec,
		state: model.EffectDeclared,
	}
	_, err = scope.session.emit(ctx, model.EventEffectDeclared, value, string(scope.id), handle.attributes())
	if err != nil {
		return nil, err
	}
	return handle, nil
}

func (e *EffectHandle) ID() string {
	return string(e.id)
}

func (e *EffectHandle) Attempt(ctx context.Context) error {
	return e.transition(ctx, model.EffectAttempted, model.EventEffectAttempted, "")
}

func (e *EffectHandle) Commit(ctx context.Context) error {
	return e.transition(ctx, model.EffectCommitted, model.EventEffectCommitted, "")
}

func (e *EffectHandle) Fail(ctx context.Context, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	return e.transition(ctx, model.EffectFailed, model.EventEffectFailed, message)
}

func (e *EffectHandle) Unknown(ctx context.Context, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	return e.transition(ctx, model.EffectUnknown, model.EventEffectUnknown, message)
}

func (e *EffectHandle) Compensate(ctx context.Context) error {
	return e.transition(ctx, model.EffectCompensated, model.EventEffectCompensated, "")
}

func (e *EffectHandle) transition(ctx context.Context, next model.EffectState, eventType model.EventType, message string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := model.ValidateEffectTransition(e.state, next); err != nil {
		return errors.Join(ErrInvalidEffect, err)
	}
	if e.scope != nil {
		attributes := e.attributes()
		if message != "" {
			attributes["error"] = message
		}
		if _, err := e.scope.session.emit(context.WithoutCancel(ctx), eventType, string(e.id), string(e.scope.id), attributes); err != nil {
			return err
		}
	}
	e.state = next
	return nil
}

func (e *EffectHandle) attributes() map[string]string {
	return map[string]string{
		"kind":           e.spec.Kind,
		"idempotencyKey": e.spec.IdempotencyKey,
		"evidenceSource": e.spec.EvidenceSource,
	}
}
