package contracts

import (
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/fixtureledger"
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
			EffectID: effectID, Kind: "payment.charge", Source: "fixture", CommittedAt: time.Now().UTC(),
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
