package replay

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/capsule"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/minimize"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/abhisek343/cutline/internal/signature"
)

func TestReplayComparesExactSignatureAndRejectsTamper(t *testing.T) {
	root := buildReplayCapsule(t)
	want := signature.Signature{Digest: "sha256:replay"}
	result, err := Run(context.Background(), root, Config{}, func(_ context.Context, _ campaign.Campaign, schedule explorer.Schedule) ([]signature.Signature, error) {
		if schedule.CancelAt != "cut" {
			t.Fatalf("replay schedule = %#v", schedule)
		}
		return []signature.Signature{want}, nil
	})
	if err != nil || !result.Exact || result.Status != StatusReproduced {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if err := os.WriteFile(filepath.Join(root, "schedule.json"), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), root, Config{}, func(context.Context, campaign.Campaign, explorer.Schedule) ([]signature.Signature, error) {
		return []signature.Signature{want}, nil
	}); err == nil {
		t.Fatal("replay accepted tampered capsule")
	}
}

func buildReplayCapsule(t *testing.T) string {
	t.Helper()
	raw := []byte("apiVersion: cutline.dev/v1alpha1\nkind: Campaign\nname: replay-test\ntarget:\n  adapter: go-command\n  command: [true]\nexploration:\n  strategy: single-cut\n  maxSchedules: 1\n  maxPointVisits: 1\n  scheduleTimeout: 1s\n  drainTimeout: 1s\n  cancelAt: [cut]\ncontracts:\n  - name: rule\n    severity: critical\n    expression: builtin.no_effect_after_cancel_observed(\"x\")\n")
	spec, err := campaign.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	runValue, _ := model.ContentID("run", t.Name())
	attemptValue, _ := model.ContentID("attempt", t.Name())
	sessionValue, _ := model.ContentID("session", t.Name())
	eventValue, _ := model.ContentID("task", t.Name())
	runID, attemptID := model.RunID(runValue), model.AttemptID(attemptValue)
	event := model.Event{SchemaVersion: 1, RunID: runID, AttemptID: attemptID, SessionID: model.SessionID(sessionValue), LocalSequence: 1, CanonicalOrder: 1, Type: model.EventTargetReturned, EntityID: eventValue, ObservedAt: time.Unix(1, 0).UTC()}
	schedule := explorer.Schedule{ID: "original", Ordinal: 1, Strategy: "single-cut", CancelAt: "cut"}
	sig := signature.Signature{Version: 1, Digest: "sha256:replay", Contract: "rule", ContractVersion: 1, ViolationClass: "generic"}
	minimized := schedule.WithReleasePrefix(nil)
	c, err := capsule.Build(capsule.Input{Campaign: spec, CampaignYAML: raw, RunID: runID, AttemptID: attemptID, Execution: capsule.Execution{Status: "violation"}, Schedule: schedule, Snapshot: ingest.Snapshot{RunID: runID, AttemptID: attemptID, Events: []model.Event{event}}, View: evidence.View{RunID: runID, AttemptID: attemptID, Events: []model.Event{event}}, Signatures: []signature.Signature{sig}, Minimization: &minimize.Result{Original: schedule, Minimized: minimized, Target: sig, Stable: true}})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "capsule")
	if err := c.WriteDirectory(root); err != nil {
		t.Fatal(err)
	}
	return root
}
