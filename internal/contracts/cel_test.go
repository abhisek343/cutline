package contracts

import (
	"strings"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func TestCELEngineEvaluatesFrozenEvidence(t *testing.T) {
	t.Parallel()

	snapshot, effectID := contractSnapshot(t, true)
	for index := range snapshot.Events {
		snapshot.Events[index].ObservedAt = snapshot.Events[index].ObservedAt.Add(time.Duration(index) * time.Millisecond)
	}
	view := evidence.Build(evidence.BuildInput{
		Snapshot: snapshot,
		AuthoritativeEffects: []fixtureledger.Record{{
			EffectID: effectID, Kind: "payment.charge", IdempotencyKey: "order-1", Source: "fixture", CommittedAt: time.Now().UTC(),
		}},
		AuthoritativeEffectsComplete: true,
	})
	spec := celSpec(`!effects.exists(e,
  e.kind == "payment.charge" &&
  e.committed &&
  e.startedAfter(cancel.observedAt))`)
	result := Evaluate(view, []campaign.ContractSpec{spec})[0]
	if result.Status != StatusViolation {
		t.Fatalf("status = %s, message = %s", result.Status, result.Message)
	}
	if result.BoundaryOrder != 6 {
		t.Fatalf("boundary order = %d, want 6", result.BoundaryOrder)
	}
}

func TestCELEnginePassesEmptyEffectSetAndUsesHelpers(t *testing.T) {
	t.Parallel()

	snapshot, _ := contractSnapshot(t, false)
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot, AuthoritativeEffectsComplete: true})
	spec := celSpec(`effects.all(e, !e.committed) &&
  !tasks.exists(t, t.descendsFrom(cancel.targetTask) && !t.isTerminalAt(run.drainedAt))`)
	result := Evaluate(view, []campaign.ContractSpec{spec})[0]
	if result.Status != StatusPass {
		t.Fatalf("status = %s, message = %s", result.Status, result.Message)
	}
}

func TestCELEngineClassifiesContractProblems(t *testing.T) {
	t.Parallel()

	snapshot, _ := contractSnapshot(t, false)
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot, AuthoritativeEffectsComplete: true})

	cases := []struct {
		name   string
		expr   string
		status Status
	}{
		{name: "syntax", expr: "effects.exists(e,", status: StatusInvalid},
		{name: "non-bool", expr: `"not a boolean"`, status: StatusInvalid},
		{name: "unsupported-version", expr: "true", status: StatusInvalid},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			spec := celSpec(testCase.expr)
			if testCase.name == "unsupported-version" {
				spec.Version = EnvironmentVersion + 1
			}
			result := Evaluate(view, []campaign.ContractSpec{spec})[0]
			if result.Status != testCase.status {
				t.Fatalf("status = %s, message = %s", result.Status, result.Message)
			}
		})
	}

	missing := view
	missing.Capabilities = nil
	result := Evaluate(missing, []campaign.ContractSpec{celSpec("true")})[0]
	if result.Status != StatusInconclusive {
		t.Fatalf("missing capability status = %s, message = %s", result.Status, result.Message)
	}
}

func TestCELEngineAppliesCostLimit(t *testing.T) {
	t.Parallel()

	snapshot, _ := contractSnapshot(t, false)
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot, AuthoritativeEffectsComplete: true})
	engine, err := NewCELEngine(1)
	if err != nil {
		t.Fatal(err)
	}
	result := engine.Evaluate(view, celSpec(`effects.exists(e, e.kind == "payment.charge")`))
	if result.Status != StatusInconclusive {
		t.Fatalf("status = %s, message = %s", result.Status, result.Message)
	}
}

func celSpec(expression string) campaign.ContractSpec {
	return campaign.ContractSpec{
		Name:       "cel-contract",
		Version:    1,
		Requires:   []string{evidence.CapabilityCancellationObserved, evidence.CapabilityAuthoritativeCommit, evidence.CapabilityDrainRegisteredTasks},
		Boundary:   "cancellation-observed",
		Expression: expression,
	}
}

func TestLifecycleHelpersRespectBoundaryAndMissingTimes(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, helper := range []struct {
		name, state, phase string
		inclusive          bool
		call               func(ref.Val, ref.Val) ref.Val
	}{
		{"isTerminalAt", "terminal", "terminalAt", true, isTerminalAt},
		{"wasCompensatedBefore", "compensated", "compensatedAt", false, wasCompensatedBefore},
		{"isReleasedAt", "released", "releasedAt", true, isReleasedAt},
	} {
		t.Run(helper.name, func(t *testing.T) {
			for _, delta := range []time.Duration{-time.Second, 0, time.Second} {
				entity := types.DefaultTypeAdapter.NativeToValue(map[string]any{helper.state: true, helper.phase: at.Add(delta)})
				got := helper.call(entity, types.Timestamp{Time: at})
				want := delta < 0 || (helper.inclusive && delta == 0)
				if got != types.Bool(want) {
					t.Fatalf("delta=%v: got %v want %v", delta, got, want)
				}
			}
			missing := types.DefaultTypeAdapter.NativeToValue(map[string]any{helper.state: true})
			if !types.IsError(helper.call(missing, types.Timestamp{Time: at})) {
				t.Fatal("missing lifecycle time did not remain unknown")
			}
			known := types.DefaultTypeAdapter.NativeToValue(map[string]any{helper.state: true, helper.phase: at})
			if !types.IsError(helper.call(known, types.Timestamp{})) {
				t.Fatal("missing boundary did not remain unknown")
			}
			pending := types.DefaultTypeAdapter.NativeToValue(map[string]any{helper.state: false})
			if helper.call(pending, types.Timestamp{Time: at}) != types.False {
				t.Fatal("known unfinished lifecycle should be false")
			}
		})
	}
}

