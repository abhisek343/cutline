package capsule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/minimize"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/abhisek343/cutline/internal/signature"
)

func TestBuildWriteAndValidateCapsule(t *testing.T) {
	input := capsuleInput(t)
	capsule, err := Build(input)
	if err != nil {
		t.Fatal(err)
	}
	if capsule.Manifest.CapsuleID == "" || len(capsule.Manifest.Artifacts) != 12 {
		t.Fatalf("manifest = %#v", capsule.Manifest)
	}
	if strings.Contains(string(capsule.Files["campaign.yaml"]), "secret") || capsule.Manifest.RedactionCount == 0 {
		t.Fatalf("redaction was not applied: count=%d", capsule.Manifest.RedactionCount)
	}
	directory := filepath.Join(t.TempDir(), "capsule")
	if err := capsule.WriteDirectory(directory); err != nil {
		t.Fatal(err)
	}
	manifest, err := ValidateDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.CapsuleID != capsule.Manifest.CapsuleID {
		t.Fatalf("validated ID = %s, want %s", manifest.CapsuleID, capsule.Manifest.CapsuleID)
	}
	if err := os.WriteFile(filepath.Join(directory, "schedule.json"), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDirectory(directory); err == nil {
		t.Fatal("ValidateDirectory accepted a tampered artifact")
	}
}

func TestCapsuleIDExcludesCreationTime(t *testing.T) {
	first := capsuleInput(t)
	first.CreatedAt = time.Unix(10, 0).UTC()
	second := first
	second.CreatedAt = time.Unix(20, 0).UTC()
	left, err := Build(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Build(second)
	if err != nil {
		t.Fatal(err)
	}
	if left.Manifest.CapsuleID != right.Manifest.CapsuleID {
		t.Fatalf("IDs differ: %s vs %s", left.Manifest.CapsuleID, right.Manifest.CapsuleID)
	}
}

func capsuleInput(t *testing.T) Input {
	t.Helper()
	raw := []byte("apiVersion: cutline.dev/v1alpha1\nkind: Campaign\nname: capsule-test\ntarget:\n  adapter: go-command\n  command: [go, test, --token=secret]\nexploration:\n  strategy: single-cut\n  maxSchedules: 1\n  maxPointVisits: 1\n  scheduleTimeout: 1s\n  drainTimeout: 1s\n  cancelAt: [cut]\ncontracts:\n  - name: rule\n    severity: critical\n    expression: builtin.no_effect_after_cancel_observed(\"x\")\noutput:\n  directory: .cutline\n  format: json\n")
	spec, err := campaign.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	runValue, _ := model.ContentID("run", t.Name())
	attemptValue, _ := model.ContentID("attempt", t.Name())
	sessionValue, _ := model.ContentID("session", t.Name())
	eventValue, _ := model.ContentID("task", t.Name())
	event := model.Event{
		SchemaVersion: model.EventSchemaVersion, RunID: model.RunID(runValue), AttemptID: model.AttemptID(attemptValue),
		SessionID: model.SessionID(sessionValue), LocalSequence: 1, CanonicalOrder: 1, Type: model.EventTargetReturned,
		EntityID: eventValue, ObservedAt: time.Unix(1, 0).UTC(),
	}
	schedule := explorer.Schedule{ID: "schedule-original", Ordinal: 1, Strategy: "single-cut", CancelAt: "cut", ReleasePrefix: []string{"before"}}
	minimized := schedule.WithReleasePrefix(nil)
	sig := signature.Signature{Version: 1, Digest: "sha256:" + strings.Repeat("a", 64), Contract: "rule", ContractVersion: 1, ViolationClass: "post-cancel-effect-attempt"}
	runID, attemptID := model.RunID(runValue), model.AttemptID(attemptValue)
	return Input{
		Campaign: spec, CampaignYAML: raw, RunID: runID, AttemptID: attemptID, Execution: Execution{Status: "violation"}, Schedule: schedule,
		Snapshot:   ingest.Snapshot{RunID: runID, AttemptID: attemptID, Events: []model.Event{event}},
		View:       evidence.View{RunID: runID, AttemptID: attemptID, Events: []model.Event{event}, Capabilities: []string{evidence.CapabilityCancellationObserved}},
		Signatures: []signature.Signature{sig}, FailureSignature: sig,
		Minimization: &minimize.Result{Original: schedule, Minimized: minimized, Target: sig, Attempts: []minimize.Attempt{{Candidate: minimized, Reproduced: true, SignatureDigest: sig.Digest}}, Stable: true},
		Redactions:   []Redaction{{Label: "token", Value: "secret"}}, CreatedAt: time.Unix(10, 0).UTC(),
	}
}
