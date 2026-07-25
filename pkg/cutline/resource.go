package cutline

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/abhisek343/cutline/internal/model"
)

type ResourceSpec struct {
	Kind string
	Name string
}

type Resource struct {
	mu       sync.Mutex
	scope    *scopeState
	id       model.ResourceID
	released bool
	spec     ResourceSpec
}

func Acquire(ctx context.Context, spec ResourceSpec) (*Resource, error) {
	if strings.TrimSpace(spec.Kind) == "" || strings.TrimSpace(spec.Name) == "" {
		return nil, ErrInvalidResource
	}
	resource := &Resource{spec: spec}
	scope, active := scopeFromContext(ctx)
	if !active {
		return resource, nil
	}
	value, err := scope.session.nextEntity("resource", string(scope.id), spec.Kind, spec.Name)
	if err != nil {
		return nil, err
	}
	resource.scope = scope
	resource.id = model.ResourceID(value)
	if _, err := scope.session.emit(ctx, model.EventResourceAcquired, value, string(scope.id), map[string]string{
		"kind": spec.Kind,
		"name": spec.Name,
	}); err != nil {
		return nil, err
	}
	return resource, nil
}

func (r *Resource) Release(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.released {
		return errors.Join(ErrInvalidResource, errors.New("resource already released"))
	}
	if r.scope != nil {
		if _, err := r.scope.session.emit(context.WithoutCancel(ctx), model.EventResourceReleased, string(r.id), string(r.scope.id), map[string]string{
			"kind": r.spec.Kind,
			"name": r.spec.Name,
		}); err != nil {
			return err
		}
	}
	r.released = true
	return nil
}