func TestCELUnknownOutcomeWithoutRequiresIsInconclusive(t *testing.T) {
	for _, state := range []model.EffectState{model.EffectUnknown, model.EffectAttempted} {
		view := evidence.View{Effects: []evidence.Effect{{ID: "effect_unknown", State: state}}}
		for _, expr := range []string{"true", `!effects.exists(e, e.committed)`, `builtin.no_effect_after_cancel_observed("payment.charge")`} {
			result := Evaluate(view, []campaign.ContractSpec{{Name: "no-effect", Expression: expr}})[0]
			if result.Status != StatusInconclusive {
				t.Fatalf("state=%s expr=%s result=%+v", state, expr, result)
			}
		}
	}
}

func TestCELCompensatedEffectRetainsHistoricalCommit(t *testing.T) {
	snapshot, effectID := contractSnapshot(t, true)
	// Insert compensation before task completion and drain.
	insert := len(snapshot.Events) - 3
	compensation := snapshot.Events[insert-1]
	compensation.Type = model.EventEffectCompensated
	snapshot.Events = append(snapshot.Events[:insert], append([]model.Event{compensation}, snapshot.Events[insert:]...)...)
	for i := range snapshot.Events {
		snapshot.Events[i].LocalSequence, snapshot.Events[i].CanonicalOrder = uint64(i+1), uint64(i+1)
		snapshot.Events[i].ObservedAt = time.Date(2026, 10, 8, 12, 0, i, 0, time.UTC)
	}
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot, AuthoritativeEffects: []fixtureledger.Record{{EffectID: effectID, Kind: "payment.charge", IdempotencyKey: "order-1", Source: "fixture", CommittedAt: time.Now().UTC()}}, AuthoritativeEffectsComplete: true})
	specs := []campaign.ContractSpec{
		celSpec(`!effects.exists(e, e.kind == "payment.charge" && e.committed && e.startedAfter(cancel.observedAt))`),
		{Name: "compensated", Expression: `effects.filter(e, e.committed).all(e, e.wasCompensatedBefore(run.drainedAt))`},
		builtinSpec(),
	}
	results := Evaluate(view, specs)
	for i, want := range []Status{StatusViolation, StatusPass, StatusViolation} {
		if results[i].Status != want {
			t.Fatalf("result[%d]=%+v want=%s issues=%+v", i, results[i], want, view.Issues)
		}
	}
	if results[0].OffendingEffectID != effectID {
		t.Fatalf("witness=%q want %q", results[0].OffendingEffectID, effectID)
	}
}

func TestCELWitnessUsesOriginalScopeAndDeterministicIdentity(t *testing.T) {
	view := evidence.View{Effects: []evidence.Effect{
		{ID: "effect_unrelated", Kind: "email.send", IdempotencyKey: "a", State: model.EffectCommitted, CommitOrder: 1},
		{ID: "effect_b", Kind: "payment.charge", IdempotencyKey: "b", State: model.EffectCommitted, CommitOrder: 2},
		{ID: "effect_a", Kind: "payment.charge", IdempotencyKey: "a", State: model.EffectCommitted, CommitOrder: 3},
	}}
	// A singleton counterfactual would destroy the size predicate and find no witness.
	spec := campaign.ContractSpec{Name: "payments", Expression: `!effects.exists(e, e.kind == "payment.charge" && e.committed && effects.size() == 3)`}
	result := Evaluate(view, []campaign.ContractSpec{spec})[0]
	if result.Status != StatusViolation || result.OffendingEffectID != "effect_a" {
		t.Fatalf("result=%+v", result)
	}
	view.Effects[1], view.Effects[2] = view.Effects[2], view.Effects[1]
	if reordered := Evaluate(view, []campaign.ContractSpec{spec})[0]; reordered.OffendingEffectID != result.OffendingEffectID {
		t.Fatalf("reordering changed witness: %+v", reordered)
	}
	unsupported := Evaluate(view, []campaign.ContractSpec{{Name: "aggregate", Expression: `effects.size() < 2`}})[0]
	if unsupported.Status != StatusViolation || unsupported.OffendingEffectID != "" || !strings.Contains(unsupported.Message, "minimization unavailable") {
		t.Fatalf("unsupported witness result=%+v", unsupported)
	}
}

func TestCELMissingLifecycleTimestampIsInconclusive(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	view := evidence.View{
		Tasks:  []evidence.Task{{ID: "task_finished", State: model.TaskCompleted, TerminalOrder: 1}},
		Events: []model.Event{{Type: model.EventDrainCompleted, CanonicalOrder: 2, ObservedAt: at}},
	}
	result := Evaluate(view, []campaign.ContractSpec{{Name: "task-terminal", Expression: `tasks.all(t, t.isTerminalAt(run.drainedAt))`}})[0]
	if result.Status != StatusInconclusive {
		t.Fatalf("missing timestamp result=%+v", result)
	}
}
