package evidence

import (
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
)

func TestBuildReconcilesFaultyEffectAndCausalPath(t *testing.T) {
	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	task, _ := model.ContentID("task", t.Name())
	visit, _ := model.ContentID("visit", t.Name())
	cancel, _ := model.ContentID("cancel", t.Name())
	effect, _ := model.ContentID("effect", t.Name())
	now := time.Now().UTC()
	base := func(seq uint64, typ model.EventType, id, parent string, attrs map[string]string) model.Event {
		return model.Event{SchemaVersion: model.EventSchemaVersion, RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), LocalSequence: seq, CanonicalOrder: seq, Type: typ, EntityID: id, ParentEntityID: parent, ObservedAt: now, Attributes: attrs}
	}
	events := []model.Event{
		base(1, model.EventSessionStarted, session, "", nil),
		base(2, model.EventTaskRegistered, task, "", map[string]string{"kind": "native.root", "name": "checkout"}),
		base(3, model.EventTaskStarted, task, "", nil),
		base(4, model.EventCheckpointReached, visit, task, map[string]string{"point": "before-charge"}),
		base(5, model.EventCancelRequested, cancel, "", map[string]string{"targetTask": task, "point": "before-charge", "trigger": "explicit"}),
		base(6, model.EventCancelDelivered, cancel, "", nil),
		base(7, model.EventCancelObserved, cancel, task, nil),
		base(8, model.EventEffectDeclared, effect, task, map[string]string{"kind": "payment.charge", "idempotencyKey": "o-1", "evidenceSource": "fixture"}),
		base(9, model.EventEffectAttempted, effect, task, nil),
		base(10, model.EventEffectCommitted, effect, task, nil),
		base(11, model.EventTaskFinished, task, "", map[string]string{"status": string(model.TaskCancelled)}),
		base(12, model.EventTargetReturned, task, "", nil),
		base(13, model.EventDrainCompleted, task, "", nil),
		base(14, model.EventSessionEnded, session, "", nil),
	}
	view := Build(BuildInput{Snapshot: ingest.Snapshot{RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), Events: events}, AuthoritativeEffects: []fixtureledger.Record{{EffectID: effect, Kind: "payment.charge", IdempotencyKey: "o-1", Source: "fixture", CommittedAt: now}}, AuthoritativeEffectsComplete: true})
	if !view.Complete() {
		t.Fatalf("view incomplete: %+v", view.Issues)
	}
	if !view.HasCapability(CapabilityAuthoritativeCommit) {
		t.Fatal("authoritative capability missing")
	}
	path := view.Path(task, cancel)
	if len(path) == 0 {
		t.Fatal("expected task -> visit -> cancellation path")
	}
	if got := view.Effects[0].State; got != model.EffectCommitted {
		t.Fatalf("effect state = %s", got)
	}
}

func TestBuildRejectsMissingReceiptAndGaps(t *testing.T) {
	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	task, _ := model.ContentID("task", t.Name())
	now := time.Now().UTC()
	events := []model.Event{{SchemaVersion: 1, RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), LocalSequence: 2, CanonicalOrder: 1, Type: model.EventTaskStarted, EntityID: task, ObservedAt: now}}
	view := Build(BuildInput{Snapshot: ingest.Snapshot{RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), Events: events}, AuthoritativeEffectsComplete: true})
	if view.Complete() || len(view.Issues) == 0 {
		t.Fatalf("invalid evidence passed: %+v", view)
	}
}

func TestBuildRejectsUnresolvedAndContradictoryEffects(t *testing.T) {
	for _, state := range []model.EffectState{model.EffectAttempted, model.EffectUnknown, model.EffectFailed, model.EffectCommitted, model.EffectCompensated} {
		t.Run(string(state), func(t *testing.T) {
			snapshot, receipt := effectSnapshot(t, state)
			without := Build(BuildInput{Snapshot: snapshot, AuthoritativeEffectsComplete: true})
			if without.Complete() != (state == model.EffectFailed) {
				t.Fatalf("without receipt: state=%s issues=%+v", state, without.Issues)
			}
			with := Build(BuildInput{Snapshot: snapshot, AuthoritativeEffects: []fixtureledger.Record{receipt}, AuthoritativeEffectsComplete: true})
			wantComplete := state == model.EffectCommitted || state == model.EffectCompensated
			if with.Complete() != wantComplete || with.HasCapability(CapabilityAuthoritativeCommit) != wantComplete {
				t.Fatalf("with receipt: state=%s issues=%+v capabilities=%v", state, with.Issues, with.Capabilities)
			}
		})
	}
	for _, field := range []string{"kind", "source", "idempotency-key"} {
		t.Run(field, func(t *testing.T) {
			snapshot, receipt := effectSnapshot(t, model.EffectCommitted)
			switch field {
			case "kind":
				receipt.Kind = "email.send"
			case "source":
				receipt.Source = "other-ledger"
			case "idempotency-key":
				receipt.IdempotencyKey = "other-order"
			}
			view := Build(BuildInput{Snapshot: snapshot, AuthoritativeEffects: []fixtureledger.Record{receipt}, AuthoritativeEffectsComplete: true})
			if view.Complete() || view.HasCapability(CapabilityAuthoritativeCommit) {
				t.Fatalf("contradictory receipt accepted: %+v", view)
			}
		})
	}
}

