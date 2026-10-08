package temporal

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
)

const AdapterMajorVersion = "temporal/1"

type HistoryEvent struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	ObservedAt time.Time         `json:"observedAt"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type WorkflowHistory struct {
	RunID      model.RunID     `json:"runId"`
	AttemptID  model.AttemptID `json:"attemptId"`
	SessionID  model.SessionID `json:"sessionId"`
	WorkflowID string          `json:"workflowId"`
	Events     []HistoryEvent  `json:"events"`
}

type ActivityHistory struct {
	RunID      model.RunID     `json:"runId"`
	AttemptID  model.AttemptID `json:"attemptId"`
	SessionID  model.SessionID `json:"sessionId"`
	ActivityID string          `json:"activityId"`
	Events     []HistoryEvent  `json:"events"`
}

type ActivityEffect struct {
	EffectID   string
	TaskID     string
	Kind       string
	Outcome    string
	ObservedAt time.Time
}

type RunHistory struct {
	Workflow   WorkflowHistory   `json:"workflow"`
	Activities []ActivityHistory `json:"activities"`
}

type Adapter struct {
	Version string
}

func NewAdapter() Adapter { return Adapter{Version: AdapterMajorVersion} }

func (a Adapter) Capabilities() []string {
	return []string{"cancellation.requested", "cancellation.delivered", "cancellation.observed", "effects.authoritative_commit", "drain.registered_tasks"}
}

func (a Adapter) TranslateWorkflow(input WorkflowHistory) (ingest.Snapshot, error) {
	return a.Translate(RunHistory{Workflow: input})
}

func (a Adapter) TranslateActivity(input ActivityHistory) (ingest.Snapshot, error) {
	if strings.TrimSpace(input.ActivityID) == "" {
		return ingest.Snapshot{}, fmt.Errorf("activity history requires activityId")
	}
	return a.Translate(RunHistory{Workflow: WorkflowHistory{RunID: input.RunID, AttemptID: input.AttemptID, SessionID: input.SessionID}, Activities: []ActivityHistory{input}})
}

func EffectHistory(effect ActivityEffect) (HistoryEvent, error) {
	if strings.TrimSpace(effect.EffectID) == "" || strings.TrimSpace(effect.Kind) == "" || effect.ObservedAt.IsZero() {
		return HistoryEvent{}, fmt.Errorf("activity effect requires effectId, kind, and observedAt")
	}
	outcome := strings.ToLower(strings.TrimSpace(effect.Outcome))
	if outcome != "declared" && outcome != "attempted" && outcome != "committed" && outcome != "failed" && outcome != "unknown" {
		return HistoryEvent{}, fmt.Errorf("unsupported activity effect outcome %q", effect.Outcome)
	}
	return HistoryEvent{ID: effect.EffectID, Type: "activity.effect_" + outcome, ObservedAt: effect.ObservedAt, Attributes: map[string]string{"effectId": effect.EffectID, "taskId": effect.TaskID, "kind": effect.Kind}}, nil
}

func (a Adapter) Translate(input RunHistory) (ingest.Snapshot, error) {
	workflow := input.Workflow
	if err := validateIdentity(workflow.RunID, workflow.AttemptID, workflow.SessionID); err != nil {
		return ingest.Snapshot{}, err
	}
	type item struct {
		event    HistoryEvent
		activity bool
		identity string
		order    int
	}
	items := make([]item, 0, len(workflow.Events))
	for index, event := range workflow.Events {
		items = append(items, item{event: event, identity: workflow.WorkflowID, order: index})
	}
	for _, activity := range input.Activities {
		if activity.RunID != workflow.RunID || activity.AttemptID != workflow.AttemptID || activity.SessionID != workflow.SessionID {
			return ingest.Snapshot{}, fmt.Errorf("activity history identity does not match workflow")
		}
		for index, event := range activity.Events {
			items = append(items, item{event: event, activity: true, identity: activity.ActivityID, order: index})
		}
	}
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].event.ObservedAt.Equal(items[right].event.ObservedAt) {
			return items[left].order < items[right].order
		}
		return items[left].event.ObservedAt.Before(items[right].event.ObservedAt)
	})
	store := ingest.NewMemory(workflow.RunID, workflow.AttemptID, 100_000)
	localSequence := uint64(0)
	for _, item := range items {
		event, known, err := a.canonical(workflow, item.event, item.activity, item.identity)
		if err != nil {
			_ = store.MarkIncomplete(err.Error())
			continue
		}
		if !known {
			_ = store.MarkIncomplete("unsupported Temporal history event: " + item.event.Type)
			continue
		}
		localSequence++
		event.LocalSequence = localSequence
		if _, err := store.Append(event); err != nil {
			return ingest.Snapshot{}, err
		}
	}
	return store.Freeze()
}

func (a Adapter) canonical(workflow WorkflowHistory, raw HistoryEvent, activity bool, identity string) (model.Event, bool, error) {
	if raw.ObservedAt.IsZero() {
		return model.Event{}, false, fmt.Errorf("Temporal history event %q has no observed time", raw.Type)
	}
	name := raw.ID
	if activity && identity != "" {
		name = identity
	}
	if name == "" {
		name = raw.Type
	}
	attrs := cloneAttributes(raw.Attributes)
	attrs["temporal.eventType"] = raw.Type
	if strings.HasPrefix(raw.Type, "activity.") && attrs["taskId"] != "" {
		activity, identity, name = true, attrs["taskId"], attrs["taskId"]
	}
	if activity && identity != "" && attrs["taskId"] == "" {
		attrs["taskId"] = identity
	}
	if strings.HasPrefix(raw.Type, "activity.effect_") {
		name = attrs["effectId"]
		if name == "" {
			return model.Event{}, false, fmt.Errorf("Temporal effect event %q has no effectId", raw.Type)
		}
	}
	prefix, typ := "workflow", model.EventSessionStarted
	switch raw.Type {
	case "workflow.started":
		typ = model.EventSessionStarted
	case "workflow.task_scheduled", "workflow.task_started", "workflow.task_completed", "workflow.signaled":
		typ, prefix = model.EventSchedulerAction, "scheduler"
	case "workflow.ended":
		typ = model.EventSessionEnded
	case "workflow.completed", "workflow.failed", "workflow.canceled":
		typ = model.EventTargetReturned
		attrs["status"] = strings.TrimPrefix(raw.Type, "workflow.")
	case "workflow.checkpoint_reached":
		typ = model.EventCheckpointReached
	case "workflow.checkpoint_released":
		typ = model.EventCheckpointReleased
	case "workflow.cancel_requested":
		typ = model.EventCancelRequested
	case "workflow.cancel_delivered":
		typ = model.EventCancelDelivered
	case "workflow.cancel_observed":
		typ = model.EventCancelObserved
	case "workflow.drain_completed":
		typ = model.EventDrainCompleted
	case "activity.scheduled":
		activity = true
		typ, prefix = model.EventTaskRegistered, "task"
		attrs["kind"] = "temporal.activity"
	case "activity.started":
		activity = true
		typ, prefix = model.EventTaskStarted, "task"
	case "activity.completed", "activity.failed", "activity.canceled", "activity.timed_out":
		activity = true
		typ, prefix = model.EventTaskFinished, "task"
		attrs["status"] = strings.TrimPrefix(raw.Type, "activity.")
		if raw.Type == "activity.canceled" {
			attrs["status"] = string(model.TaskCancelled)
		}
		if raw.Type == "activity.timed_out" {
			attrs["status"] = string(model.TaskFailed)
			attrs["cause"] = "timeout"
			if attrs["temporal.startedEventId"] == "0" {
				attrs["status"] = string(model.TaskLost)
			}
		}
	case "activity.cancel_requested":
		activity = true
		typ, prefix = model.EventCancelRequested, "cancellation"
	case "activity.cancel_delivered":
		activity = true
		typ, prefix = model.EventCancelDelivered, "cancellation"
	case "activity.heartbeat_cancel_observed":
		activity = true
		typ, prefix = model.EventCancelObserved, "cancellation"
	case "activity.effect_declared", "activity.effect_attempted", "activity.effect_committed", "activity.effect_failed", "activity.effect_unknown":
		activity = true
		typ, prefix = effectEventType(raw.Type)
	default:
		return model.Event{}, false, nil
	}
	if activity && prefix == "workflow" {
		prefix = "task"
	}
	id, err := model.ContentID(prefix, workflow.WorkflowID, name)
	if err != nil {
		return model.Event{}, false, err
	}
	parent := ""
	if taskName := attrs["taskId"]; taskName != "" && prefix != "task" {
		parent, _ = model.ContentID("task", workflow.WorkflowID, taskName)
	}
	if typ == model.EventCancelRequested && parent != "" {
		attrs["targetTask"] = parent
	}
	return model.Event{SchemaVersion: model.EventSchemaVersion, RunID: workflow.RunID, AttemptID: workflow.AttemptID, SessionID: workflow.SessionID, Type: typ, EntityID: id, ParentEntityID: parent, ObservedAt: raw.ObservedAt, Attributes: attrs}, true, nil
}

func effectEventType(raw string) (model.EventType, string) {
	kind := ""
	switch raw {
	case "activity.effect_declared":
		kind = "declared"
	case "activity.effect_attempted":
		kind = "attempted"
	case "activity.effect_committed":
		kind = "committed"
	case "activity.effect_failed":
		kind = "failed"
	case "activity.effect_unknown":
		kind = "unknown"
	}
	return model.EventType("effect." + kind), "effect"
}

func validateIdentity(runID model.RunID, attemptID model.AttemptID, sessionID model.SessionID) error {
	for name, value := range map[string]string{"run": string(runID), "attempt": string(attemptID), "session": string(sessionID)} {
		if err := model.ValidateID(value); err != nil {
			return fmt.Errorf("%s identity: %w", name, err)
		}
	}
	return nil
}

func cloneAttributes(source map[string]string) map[string]string {
	result := make(map[string]string, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}
