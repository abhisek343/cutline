package model

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

const EventSchemaVersion = 1

// EventType is the canonical, runtime-independent evidence vocabulary.
type EventType string

const (
	EventUnknown            EventType = ""
	EventSessionStarted     EventType = "session.started"
	EventSessionEnded       EventType = "session.ended"
	EventTaskRegistered     EventType = "task.registered"
	EventTaskStarted        EventType = "task.started"
	EventTaskFinished       EventType = "task.finished"
	EventCheckpointReached  EventType = "checkpoint.reached"
	EventCheckpointReleased EventType = "checkpoint.released"
	EventCancelRequested    EventType = "cancel.requested"
	EventCancelDelivered    EventType = "cancel.delivered"
	EventCancelObserved     EventType = "cancel.observed"
	EventEffectDeclared     EventType = "effect.declared"
	EventEffectAttempted    EventType = "effect.attempted"
	EventEffectCommitted    EventType = "effect.committed"
	EventEffectFailed       EventType = "effect.failed"
	EventEffectUnknown      EventType = "effect.unknown"
	EventEffectCompensated  EventType = "effect.compensated"
	EventResourceAcquired   EventType = "resource.acquired"
	EventResourceReleased   EventType = "resource.released"
	EventTargetReturned     EventType = "target.returned"
	EventDrainCompleted     EventType = "drain.completed"
	EventEvidenceIncomplete EventType = "evidence.incomplete"
	EventSchedulerAction    EventType = "scheduler.action"
	EventContractEvaluation EventType = "contract.evaluation"
)

var knownEventTypes = []EventType{
	EventSessionStarted,
	EventSessionEnded,
	EventTaskRegistered,
	EventTaskStarted,
	EventTaskFinished,
	EventCheckpointReached,
	EventCheckpointReleased,
	EventCancelRequested,
	EventCancelDelivered,
	EventCancelObserved,
	EventEffectDeclared,
	EventEffectAttempted,
	EventEffectCommitted,
	EventEffectFailed,
	EventEffectUnknown,
	EventEffectCompensated,
	EventResourceAcquired,
	EventResourceReleased,
	EventTargetReturned,
	EventDrainCompleted,
	EventEvidenceIncomplete,
	EventSchedulerAction,
	EventContractEvaluation,
}

// Event is one immutable canonical fact accepted from an evidence source.
type Event struct {
	SchemaVersion  int               `json:"schemaVersion"`
	RunID          RunID             `json:"runId"`
	AttemptID      AttemptID         `json:"attemptId"`
	SessionID      SessionID         `json:"sessionId"`
	LocalSequence  uint64            `json:"localSequence"`
	CanonicalOrder uint64            `json:"canonicalOrder,omitempty"`
	Type           EventType         `json:"type"`
	EntityID       string            `json:"entityId"`
	ParentEntityID string            `json:"parentEntityId,omitempty"`
	ObservedAt     time.Time         `json:"observedAt"`
	MonotonicNanos int64             `json:"monotonicNanos,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

// Validate enforces structural integrity before an event can be ingested.
func (e Event) Validate() error {
	if e.SchemaVersion != EventSchemaVersion {
		return invalid(ErrInvalidEvent, "schemaVersion", fmt.Sprintf("must equal %d", EventSchemaVersion))
	}
	for field, value := range map[string]string{
		"runId":     string(e.RunID),
		"attemptId": string(e.AttemptID),
		"sessionId": string(e.SessionID),
		"entityId":  e.EntityID,
	} {
		if err := ValidateID(value); err != nil {
			return invalid(ErrInvalidEvent, field, err.Error())
		}
	}
	if e.ParentEntityID != "" {
		if err := ValidateID(e.ParentEntityID); err != nil {
			return invalid(ErrInvalidEvent, "parentEntityId", err.Error())
		}
	}
	if e.LocalSequence == 0 {
		return invalid(ErrInvalidEvent, "localSequence", "must be greater than zero")
	}
	if !slices.Contains(knownEventTypes, e.Type) {
		return invalid(ErrInvalidEvent, "type", "is unknown")
	}
	if e.ObservedAt.IsZero() {
		return invalid(ErrInvalidEvent, "observedAt", "must be set")
	}
	for key := range e.Attributes {
		if strings.TrimSpace(key) == "" {
			return invalid(ErrInvalidEvent, "attributes", "keys must not be blank")
		}
	}
	return nil
}

// CanonicalJSON returns deterministic JSON for a value composed of supported
// model structs, slices, and string-keyed maps.
func CanonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical json: %w", err)
	}
	return data, nil
}
