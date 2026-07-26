package evidence

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
)

const (
	CapabilityCancellationRequested = "cancellation.requested"
	CapabilityCancellationDelivered = "cancellation.delivered"
	CapabilityCancellationObserved  = "cancellation.observed"
	CapabilityAuthoritativeCommit   = "effects.authoritative_commit"
	CapabilityDrainRegisteredTasks  = "drain.registered_tasks"
)

type Task struct {
	ID              string            `json:"id"`
	ParentID        string            `json:"parentId,omitempty"`
	Kind            string            `json:"kind"`
	Name            string            `json:"name"`
	State           model.TaskState   `json:"state"`
	RegisteredOrder uint64            `json:"registeredOrder"`
	TerminalOrder   uint64            `json:"terminalOrder,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type Cancellation struct {
	ID             string `json:"id"`
	TargetTaskID   string `json:"targetTaskId"`
	Trigger        string `json:"trigger"`
	RequestedOrder uint64 `json:"requestedOrder"`
	DeliveredOrder uint64 `json:"deliveredOrder,omitempty"`
	ObservedOrder  uint64 `json:"observedOrder,omitempty"`
}

type Effect struct {
	ID             string            `json:"id"`
	OwnerTaskID    string            `json:"ownerTaskId"`
	Kind           string            `json:"kind"`
	IdempotencyKey string            `json:"idempotencyKey"`
	EvidenceSource string            `json:"evidenceSource"`
	State          model.EffectState `json:"state"`
	DeclaredOrder  uint64            `json:"declaredOrder"`
	AttemptOrder   uint64            `json:"attemptOrder,omitempty"`
	CommitOrder    uint64            `json:"commitOrder,omitempty"`
	TerminalOrder  uint64            `json:"terminalOrder,omitempty"`
}

type Resource struct {
	ID            string              `json:"id"`
	OwnerTaskID   string              `json:"ownerTaskId"`
	Kind          string              `json:"kind"`
	Name          string              `json:"name"`
	State         model.ResourceState `json:"state"`
	AcquiredOrder uint64              `json:"acquiredOrder"`
	ReleasedOrder uint64              `json:"releasedOrder,omitempty"`
}

type Edge struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Relation      string `json:"relation"`
	EvidenceOrder uint64 `json:"evidenceOrder,omitempty"`
}

type Issue struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	EntityID      string `json:"entityId,omitempty"`
	EvidenceOrder uint64 `json:"evidenceOrder,omitempty"`
}

// View is the immutable, typed projection consumed by contracts and reports.
// Raw Events stay available to Go callers but are omitted from JSON because a
// capsule stores the canonical event stream separately.
type View struct {
	RunID             model.RunID     `json:"runId"`
	AttemptID         model.AttemptID `json:"attemptId"`
	Events            []model.Event   `json:"-"`
	Tasks             []Task          `json:"tasks"`
	Cancellations     []Cancellation  `json:"cancellations"`
	Effects           []Effect        `json:"effects"`
	Resources         []Resource      `json:"resources"`
	Edges             []Edge          `json:"edges"`
	Capabilities      []string        `json:"capabilities"`
	IncompleteReasons []string        `json:"incompleteReasons"`
	Issues            []Issue         `json:"issues"`
	Drained           bool            `json:"drained"`
}

type BuildInput struct {
	Snapshot                     ingest.Snapshot
	AuthoritativeEffects         []fixtureledger.Record
	AuthoritativeEffectsComplete bool
}

func (v View) Complete() bool {
	return len(v.IncompleteReasons) == 0 && len(v.Issues) == 0
}

func (v View) HasCapability(capability string) bool {
	return slices.Contains(v.Capabilities, capability)
}

func Build(input BuildInput) View {
	v := View{
		RunID: input.Snapshot.RunID, AttemptID: input.Snapshot.AttemptID,
		Events:            cloneEvents(input.Snapshot.Events),
		IncompleteReasons: append([]string(nil), input.Snapshot.IncompleteReasons...),
		Tasks:             []Task{}, Cancellations: []Cancellation{}, Effects: []Effect{}, Resources: []Resource{}, Edges: []Edge{}, Issues: []Issue{},
	}
	if !input.AuthoritativeEffectsComplete {
		v.IncompleteReasons = appendUnique(v.IncompleteReasons, "authoritative effect evidence is incomplete")
	}
	if len(v.Events) == 0 {
		v.Issues = append(v.Issues, Issue{Code: "empty_evidence", Message: "evidence contains no events"})
	}
	tasks := make(map[string]*Task)
	cancellations := make(map[string]*Cancellation)
	checkpoints := make(map[string]model.Event)
	effects := make(map[string]*Effect)
	resources := make(map[string]*Resource)
	lastCanonical := uint64(0)
	local := make(map[model.SessionID]uint64)
	capabilities := make(map[string]bool)
	for index, event := range v.Events {
		if event.CanonicalOrder != uint64(index+1) {
			v.Issues = append(v.Issues, Issue{Code: "canonical_gap", Message: fmt.Sprintf("event order is %d at index %d", event.CanonicalOrder, index+1), EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
		}
		if event.CanonicalOrder <= lastCanonical {
			v.Issues = append(v.Issues, Issue{Code: "canonical_order", Message: "canonical order is not increasing", EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
		}
		lastCanonical = event.CanonicalOrder
		if want := local[event.SessionID] + 1; event.LocalSequence != want {
			v.Issues = append(v.Issues, Issue{Code: "local_gap", Message: fmt.Sprintf("session sequence is %d, want %d", event.LocalSequence, want), EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
		}
		local[event.SessionID] = event.LocalSequence
		switch event.Type {
		case model.EventSessionStarted:
		case model.EventSessionEnded:
		case model.EventTaskRegistered:
			if _, exists := tasks[event.EntityID]; exists {
				v.Issues = append(v.Issues, Issue{Code: "duplicate_task", Message: "task registered twice", EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
			} else {
				task := &Task{ID: event.EntityID, ParentID: event.ParentEntityID, Kind: event.Attributes["kind"], Name: event.Attributes["name"], State: model.TaskRegistered, RegisteredOrder: event.CanonicalOrder, Metadata: cloneMap(event.Attributes)}
				tasks[event.EntityID] = task
				v.Tasks = append(v.Tasks, *task)
				if event.ParentEntityID != "" {
					if _, ok := tasks[event.ParentEntityID]; !ok {
						v.Issues = append(v.Issues, Issue{Code: "missing_task_parent", Message: "task parent is not registered", EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
					} else {
						v.Edges = append(v.Edges, Edge{From: event.ParentEntityID, To: event.EntityID, Relation: "task.spawned", EvidenceOrder: event.CanonicalOrder})
					}
				}
			}
		case model.EventTaskStarted:
			v.setTaskState(tasks, event, model.TaskStarted)
		case model.EventTaskFinished:
			state := model.TaskState(event.Attributes["status"])
			if state != model.TaskCompleted && state != model.TaskCancelled && state != model.TaskFailed {
				state = model.TaskLost
			}
			v.setTaskState(tasks, event, state)
		case model.EventCheckpointReached:
			checkpoints[event.EntityID] = event
			if task, ok := tasks[event.ParentEntityID]; ok {
				v.Edges = append(v.Edges, Edge{From: task.ID, To: event.EntityID, Relation: "task.reached", EvidenceOrder: event.CanonicalOrder})
			} else {
				v.Issues = append(v.Issues, Issue{Code: "missing_checkpoint_task", Message: "checkpoint parent task is missing", EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
			}
		case model.EventCheckpointReleased:
		case model.EventCancelRequested:
			capabilities[CapabilityCancellationRequested] = true
			cancel := &Cancellation{ID: event.EntityID, TargetTaskID: event.Attributes["targetTask"], Trigger: event.Attributes["trigger"], RequestedOrder: event.CanonicalOrder}
			if cancel.TargetTaskID == "" {
				v.Issues = append(v.Issues, Issue{Code: "missing_cancel_target", Message: "cancellation target is missing", EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
			}
			cancellations[event.EntityID] = cancel
			v.Cancellations = append(v.Cancellations, *cancel)
			if _, ok := tasks[cancel.TargetTaskID]; ok {
				v.Edges = append(v.Edges, Edge{From: cancel.ID, To: cancel.TargetTaskID, Relation: "cancellation.targets", EvidenceOrder: event.CanonicalOrder})
			}
			for visitID, visit := range checkpoints {
				if visit.Attributes["point"] == event.Attributes["point"] && visit.CanonicalOrder < event.CanonicalOrder {
					v.Edges = append(v.Edges, Edge{From: visitID, To: cancel.ID, Relation: "schedule.caused", EvidenceOrder: event.CanonicalOrder})
				}
			}
		case model.EventCancelDelivered:
			capabilities[CapabilityCancellationDelivered] = true
			if cancel := cancellations[event.EntityID]; cancel != nil {
				cancel.DeliveredOrder = event.CanonicalOrder
			}
		case model.EventCancelObserved:
			capabilities[CapabilityCancellationObserved] = true
			if cancel := cancellations[event.EntityID]; cancel != nil {
				cancel.ObservedOrder = event.CanonicalOrder
				if event.ParentEntityID != "" {
					v.Edges = append(v.Edges, Edge{From: cancel.ID, To: event.ParentEntityID, Relation: "cancellation.observed-by", EvidenceOrder: event.CanonicalOrder})
				}
			}
		case model.EventEffectDeclared:
			effect := &Effect{ID: event.EntityID, OwnerTaskID: event.ParentEntityID, Kind: event.Attributes["kind"], IdempotencyKey: event.Attributes["idempotencyKey"], EvidenceSource: event.Attributes["evidenceSource"], State: model.EffectDeclared, DeclaredOrder: event.CanonicalOrder}
			effects[event.EntityID] = effect
			v.Effects = append(v.Effects, *effect)
			if _, ok := tasks[event.ParentEntityID]; ok {
				v.Edges = append(v.Edges, Edge{From: event.ParentEntityID, To: event.EntityID, Relation: "task.initiated-effect", EvidenceOrder: event.CanonicalOrder})
			}
		case model.EventEffectAttempted, model.EventEffectCommitted, model.EventEffectFailed, model.EventEffectUnknown, model.EventEffectCompensated:
			v.updateEffect(effects, event)
		case model.EventResourceAcquired:
			resource := &Resource{ID: event.EntityID, OwnerTaskID: event.ParentEntityID, Kind: event.Attributes["kind"], Name: event.Attributes["name"], State: model.ResourceAcquired, AcquiredOrder: event.CanonicalOrder}
			resources[event.EntityID] = resource
			v.Resources = append(v.Resources, *resource)
			if _, ok := tasks[event.ParentEntityID]; ok {
				v.Edges = append(v.Edges, Edge{From: event.ParentEntityID, To: event.EntityID, Relation: "task.acquired-resource", EvidenceOrder: event.CanonicalOrder})
			}
		case model.EventResourceReleased:
			if resource := resources[event.EntityID]; resource != nil {
				resource.State, resource.ReleasedOrder = model.ResourceReleased, event.CanonicalOrder
			}
		case model.EventDrainCompleted:
			capabilities[CapabilityDrainRegisteredTasks] = true
			v.Drained = true
		}
	}
	if !capabilities[CapabilityCancellationObserved] && capabilities[CapabilityCancellationRequested] {
		v.Issues = append(v.Issues, Issue{Code: "cancellation_not_observed", Message: "cancellation was requested but not observed"})
	}
	if !v.Drained {
		v.Issues = append(v.Issues, Issue{Code: "drain_missing", Message: "registered tasks were not drained"})
	}
	v.reconcileEffects(input.AuthoritativeEffects, effects)
	if input.AuthoritativeEffectsComplete && len(effects) == 0 && len(input.AuthoritativeEffects) == 0 && len(v.Issues) == 0 {
		v.Capabilities = appendUnique(v.Capabilities, CapabilityAuthoritativeCommit)
	}
	for capability := range capabilities {
		v.Capabilities = append(v.Capabilities, capability)
	}
	slices.Sort(v.Capabilities)
	for i := range v.Tasks {
		if task := tasks[v.Tasks[i].ID]; task != nil {
			v.Tasks[i] = *task
		}
	}
	for i := range v.Cancellations {
		if cancel := cancellations[v.Cancellations[i].ID]; cancel != nil {
			v.Cancellations[i] = *cancel
		}
	}
	for i := range v.Effects {
		if effect := effects[v.Effects[i].ID]; effect != nil {
			v.Effects[i] = *effect
		}
	}
	for i := range v.Resources {
		if resource := resources[v.Resources[i].ID]; resource != nil {
			v.Resources[i] = *resource
		}
	}
	sort.SliceStable(v.Edges, func(i, j int) bool {
		if v.Edges[i].EvidenceOrder != v.Edges[j].EvidenceOrder {
			return v.Edges[i].EvidenceOrder < v.Edges[j].EvidenceOrder
		}
		if v.Edges[i].From != v.Edges[j].From {
			return v.Edges[i].From < v.Edges[j].From
		}
		return v.Edges[i].To < v.Edges[j].To
	})
	return v
}

func (v *View) setTaskState(tasks map[string]*Task, event model.Event, state model.TaskState) {
	task := tasks[event.EntityID]
	if task == nil {
		v.Issues = append(v.Issues, Issue{Code: "missing_task", Message: "task event has no registration", EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
		return
	}
	if task.State != model.TaskRegistered && task.State != model.TaskStarted {
		v.Issues = append(v.Issues, Issue{Code: "task_transition", Message: "task has a terminal state before this event", EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
		return
	}
	task.State = state
	if state != model.TaskStarted {
		task.TerminalOrder = event.CanonicalOrder
	}
}

func (v *View) updateEffect(effects map[string]*Effect, event model.Event) {
	effect := effects[event.EntityID]
	if effect == nil {
		v.Issues = append(v.Issues, Issue{Code: "missing_effect", Message: "effect transition has no declaration", EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
		return
	}
	next := effectState(event.Type)
	if err := model.ValidateEffectTransition(effect.State, next); err != nil {
		v.Issues = append(v.Issues, Issue{Code: "effect_transition", Message: err.Error(), EntityID: event.EntityID, EvidenceOrder: event.CanonicalOrder})
		return
	}
	effect.State = next
	switch next {
	case model.EffectAttempted:
		effect.AttemptOrder = event.CanonicalOrder
	case model.EffectCommitted:
		effect.CommitOrder = event.CanonicalOrder
		effect.TerminalOrder = event.CanonicalOrder
	case model.EffectFailed, model.EffectUnknown, model.EffectCompensated:
		effect.TerminalOrder = event.CanonicalOrder
	}
}

func (v *View) reconcileEffects(records []fixtureledger.Record, effects map[string]*Effect) {
	authoritative := make(map[string]fixtureledger.Record, len(records))
	for _, record := range records {
		if err := record.Validate(); err != nil {
			v.Issues = append(v.Issues, Issue{Code: "invalid_authoritative_effect", Message: err.Error(), EntityID: record.EffectID})
			continue
		}
		if _, exists := authoritative[record.EffectID]; exists {
			v.Issues = append(v.Issues, Issue{Code: "duplicate_authoritative_effect", Message: "effect has duplicate authoritative receipts", EntityID: record.EffectID})
		}
		authoritative[record.EffectID] = record
	}
	hasCommit := false
	for id, effect := range effects {
		if effect.State != model.EffectCommitted {
			continue
		}
		hasCommit = true
		if _, ok := authoritative[id]; !ok {
			v.Issues = append(v.Issues, Issue{Code: "missing_authoritative_effect", Message: "SDK committed effect has no authoritative receipt", EntityID: id, EvidenceOrder: effect.CommitOrder})
		}
	}
	for id := range authoritative {
		if _, ok := effects[id]; !ok {
			v.Issues = append(v.Issues, Issue{Code: "unknown_authoritative_effect", Message: "authoritative receipt has no SDK effect", EntityID: id})
		}
	}
	if hasCommit && len(authoritative) > 0 && len(v.Issues) == 0 {
		// This capability is only true when reconciliation had a chance to
		// compare both sides and found no contradiction.
		v.Capabilities = appendUnique(v.Capabilities, CapabilityAuthoritativeCommit)
	}
}

func effectState(eventType model.EventType) model.EffectState {
	switch eventType {
	case model.EventEffectAttempted:
		return model.EffectAttempted
	case model.EventEffectCommitted:
		return model.EffectCommitted
	case model.EventEffectFailed:
		return model.EffectFailed
	case model.EventEffectUnknown:
		return model.EffectUnknown
	case model.EventEffectCompensated:
		return model.EffectCompensated
	default:
		return model.EffectState(eventType)
	}
}

func (v View) Path(from, to string) []string {
	if from == to {
		return []string{from}
	}
	adjacency := make(map[string][]string)
	for _, edge := range v.Edges {
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
	}
	queue := []string{from}
	previous := map[string]string{from: ""}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adjacency[current] {
			if _, seen := previous[next]; seen {
				continue
			}
			previous[next] = current
			if next == to {
				queue = nil
				break
			}
			queue = append(queue, next)
		}
	}
	if _, ok := previous[to]; !ok {
		return nil
	}
	path := []string{to}
	for current := to; previous[current] != ""; current = previous[current] {
		path = append(path, previous[current])
	}
	slices.Reverse(path)
	return path
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

func appendUnique(values []string, value string) []string {
	if strings.TrimSpace(value) == "" || slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}
