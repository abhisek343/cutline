package ingest

import (
	"errors"
	"fmt"
	"sync"

	"github.com/abhisek343/cutline/internal/model"
)

var (
	ErrFrozen      = errors.New("evidence is frozen")
	ErrIdentity    = errors.New("evidence identity mismatch")
	ErrSequenceGap = errors.New("evidence sequence gap")
	ErrDuplicate   = errors.New("duplicate evidence")
	ErrEventLimit  = errors.New("evidence event limit reached")
)

// Snapshot is an immutable copy of one attempt's canonical evidence.
type Snapshot struct {
	RunID             model.RunID     `json:"runId"`
	AttemptID         model.AttemptID `json:"attemptId"`
	Events            []model.Event   `json:"events"`
	IncompleteReasons []string        `json:"incompleteReasons"`
}

// Store is the append-only evidence boundary used by native and durable runs.
// Implementations must make Freeze terminal: after it returns, no evidence or
// incompleteness marker can be added.
type Store interface {
	Append(model.Event) (model.Event, error)
	MarkIncomplete(string) error
	Current() ([]model.Event, error)
	Freeze() (Snapshot, error)
}

func (s Snapshot) Complete() bool {
	return len(s.IncompleteReasons) == 0
}

// Memory accepts events in coordinator observation order and assigns the single
// canonical order used by schedulers and contracts.
type Memory struct {
	mu         sync.Mutex
	runID      model.RunID
	attemptID  model.AttemptID
	maxEvents  int
	frozen     bool
	events     []model.Event
	lastLocal  map[model.SessionID]uint64
	incomplete []string
}

func NewMemory(runID model.RunID, attemptID model.AttemptID, maxEvents int) *Memory {
	return &Memory{
		runID:     runID,
		attemptID: attemptID,
		maxEvents: maxEvents,
		lastLocal: make(map[model.SessionID]uint64),
	}
}

func (m *Memory) Append(event model.Event) (model.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.frozen {
		return model.Event{}, ErrFrozen
	}
	if event.RunID != m.runID || event.AttemptID != m.attemptID {
		return model.Event{}, ErrIdentity
	}
	if err := event.Validate(); err != nil {
		return model.Event{}, err
	}
	if m.maxEvents > 0 && len(m.events) >= m.maxEvents {
		m.markIncompleteLocked(ErrEventLimit.Error())
		return model.Event{}, ErrEventLimit
	}
	last := m.lastLocal[event.SessionID]
	switch {
	case event.LocalSequence == last:
		return model.Event{}, ErrDuplicate
	case event.LocalSequence != last+1:
		m.markIncompleteLocked(fmt.Sprintf("%v: session %s got %d after %d", ErrSequenceGap, event.SessionID, event.LocalSequence, last))
		return model.Event{}, ErrSequenceGap
	}
	event.CanonicalOrder = uint64(len(m.events) + 1)
	event.Attributes = cloneMap(event.Attributes)
	m.lastLocal[event.SessionID] = event.LocalSequence
	m.events = append(m.events, event)
	return event, nil
}

func (m *Memory) MarkIncomplete(reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.frozen {
		return ErrFrozen
	}
	m.markIncompleteLocked(reason)
	return nil
}

func (m *Memory) markIncompleteLocked(reason string) {
	for _, existing := range m.incomplete {
		if existing == reason {
			return
		}
	}
	m.incomplete = append(m.incomplete, reason)
}

func (m *Memory) Freeze() (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.frozen = true
	return Snapshot{
		RunID:             m.runID,
		AttemptID:         m.attemptID,
		Events:            cloneEvents(m.events),
		IncompleteReasons: append([]string(nil), m.incomplete...),
	}, nil
}

func (m *Memory) Current() ([]model.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneEvents(m.events), nil
}

func cloneEvents(events []model.Event) []model.Event {
	result := make([]model.Event, len(events))
	copy(result, events)
	for i := range result {
		result[i].Attributes = cloneMap(result[i].Attributes)
	}
	return result
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
