package cutline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/abhisek343/cutline/internal/control"
	"github.com/abhisek343/cutline/internal/model"
)

const (
	envNetwork      = "CUTLINE_NETWORK"
	envEndpoint     = "CUTLINE_ENDPOINT"
	envToken        = "CUTLINE_TOKEN"
	envRunID        = "CUTLINE_RUN_ID"
	envAttemptID    = "CUTLINE_ATTEMPT_ID"
	envSessionID    = "CUTLINE_SESSION_ID"
	evidenceTimeout = 5 * time.Second
)

type runtimeSession struct {
	client    *control.Client
	runID     model.RunID
	attemptID model.AttemptID
	sessionID model.SessionID
	startedAt time.Time
	entities  atomic.Uint64
}

type scopeContextKey struct{}

type scopeState struct {
	session *runtimeSession
	id      model.TaskID
	cancel  context.CancelCauseFunc
}

func sessionFromEnvironment(ctx context.Context) (*runtimeSession, bool, error) {
	endpoint := os.Getenv(envEndpoint)
	if endpoint == "" {
		return nil, false, nil
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
		client:    client,
		runID:     config.RunID,
		attemptID: config.AttemptID,
		sessionID: config.SessionID,
		startedAt: time.Now(),
	}, true, nil
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
