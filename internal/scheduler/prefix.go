package scheduler

import (
	"strings"
	"sync"

	"github.com/abhisek343/cutline/internal/control"
)

// PrefixCut executes an explicit release prefix and cancels at the requested
// point. A point outside the prefix is an earlier cut, which makes removing a
// prefix action observable during minimization.
type PrefixCut struct {
	cancelAt string
	releases map[string]struct{}

	mu       sync.Mutex
	injected bool
}

func NewPrefixCut(cancelAt string, releasePrefix []string) (*PrefixCut, error) {
	cut, err := NewSingleCut(cancelAt)
	if err != nil {
		return nil, err
	}
	result := &PrefixCut{cancelAt: cut.cancelAt, releases: make(map[string]struct{}, len(releasePrefix))}
	for _, point := range releasePrefix {
		point = strings.TrimSpace(point)
		if point != "" {
			result.releases[point] = struct{}{}
		}
	}
	return result, nil
}

func (p *PrefixCut) Decide(pointName string) control.Action {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.injected {
		return control.ActionRelease
	}
	if pointName == p.cancelAt {
		p.injected = true
		return control.ActionCancel
	}
	if _, ok := p.releases[pointName]; ok {
		return control.ActionRelease
	}
	p.injected = true
	return control.ActionCancel
}

func (p *PrefixCut) Injected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.injected
}

var _ Policy = (*PrefixCut)(nil)
