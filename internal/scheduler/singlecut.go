package scheduler

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/abhisek343/cutline/internal/control"
)

var ErrInvalidPlan = errors.New("invalid scheduler plan")

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
