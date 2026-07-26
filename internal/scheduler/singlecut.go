package scheduler

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/abhisek343/cutline/internal/control"
)

var ErrInvalidPlan = errors.New("invalid scheduler plan")

// Policy is the coordinator-side decision surface shared by discovery and
// execution. A policy must be safe for concurrent checkpoint visits.
type Policy interface {
	Decide(pointName string) control.Action
	Injected() bool
}

// SingleCut injects cancellation at the first visit of one named point and
// releases every other visit. It is deterministic and safe for concurrent calls.
type SingleCut struct {
	cancelAt string
	mu       sync.Mutex
	injected bool
}

func NewSingleCut(cancelAt string) (*SingleCut, error) {
	cancelAt = strings.TrimSpace(cancelAt)
	if cancelAt == "" {
		return nil, fmt.Errorf("%w: cancel point is blank", ErrInvalidPlan)
	}
	return &SingleCut{cancelAt: cancelAt}, nil
}

func (s *SingleCut) Decide(pointName string) control.Action {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.injected && pointName == s.cancelAt {
		s.injected = true
		return control.ActionCancel
	}
	return control.ActionRelease
}

func (s *SingleCut) Injected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.injected
}

// Discovery releases every checkpoint and records no cancellation. The
// coordinator uses it for a bounded, cancellation-free pass before generating
// execution schedules.
type Discovery struct {
	maxVisits int

	mu        sync.Mutex
	visits    int
	truncated bool
}

func NewDiscovery(maxVisits int) *Discovery {
	if maxVisits <= 0 {
		maxVisits = 1_000
	}
	return &Discovery{maxVisits: maxVisits}
}

func (d *Discovery) Decide(_ string) control.Action {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.visits >= d.maxVisits {
		d.truncated = true
		return control.ActionRelease
	}
	d.visits++
	return control.ActionRelease
}

func (d *Discovery) Injected() bool {
	return false
}

func (d *Discovery) Truncated() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.truncated
}

var _ Policy = (*SingleCut)(nil)
var _ Policy = (*Discovery)(nil)
