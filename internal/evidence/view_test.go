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
	view := Build(BuildInput{Snapshot: ingest.Snapshot{RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), Events: events}, AuthoritativeEffects: []fixtureledger.Record{{EffectID: effect, Kind: "payment.charge", Source: "fixture", CommittedAt: now}}, AuthoritativeEffectsComplete: true})
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
