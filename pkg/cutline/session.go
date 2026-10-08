package cutline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abhisek343/cutline/internal/control"
	"github.com/abhisek343/cutline/internal/model"
)

const (
	envNetwork          = "CUTLINE_NETWORK"
	envEndpoint         = "CUTLINE_ENDPOINT"
	envToken            = "CUTLINE_TOKEN"
	envRunID            = "CUTLINE_RUN_ID"
	envAttemptID        = "CUTLINE_ATTEMPT_ID"
	envSessionID        = "CUTLINE_SESSION_ID"
	envDrainTimeout     = "CUTLINE_DRAIN_TIMEOUT"
	evidenceTimeout     = 5 * time.Second
	defaultDrainTimeout = 10 * time.Second
)

type runtimeSession struct {
	client       *control.Client
	runID        model.RunID
	attemptID    model.AttemptID
	sessionID    model.SessionID
	startedAt    time.Time
	entities     atomic.Uint64
	drainTimeout time.Duration

	tasksMu   sync.Mutex
	liveTasks map[model.TaskID]bool
	drained   chan struct{}
	drainErr  error
	stopped   bool
}

type scopeContextKey struct{}

type scopeState struct {
	session  *runtimeSession
	id       model.TaskID
	parentID model.TaskID
	cancel   context.CancelCauseFunc
}

func sessionFromEnvironment(ctx context.Context) (*runtimeSession, bool, error) {
	endpoint := os.Getenv(envEndpoint)
	if endpoint == "" {
		return nil, false, nil
	}
	drainTimeout := defaultDrainTimeout
	if value := os.Getenv(envDrainTimeout); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			return nil, true, fmt.Errorf("%w: %s must be a positive duration", ErrControlUnavailable, envDrainTimeout)
		}
		drainTimeout = parsed
	}
	config := control.ClientConfig{
		Network:   os.Getenv(envNetwork),
		Address:   endpoint,
		Token:     os.Getenv(envToken),
		RunID:     model.RunID(os.Getenv(envRunID)),
		AttemptID: model.AttemptID(os.Getenv(envAttemptID)),
		SessionID: model.SessionID(os.Getenv(envSessionID)),
	}
	if config.Network == "" {
		config.Network = "unix"
	}
	for name, value := range map[string]string{
		envNetwork:   config.Network,
		envEndpoint:  config.Address,
		envToken:     config.Token,
		envRunID:     string(config.RunID),
		envAttemptID: string(config.AttemptID),
		envSessionID: string(config.SessionID),
	} {
		if strings.TrimSpace(value) == "" {
			return nil, true, fmt.Errorf("%w: %s is not set", ErrControlUnavailable, name)
		}
	}
	client, err := control.Dial(ctx, config)
	if err != nil {
		return nil, true, fmt.Errorf("%w: %v", ErrControlUnavailable, err)
	}
	return &runtimeSession{
		client:       client,
		runID:        config.RunID,
		attemptID:    config.AttemptID,
		sessionID:    config.SessionID,
		startedAt:    time.Now(),
		drainTimeout: drainTimeout,
		liveTasks:    make(map[model.TaskID]bool),
		drained:      make(chan struct{}),
	}, true, nil
}

// reserveTask closes the registration-versus-return race: a child is counted
// before sending its registration, and only a still-live parent can add work.
func (s *runtimeSession) reserveTask(id, parentID model.TaskID) error {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	if s.stopped {
		return ErrTaskScopeClosed
	}
	if parentID != "" {
		if accepting := s.liveTasks[parentID]; !accepting {
			return ErrTaskScopeClosed
		}
	}
	select {
	case <-s.drained:
		return ErrTaskScopeClosed
	default:
	}
	s.liveTasks[id] = true
	return nil
}

func (s *runtimeSession) closeTaskScope(id model.TaskID) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	if _, live := s.liveTasks[id]; live {
		s.liveTasks[id] = false
	}
}

func (s *runtimeSession) close() {
	s.tasksMu.Lock()
	s.stopped = true
	s.tasksMu.Unlock()
	_ = s.client.Close()
}

// finishTask counts a task as drained only after its terminal event was
// acknowledged. A failed evidence call prevents a successful drain claim.
func (s *runtimeSession) finishTask(id model.TaskID, evidenceErr error) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	if _, live := s.liveTasks[id]; !live {
		return
	}
	delete(s.liveTasks, id)
	s.drainErr = errors.Join(s.drainErr, evidenceErr)
	if len(s.liveTasks) == 0 {
		close(s.drained)
	}
}

func (s *runtimeSession) waitForDrain() error {
	timer := time.NewTimer(s.drainTimeout)
	defer timer.Stop()
	select {
	case <-s.drained:
		s.tasksMu.Lock()
		defer s.tasksMu.Unlock()
		return s.drainErr
	case <-timer.C:
		return ErrDrainTimeout
	}
}

func (s *runtimeSession) nextEntity(prefix string, parts ...string) (string, error) {
	ordinal := s.entities.Add(1)
	all := []string{string(s.runID), string(s.sessionID), fmt.Sprint(ordinal)}
	all = append(all, parts...)
	return model.ContentID(prefix, all...)
}

func (s *runtimeSession) emit(ctx context.Context, eventType model.EventType, entityID, parentID string, attributes map[string]string) (control.Decision, error) {
	event := model.Event{
		SchemaVersion:  model.EventSchemaVersion,
		RunID:          s.runID,
		AttemptID:      s.attemptID,
		SessionID:      s.sessionID,
		Type:           eventType,
		EntityID:       entityID,
		ParentEntityID: parentID,
		ObservedAt:     time.Now().UTC(),
		MonotonicNanos: time.Since(s.startedAt).Nanoseconds(),
		Attributes:     attributes,
	}
	evidenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), evidenceTimeout)
	defer cancel()
	decision, err := s.client.SendEvent(evidenceCtx, event)
	if err != nil {
		return control.Decision{}, fmt.Errorf("%w: emit %s: %v", ErrControlUnavailable, eventType, err)
	}
	return decision, nil
}

func scopeFromContext(ctx context.Context) (*scopeState, bool) {
	scope, ok := ctx.Value(scopeContextKey{}).(*scopeState)
	return scope, ok && scope != nil
}

func classifyTerminal(err error, ctx context.Context) string {
	switch {
	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, ErrInjectedCancellation),
		context.Cause(ctx) != nil:
		return string(model.TaskCancelled)
	case err != nil:
		return string(model.TaskFailed)
	default:
		return string(model.TaskCompleted)
	}
}
