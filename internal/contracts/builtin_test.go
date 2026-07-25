package contracts

import (
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/campaign"
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
	results := EvaluateBuiltins(snapshot, records, []campaign.ContractSpec{builtinSpec()})
	if results[0].Status != StatusViolation {
		t.Fatalf("status = %s, message = %s", results[0].Status, results[0].Message)
	}
}

func TestBuiltinPassAndInconclusive(t *testing.T) {
	t.Parallel()

	clean, _ := contractSnapshot(t, false)
	if got := EvaluateBuiltins(clean, nil, []campaign.ContractSpec{builtinSpec()})[0].Status; got != StatusPass {
		t.Fatalf("clean status = %s", got)
	}
	clean.IncompleteReasons = []string{"sequence gap"}
	if got := EvaluateBuiltins(clean, nil, []campaign.ContractSpec{builtinSpec()})[0].Status; got != StatusInconclusive {
		t.Fatalf("incomplete status = %s", got)
	}
}

func TestBuiltinRejectsUnsupportedExpression(t *testing.T) {
	t.Parallel()

	snapshot, _ := contractSnapshot(t, false)
	spec := builtinSpec()
	spec.Expression = "true"
	if got := EvaluateBuiltins(snapshot, nil, []campaign.ContractSpec{spec})[0].Status; got != StatusInvalid {
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
	cancelID, _ := model.ContentID("cancel", t.Name())
	effectID, _ := model.ContentID("effect", t.Name())
	events := []model.Event{{
		SchemaVersion: model.EventSchemaVersion,
		RunID:         model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session),
		LocalSequence: 1, CanonicalOrder: 1, Type: model.EventCancelObserved,
		EntityID: cancelID, ObservedAt: time.Now().UTC(),
	}}
	if committed {
		events = append(events, model.Event{
			SchemaVersion: model.EventSchemaVersion,
			RunID:         model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session),
			LocalSequence: 2, CanonicalOrder: 2, Type: model.EventEffectCommitted,
			EntityID: effectID, ObservedAt: time.Now().UTC(),
			Attributes: map[string]string{"kind": "payment.charge"},
		})
	}
	return ingest.Snapshot{
		RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), Events: events,
	}, effectID
}
