package native

import (
	"fmt"
	"sync"
	"time"

	"github.com/abhisek343/cutline/internal/control"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/abhisek343/cutline/internal/scheduler"
)

type eventHandler struct {
	runID              model.RunID
	attemptID          model.AttemptID
	coordinatorSession model.SessionID
	store              *ingest.Memory
	scheduler          *scheduler.SingleCut

	mu             sync.Mutex
	coordinatorSeq uint64
	cancellationID model.CancellationID
}

func newEventHandler(
	runID model.RunID,
	attemptID model.AttemptID,
	store *ingest.Memory,
	schedule *scheduler.SingleCut,
) (*eventHandler, error) {
	sessionValue, err := model.ContentID("session", "coordinator", string(runID), string(attemptID))
	if err != nil {
		return nil, err
	}
	return &eventHandler{
		runID:              runID,
		attemptID:          attemptID,
		coordinatorSession: model.SessionID(sessionValue),
		store:              store,
		scheduler:          schedule,
	}, nil
}

func (h *eventHandler) SessionStarted(hello control.Hello) error {
	event := model.Event{
		SchemaVersion: model.EventSchemaVersion,
		RunID:         hello.RunID,
		AttemptID:     hello.AttemptID,
		SessionID:     hello.SessionID,
		LocalSequence: 1,
		Type:          model.EventSessionStarted,
		EntityID:      string(hello.SessionID),
		ObservedAt:    time.Now().UTC(),
		Attributes:    map[string]string{"protocolVersion": fmt.Sprint(control.ProtocolVersion)},
	}
	_, err := h.store.Append(event)
	return err
}

func (h *eventHandler) EventAccepted(event model.Event) (control.Decision, error) {
	accepted, err := h.store.Append(event)
	if err != nil {
		h.store.MarkIncomplete(err.Error())
		return control.Decision{}, err
	}
	if accepted.Type != model.EventCheckpointReached {
		return control.Decision{Action: control.ActionNone}, nil
	}

	pointName := accepted.Attributes["point"]
	action := h.scheduler.Decide(pointName)
	if err := h.appendCoordinator(model.EventSchedulerAction, accepted.EntityID, map[string]string{
		"action": string(action),
		"point":  pointName,
	}); err != nil {
		return control.Decision{}, err
	}
	if action == control.ActionRelease {
		return control.Decision{Action: action}, nil
	}

	h.mu.Lock()
	if h.cancellationID == "" {
		value, idErr := model.ContentID("cancel", string(h.runID), string(h.attemptID), pointName)
		if idErr != nil {
			h.mu.Unlock()
			return control.Decision{}, idErr
		}
		h.cancellationID = model.CancellationID(value)
	}
	cancellationID := h.cancellationID
	h.mu.Unlock()

	attributes := map[string]string{
		"point":      pointName,
		"targetTask": accepted.ParentEntityID,
		"trigger":    "explicit",
	}
	if err := h.appendCoordinator(model.EventCancelRequested, string(cancellationID), attributes); err != nil {
		return control.Decision{}, err
	}
	if err := h.appendCoordinator(model.EventCancelDelivered, string(cancellationID), attributes); err != nil {
		return control.Decision{}, err
	}
	return control.Decision{Action: control.ActionCancel, CancellationID: cancellationID}, nil
}

func (h *eventHandler) appendCoordinator(eventType model.EventType, entityID string, attributes map[string]string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.coordinatorSeq++
	event := model.Event{
		SchemaVersion: model.EventSchemaVersion,
		RunID:         h.runID,
		AttemptID:     h.attemptID,
		SessionID:     h.coordinatorSession,
		LocalSequence: h.coordinatorSeq,
		Type:          eventType,
		EntityID:      entityID,
		ObservedAt:    time.Now().UTC(),
		Attributes:    attributes,
	}
	_, err := h.store.Append(event)
	if err != nil {
		h.store.MarkIncomplete(err.Error())
	}
	return err
}
