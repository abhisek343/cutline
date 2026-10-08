package signature

import (
	"errors"
	"testing"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/contracts"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/model"
)

func TestFailureSignatureExcludesRuntimeIdentity(t *testing.T) {
	t.Parallel()

	leftView, leftEffect := signatureView(t, "left")
	rightView, rightEffect := signatureView(t, "right")
	spec := campaign.ContractSpec{Name: "no-charge", Version: 1, Expression: `builtin.no_effect_after_cancel_observed("payment.charge")`}
	leftEvaluation := contracts.Result{Contract: spec.Name, Status: contracts.StatusViolation, OffendingEffectID: leftEffect, BoundaryOrder: 7, EffectOrder: 9}
	rightEvaluation := contracts.Result{Contract: spec.Name, Status: contracts.StatusViolation, OffendingEffectID: rightEffect, BoundaryOrder: 7, EffectOrder: 9}
	left, err := Build(leftView, spec, leftEvaluation, BuildContext{AdapterMajorVersion: "native/go-test/1"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := Build(rightView, spec, rightEvaluation, BuildContext{AdapterMajorVersion: "native/go-test/1"})
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("digests differ: %s != %s", left.Digest, right.Digest)
	}
	if left.PrimaryEntityKind != "effect" || left.PrimaryIdentityClass != "payment.charge" || left.ViolationClass != "post-cancel-effect-attempt" {
		t.Fatalf("signature = %#v", left)
	}
	if len(left.CausalPath) == 0 || left.CausalPath[0] != "task" {
		t.Fatalf("causal path = %#v", left.CausalPath)
	}
	if left.CancellationTrigger != "explicit" {
		t.Fatalf("trigger = %s", left.CancellationTrigger)
	}
}

func TestBuildAllSkipsPassingContractsAndDeduplicates(t *testing.T) {
	t.Parallel()

	view, _ := signatureView(t, "dedupe")
	specs := []campaign.ContractSpec{
		{Name: "first", Version: 1, Expression: `builtin.no_effect_after_cancel_observed("payment.charge")`},
		{Name: "second", Version: 1, Expression: `builtin.no_effect_after_cancel_observed("payment.charge")`},
	}
	evaluations := contracts.EvaluateBuiltins(view, specs)
	values, err := BuildAll(view, specs, evaluations, BuildContext{AdapterMajorVersion: "native/go-test/1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].Digest == values[1].Digest {
		t.Fatalf("signatures = %#v", values)
	}
	if _, err := Build(view, specs[0], contracts.Result{Contract: specs[0].Name, Status: contracts.StatusPass}, BuildContext{}); err == nil {
		t.Fatal("passing evaluation accepted")
	}
}

func signatureView(t *testing.T, label string) (evidence.View, string) {
	t.Helper()
	run, _ := model.ContentID("run", label)
	attempt, _ := model.ContentID("attempt", label)
	task, _ := model.ContentID("task", label)
	visit, _ := model.ContentID("visit", label)
	cancel, _ := model.ContentID("cancel", label)
	effect, _ := model.ContentID("effect", label)
	return evidence.View{
		RunID: model.RunID(run), AttemptID: model.AttemptID(attempt),
		Tasks:         []evidence.Task{{ID: task, Kind: "native.root", State: model.TaskCancelled, RegisteredOrder: 2, TerminalOrder: 10}},
		Cancellations: []evidence.Cancellation{{ID: cancel, TargetTaskID: task, Trigger: "explicit", RequestedOrder: 6, ObservedOrder: 7}},
		Effects:       []evidence.Effect{{ID: effect, OwnerTaskID: task, Kind: "payment.charge", IdempotencyKey: "order-1", State: model.EffectCommitted, AttemptOrder: 9, CommitOrder: 11, TerminalOrder: 11}},
		Edges:         []evidence.Edge{{From: task, To: visit, Relation: "task.reached"}, {From: visit, To: cancel, Relation: "schedule.caused"}},
	}, effect
}

func TestSignatureRequiresActualWitnessAndPreservesContractScope(t *testing.T) {
	view, effectID := signatureView(t, "scope")
	spec := campaign.ContractSpec{Name: "rule", Expression: `!effects.exists(e, e.kind == "payment.charge" && e.committed)`}
	unsupported := contracts.Result{Contract: "rule", Status: contracts.StatusViolation, BoundaryOrder: 7}
	if _, err := Build(view, spec, unsupported, BuildContext{}); !errors.Is(err, ErrUnsupportedWitness) {
		t.Fatalf("generic result signed: %v", err)
	}
	values, err := BuildAll(view, []campaign.ContractSpec{spec}, []contracts.Result{unsupported}, BuildContext{})
	if err != nil || len(values) != 0 {
		t.Fatalf("unsupported violation should remain unsigned: values=%+v err=%v", values, err)
	}
	evaluation := contracts.Result{Contract: "rule", Status: contracts.StatusViolation, OffendingEffectID: effectID}
	original, err := Build(view, spec, evaluation, BuildContext{})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"expression", "boundary", "requires", "effect-key", "source"} {
		t.Run(change, func(t *testing.T) {
			changedSpec := spec
			changedView := view
			changedView.Effects = append([]evidence.Effect(nil), view.Effects...)
			switch change {
			case "expression":
				changedSpec.Expression = "false"
			case "boundary":
				changedSpec.Boundary = "cancellation-observed"
			case "requires":
				changedSpec.Requires = []string{evidence.CapabilityAuthoritativeCommit}
			case "effect-key":
				changedView.Effects[0].IdempotencyKey = "different-order"
			case "source":
				changedView.Effects[0].EvidenceSource = "different-ledger"
			}
			changed, err := Build(changedView, changedSpec, evaluation, BuildContext{})
			if err != nil || changed.Digest == original.Digest {
				t.Fatalf("signature changed scope undetected: value=%+v err=%v", changed, err)
			}
		})
	}
}

func TestSignatureMinimizationCannotSwitchSameKindViolations(t *testing.T) {
	view, _ := signatureView(t, "multiple")
	first := view.Effects[0]
	first.ID, first.IdempotencyKey = "effect_a", "order-a"
	second := first
	second.ID, second.IdempotencyKey = "effect_b", "order-b"
	unrelated := first
	unrelated.ID, unrelated.Kind, unrelated.IdempotencyKey = "effect_email", "email.send", "unrelated"
	view.Effects = []evidence.Effect{unrelated, second, first}
	spec := campaign.ContractSpec{Name: "no-charge", Expression: `!effects.exists(e, e.kind == "payment.charge" && e.committed)`}
	build := func(v evidence.View) Signature {
		t.Helper()
		evaluation := contracts.Evaluate(v, []campaign.ContractSpec{spec})[0]
		value, err := Build(v, spec, evaluation, BuildContext{})
		if err != nil {
			t.Fatalf("evaluation=%+v err=%v", evaluation, err)
		}
		return value
	}
	original := build(view)
	withoutUnrelated := view
	withoutUnrelated.Effects = []evidence.Effect{second, first}
	if got := build(withoutUnrelated); got.Digest != original.Digest {
		t.Fatal("unrelated effect changed the witnessed signature")
	}
	withoutWitness := view
	withoutWitness.Effects = []evidence.Effect{unrelated, second}
	if got := build(withoutWitness); got.Digest == original.Digest {
		t.Fatal("removing witnessed violation switched to another effect with the same signature")
	}
}