func TestDrainRequiresEveryTaskTerminalBeforeMarker(t *testing.T) {
	for _, test := range []string{"live-task", "late-finish", "lost-task", "late-registration", "complete"} {
		t.Run(test, func(t *testing.T) {
			snapshot, receipt := effectSnapshot(t, model.EffectCommitted)
			// The baseline ends in task.finished, drain.completed, session.ended.
			finish := len(snapshot.Events) - 3
			switch test {
			case "live-task":
				snapshot.Events = append(snapshot.Events[:finish], snapshot.Events[finish+1:]...)
			case "late-finish":
				snapshot.Events[finish], snapshot.Events[finish+1] = snapshot.Events[finish+1], snapshot.Events[finish]
			case "lost-task":
				snapshot.Events[finish].Attributes["status"] = string(model.TaskLost)
			case "late-registration":
				child := snapshot.Events[1]
				child.EntityID = "task_child"
				snapshot.Events = append(snapshot.Events, child)
			}
			for i := range snapshot.Events {
				snapshot.Events[i].CanonicalOrder, snapshot.Events[i].LocalSequence = uint64(i+1), uint64(i+1)
			}
			view := Build(BuildInput{Snapshot: snapshot, AuthoritativeEffects: []fixtureledger.Record{receipt}, AuthoritativeEffectsComplete: true})
			if got, want := view.Drained && view.HasCapability(CapabilityDrainRegisteredTasks), test == "complete"; got != want {
				t.Fatalf("drain=%v capabilities=%v issues=%+v", view.Drained, view.Capabilities, view.Issues)
			}
		})
	}
}

func effectSnapshot(t *testing.T, state model.EffectState) (ingest.Snapshot, fixtureledger.Record) {
	t.Helper()
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	var events []model.Event
	add := func(typ model.EventType, id, parent string, attrs map[string]string) {
		order := uint64(len(events) + 1)
		events = append(events, model.Event{SchemaVersion: model.EventSchemaVersion, RunID: "run_test", AttemptID: "attempt_test", SessionID: "session_test", LocalSequence: order, CanonicalOrder: order, Type: typ, EntityID: id, ParentEntityID: parent, Attributes: attrs, ObservedAt: now.Add(time.Duration(order) * time.Second)})
	}
	add(model.EventSessionStarted, "session_test", "", nil)
	add(model.EventTaskRegistered, "task_test", "", map[string]string{"kind": "native.root"})
	add(model.EventTaskStarted, "task_test", "", nil)
	add(model.EventEffectDeclared, "effect_test", "task_test", map[string]string{"kind": "payment.charge", "idempotencyKey": "order-1", "evidenceSource": "fixture"})
	add(model.EventEffectAttempted, "effect_test", "task_test", nil)
	switch state {
	case model.EffectCommitted, model.EffectCompensated:
		add(model.EventEffectCommitted, "effect_test", "task_test", nil)
		if state == model.EffectCompensated {
			add(model.EventEffectCompensated, "effect_test", "task_test", nil)
		}
	case model.EffectFailed:
		add(model.EventEffectFailed, "effect_test", "task_test", nil)
	case model.EffectUnknown:
		add(model.EventEffectUnknown, "effect_test", "task_test", nil)
	}
	add(model.EventTaskFinished, "task_test", "", map[string]string{"status": string(model.TaskCompleted)})
	add(model.EventDrainCompleted, "task_test", "", nil)
	add(model.EventSessionEnded, "session_test", "", nil)
	return ingest.Snapshot{RunID: "run_test", AttemptID: "attempt_test", Events: events}, fixtureledger.Record{EffectID: "effect_test", Kind: "payment.charge", IdempotencyKey: "order-1", Source: "fixture", CommittedAt: now}
}

func TestDuplicateDeclarationCannotEraseHistoricalCommit(t *testing.T) {
	snapshot, receipt := effectSnapshot(t, model.EffectCommitted)
	declaration := snapshot.Events[3]
	insert := len(snapshot.Events) - 3
	snapshot.Events = append(snapshot.Events[:insert], append([]model.Event{declaration}, snapshot.Events[insert:]...)...)
	for i := range snapshot.Events {
		snapshot.Events[i].CanonicalOrder, snapshot.Events[i].LocalSequence = uint64(i+1), uint64(i+1)
	}
	view := Build(BuildInput{Snapshot: snapshot, AuthoritativeEffects: []fixtureledger.Record{receipt}, AuthoritativeEffectsComplete: true})
	if view.Complete() || len(view.Effects) != 1 || view.Effects[0].CommitOrder == 0 {
		t.Fatalf("duplicate declaration erased evidence: %+v", view)
	}
}
