package evidence

import (
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
)

func TestBuildAcceptsValidCancellationChain(t *testing.T) {
	t.Parallel()

	run, attempt, events := cancellationFixture(t)
	view := Build(BuildInput{
		Snapshot:                     ingest.Snapshot{RunID: run, AttemptID: attempt, Events: events},
		AuthoritativeEffectsComplete: true,
	})
	if !view.Complete() {
		t.Fatalf("valid cancellation chain was incomplete: issues=%+v reasons=%v", view.Issues, view.IncompleteReasons)
	}
	if !view.HasCapability(CapabilityCancellationRequested) ||
		!view.HasCapability(CapabilityCancellationDelivered) ||
		!view.HasCapability(CapabilityCancellationObserved) {
		t.Fatalf("cancellation capabilities = %v", view.Capabilities)
	}
}

func TestBuildRejectsOrphanAndMalformedCancellationObservations(t *testing.T) {
	t.Parallel()

	run, attempt, events := cancellationFixture(t)
	orphan := events[5]
	orphan.EntityID, _ = model.ContentID("cancel", t.Name(), "orphan")
	orphan.LocalSequence, orphan.CanonicalOrder = 4, 4
	orphan.ParentEntityID = events[2].EntityID
	events = append([]model.Event{events[0], events[1], events[2], orphan}, events[7:]...)
	renumberEvents(events)
	view := Build(BuildInput{
		Snapshot:                     ingest.Snapshot{RunID: run, AttemptID: attempt, Events: events},
		AuthoritativeEffectsComplete: true,
	})
	if view.Complete() || view.HasCapability(CapabilityCancellationObserved) || !hasIssue(view, "orphan_cancellation_observation") {
		t.Fatalf("orphan observation was accepted: complete=%t capabilities=%v issues=%+v", view.Complete(), view.Capabilities, view.Issues)
	}

	_, _, malformedEvents := cancellationFixture(t)
	malformedEvents[5].ParentEntityID = ""
	malformed := Build(BuildInput{
		Snapshot:                     ingest.Snapshot{RunID: run, AttemptID: attempt, Events: malformedEvents},
		AuthoritativeEffectsComplete: true,
	})
	if malformed.Complete() || !hasIssue(malformed, "cancellation_observation_parent") {
		t.Fatalf("malformed observation was accepted: complete=%t issues=%+v", malformed.Complete(), malformed.Issues)
	}
}

func TestBuildRejectsDuplicateAndOutOfOrderCancellationEvents(t *testing.T) {
	t.Parallel()

	run, attempt, events := cancellationFixture(t)
	duplicate := events[5]
	events = append(events[:6], append([]model.Event{duplicate}, events[6:]...)...)
	renumberEvents(events)
	view := Build(BuildInput{
		Snapshot:                     ingest.Snapshot{RunID: run, AttemptID: attempt, Events: events},
		AuthoritativeEffectsComplete: true,
	})
	if view.Complete() || !hasIssue(view, "duplicate_cancellation_observation") {
		t.Fatalf("duplicate observation was accepted: complete=%t issues=%+v", view.Complete(), view.Issues)
	}

	run, attempt, events = cancellationFixture(t)
	outOfOrder := append([]model.Event{}, events[:4]...)
	outOfOrder = append(outOfOrder, events[5], events[4])
	outOfOrder = append(outOfOrder, events[6:]...)
	events = outOfOrder
	renumberEvents(events)
	view = Build(BuildInput{
		Snapshot:                     ingest.Snapshot{RunID: run, AttemptID: attempt, Events: events},
		AuthoritativeEffectsComplete: true,
	})
	if view.Complete() || !hasIssue(view, "cancellation_order") {
		t.Fatalf("out-of-order cancellation was accepted: complete=%t issues=%+v", view.Complete(), view.Issues)
	}
}

func cancellationFixture(t *testing.T) (model.RunID, model.AttemptID, []model.Event) {
	t.Helper()
	runValue, _ := model.ContentID("run", t.Name())
	attemptValue, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	task, _ := model.ContentID("task", t.Name())
	cancel, _ := model.ContentID("cancel", t.Name())
	now := time.Unix(10_000, 0).UTC()
	base := func(sequence uint64, typ model.EventType, entity, parent string, attributes map[string]string) model.Event {
		return model.Event{
			SchemaVersion: model.EventSchemaVersion, RunID: model.RunID(runValue), AttemptID: model.AttemptID(attemptValue),
			SessionID: model.SessionID(session), LocalSequence: sequence, CanonicalOrder: sequence,
			Type: typ, EntityID: entity, ParentEntityID: parent, ObservedAt: now.Add(time.Duration(sequence) * time.Millisecond), Attributes: attributes,
		}
	}
	events := []model.Event{
		base(1, model.EventSessionStarted, session, "", nil),
		base(2, model.EventTaskRegistered, task, "", map[string]string{"kind": "native.root", "name": "test"}),
		base(3, model.EventTaskStarted, task, "", nil),
		base(4, model.EventCancelRequested, cancel, "", map[string]string{"targetTask": task, "trigger": "explicit"}),
		base(5, model.EventCancelDelivered, cancel, "", nil),
		base(6, model.EventCancelObserved, cancel, task, nil),
		base(7, model.EventTaskFinished, task, "", map[string]string{"status": string(model.TaskCancelled)}),
		base(8, model.EventDrainCompleted, task, "", nil),
		base(9, model.EventSessionEnded, session, "", nil),
	}
	return model.RunID(runValue), model.AttemptID(attemptValue), events
}

func renumberEvents(events []model.Event) {
	for index := range events {
		sequence := uint64(index + 1)
		events[index].LocalSequence = sequence
		events[index].CanonicalOrder = sequence
	}
}

func hasIssue(view View, code string) bool {
	for _, issue := range view.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
