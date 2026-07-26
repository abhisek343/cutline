package contracts

import (
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
)

func TestBuiltinDetectsAuthoritativePostCancelCommit(t *testing.T) {
	t.Parallel()

	snapshot, effectID := contractSnapshot(t, true)
	records := []fixtureledger.Record{{
		EffectID: effectID, Kind: "payment.charge", Source: "fixture", CommittedAt: time.Now().UTC(),
	}}
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot, AuthoritativeEffects: records, AuthoritativeEffectsComplete: true})
	results := EvaluateBuiltins(view, []campaign.ContractSpec{builtinSpec()})
	if results[0].Status != StatusViolation {
		t.Fatalf("status = %s, message = %s", results[0].Status, results[0].Message)
	}
}

func TestBuiltinPassAndInconclusive(t *testing.T) {
	t.Parallel()

	clean, _ := contractSnapshot(t, false)
	cleanView := evidence.Build(evidence.BuildInput{Snapshot: clean, AuthoritativeEffectsComplete: true})
	if got := EvaluateBuiltins(cleanView, []campaign.ContractSpec{builtinSpec()})[0].Status; got != StatusPass {
		t.Fatalf("clean status = %s", got)
	}
	clean.IncompleteReasons = []string{"sequence gap"}
	cleanView = evidence.Build(evidence.BuildInput{Snapshot: clean, AuthoritativeEffectsComplete: true})
	if got := EvaluateBuiltins(cleanView, []campaign.ContractSpec{builtinSpec()})[0].Status; got != StatusInconclusive {
		t.Fatalf("incomplete status = %s", got)
	}
}

func TestBuiltinRejectsUnsupportedExpression(t *testing.T) {
	t.Parallel()

	snapshot, _ := contractSnapshot(t, false)
	spec := builtinSpec()
	spec.Expression = "true"
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot, AuthoritativeEffectsComplete: true})
	if got := EvaluateBuiltins(view, []campaign.ContractSpec{spec})[0].Status; got != StatusInvalid {
		t.Fatalf("status = %s", got)
	}
}

func builtinSpec() campaign.ContractSpec {
	return campaign.ContractSpec{
		Name:       "no-charge-after-cancel",
		Expression: `builtin.no_effect_after_cancel_observed("payment.charge")`,
	}
}

func contractSnapshot(t *testing.T, committed bool) (ingest.Snapshot, string) {
	t.Helper()
	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	task, _ := model.ContentID("task", t.Name())
	cancelID, _ := model.ContentID("cancel", t.Name())
	effectID, _ := model.ContentID("effect", t.Name())
	now := time.Now().UTC()
	base := func(seq uint64, typ model.EventType, id, parent string, attrs map[string]string) model.Event {
		return model.Event{SchemaVersion: model.EventSchemaVersion, RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), LocalSequence: seq, CanonicalOrder: seq, Type: typ, EntityID: id, ParentEntityID: parent, ObservedAt: now, Attributes: attrs}
	}
	events := []model.Event{
		base(1, model.EventSessionStarted, session, "", nil),
		base(2, model.EventTaskRegistered, task, "", map[string]string{"kind": "native.root", "name": "test"}),
		base(3, model.EventTaskStarted, task, "", nil),
		base(4, model.EventCancelRequested, cancelID, "", map[string]string{"targetTask": task, "trigger": "explicit"}),
		base(5, model.EventCancelDelivered, cancelID, "", nil),
		base(6, model.EventCancelObserved, cancelID, task, nil),
		base(7, model.EventDrainCompleted, task, "", nil),
	}
	if committed {
		events = append(events, base(8, model.EventEffectDeclared, effectID, task, map[string]string{"kind": "payment.charge", "idempotencyKey": "order-1", "evidenceSource": "fixture"}))
		events = append(events, base(9, model.EventEffectAttempted, effectID, task, nil), base(10, model.EventEffectCommitted, effectID, task, nil))
	}
	events = append(events, base(uint64(len(events)+1), model.EventSessionEnded, session, "", nil))
	return ingest.Snapshot{
		RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), Events: events,
	}, effectID
}
