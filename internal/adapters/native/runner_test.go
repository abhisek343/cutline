package native

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/contracts"
	"github.com/abhisek343/cutline/internal/model"
)

func TestRunnerFaultyAndCleanCheckout(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns Go fixture processes")
	}
	root := repositoryRoot(t)
	tests := []struct {
		name        string
		campaign    string
		wantStatus  OverallStatus
		wantEffects int
	}{
		{"faulty", "campaign-faulty.yaml", OverallViolation, 1},
		{"clean", "campaign-clean.yaml", OverallPass, 0},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			spec, err := campaign.LoadFile(filepath.Join(root, "test", "fixtures", "checkout", tt.campaign))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := (Runner{BaseDirectory: root}).Run(ctx, spec)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s\nstdout:\n%s\nstderr:\n%s\nreasons=%v",
					result.Status, tt.wantStatus, result.Stdout, result.Stderr, result.Evidence.IncompleteReasons)
			}
			if len(result.Effects) != tt.wantEffects {
				t.Fatalf("effects = %d, want %d", len(result.Effects), tt.wantEffects)
			}
			if tt.wantStatus == OverallViolation && len(result.Signatures) != 1 {
				t.Fatalf("signatures = %#v", result.Signatures)
			}
			if tt.wantStatus == OverallPass && len(result.Signatures) != 0 {
				t.Fatalf("passing run has signatures = %#v", result.Signatures)
			}
			if !result.Evidence.Complete() {
				t.Fatalf("evidence incomplete: %v", result.Evidence.IncompleteReasons)
			}
			if len(result.Evaluations) != 1 {
				t.Fatalf("evaluations = %d", len(result.Evaluations))
			}
			if tt.wantStatus == OverallViolation && result.Evaluations[0].Status != contracts.StatusViolation {
				t.Fatalf("evaluation = %#v", result.Evaluations[0])
			}
			assertCancellationOrder(t, result)
		})
	}
}

func TestRunnerDiscoversBeforeExecutingSingleCut(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns Go fixture processes")
	}
	root := repositoryRoot(t)
	spec, err := campaign.LoadFile(filepath.Join(root, "test", "fixtures", "checkout", "campaign-faulty.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	runner := Runner{BaseDirectory: root}
	discoveryResult, discovery, err := runner.Discover(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !discovery.Complete() || len(discovery.Checkpoints) != 2 || discovery.Checkpoints[0].Name != "before-charge" || discovery.Checkpoints[1].Name != "after-charge" {
		t.Fatalf("discovery = %#v", discovery)
	}
	for _, event := range discoveryResult.Evidence.Events {
		if event.Type == model.EventCancelRequested || event.Type == model.EventCancelObserved {
			t.Fatalf("discovery injected cancellation: %#v", event)
		}
	}

	result, err := runner.RunCampaign(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != OverallViolation || len(result.Schedules) != 1 || result.Plan.Schedules[0].CancelAt != "before-charge" {
		t.Fatalf("campaign result = %#v", result)
	}
	if result.Schedules[0].Status != OverallViolation {
		t.Fatalf("schedule status = %s", result.Schedules[0].Status)
	}
}

func TestRunnerExecutesBoundaryPairAndBoundedPrefixPlans(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns Go fixture processes")
	}
	root := repositoryRoot(t)
	tests := []struct {
		campaign string
		want     int
	}{
		{campaign: "campaign-boundary.yaml", want: 2},
		{campaign: "campaign-prefix.yaml", want: 2},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.campaign, func(t *testing.T) {
			spec, err := campaign.LoadFile(filepath.Join(root, "test", "fixtures", "checkout", testCase.campaign))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			result, err := (Runner{BaseDirectory: root}).RunCampaign(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Schedules) != testCase.want || result.Status != OverallViolation {
				t.Fatalf("status=%s schedules=%d plan=%#v", result.Status, len(result.Schedules), result.Plan)
			}
			for _, schedule := range result.Schedules {
				if schedule.Status != OverallViolation && schedule.Status != OverallPass {
					t.Fatalf("unexpected schedule status = %s", schedule.Status)
				}
			}
		})
	}
}

func TestRunnerWithoutInstrumentationIsInconclusive(t *testing.T) {
	t.Parallel()

	spec := campaign.Campaign{
		APIVersion: campaign.APIVersionV1Alpha1,
		Kind:       campaign.KindCampaign,
		Name:       "no-instrumentation",
		Target:     campaign.TargetSpec{Adapter: "go-command", Command: []string{"/bin/true"}},
		Exploration: campaign.ExplorationSpec{
			Strategy: "single-cut", MaxSchedules: 1, MaxPointVisits: 1,
			ScheduleTimeout: campaign.Duration(time.Second), DrainTimeout: campaign.Duration(time.Second),
			CancelAt: []string{"never"},
		},
		Contracts: []campaign.ContractSpec{{
			Name: "rule", Version: 1, Severity: "critical",
			Expression: `builtin.no_effect_after_cancel_observed("payment.charge")`,
		}},
		Output: campaign.OutputSpec{Directory: ".cutline", Format: "text"},
	}
	result, err := (Runner{BaseDirectory: repositoryRoot(t)}).Run(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != OverallInconclusive || result.Evidence.Complete() {
		t.Fatalf("status=%s complete=%t reasons=%v", result.Status, result.Evidence.Complete(), result.Evidence.IncompleteReasons)
	}
}

func TestResolveWorkingDirectoryRejectsEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := (Runner{BaseDirectory: root}).resolveWorkingDirectory("../outside"); err == nil {
		t.Fatal("resolveWorkingDirectory() accepted escape")
	}
}

func assertCancellationOrder(t *testing.T, result Result) {
	t.Helper()
	var requested, delivered, observed uint64
	for _, event := range result.Evidence.Events {
		switch event.Type {
		case model.EventCancelRequested:
			requested = event.CanonicalOrder
		case model.EventCancelDelivered:
			delivered = event.CanonicalOrder
		case model.EventCancelObserved:
			observed = event.CanonicalOrder
		}
	}
	if requested == 0 || delivered <= requested || observed <= delivered {
		t.Fatalf("cancellation order requested=%d delivered=%d observed=%d", requested, delivered, observed)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root: %v", err)
	}
	return root
}
