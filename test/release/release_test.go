package release_test

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/adapters/native"
	"github.com/abhisek343/cutline/internal/capsule"
	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/minimize"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/abhisek343/cutline/internal/replay"
	"github.com/abhisek343/cutline/internal/report"
	"github.com/abhisek343/cutline/internal/signature"
)

func TestReferenceCheckoutReleaseGate(t *testing.T) {
	if testing.Short() {
		t.Skip("builds real CLI and replays saved capsule 20 times")
	}
	root := repositoryRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "cutline")
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", binary, "./cmd/cutline")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	lastExit := 0
	invoke := func(wantExit int, args ...string) []byte {
		t.Helper()
		command := exec.CommandContext(ctx, binary, args...)
		command.Dir = root
		output, err := command.Output()
		gotExit := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("CLI %v: %v", args, err)
			}
			gotExit = exit.ExitCode()
			if wantExit >= 0 && gotExit != wantExit {
				t.Fatalf("CLI %v exit=%d want=%d stderr=%s stdout=%s", args, gotExit, wantExit, exit.Stderr, output)
			}
		}
		if wantExit >= 0 && gotExit != wantExit {
			t.Fatalf("CLI %v exit=%d want=%d output=%s", args, gotExit, wantExit, output)
		}
		lastExit = gotExit
		return output
	}
	faulty := "test/fixtures/checkout/campaign-faulty.yaml"
	var found native.CampaignResult
	decode(t, invoke(2, "run", "--campaign", faulty, "--json"), &found)
	if found.Status != native.OverallViolation || len(found.Signatures) != 1 {
		t.Fatalf("faulty CLI result: %s signatures=%d", found.Status, len(found.Signatures))
	}
	directory, err := os.MkdirTemp(root, "cutline-release-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	destination := filepath.Join(directory, "capsule")
	relative, err := filepath.Rel(root, destination)
	if err != nil {
		t.Fatal(err)
	}
	var reduced struct {
		Schedule     explorer.Schedule   `json:"schedule"`
		Signature    signature.Signature `json:"signature"`
		Minimization minimize.Result     `json:"minimization"`
	}
	decode(t, invoke(2, "minimize", "--campaign", faulty, "--output", relative, "--max-attempts", "10", "--confirmations", "2", "--json"), &reduced)
	manifest, err := capsule.ValidateDirectory(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !reduced.Minimization.Stable || reduced.Signature.Digest != found.Signatures[0].Digest || manifest.FailureSignature.Digest != reduced.Signature.Digest {
		t.Fatalf("minimization did not preserve original signature: %#v", reduced)
	}
	var saved explorer.Schedule
	scheduleData, err := capsule.ReadArtifact(destination, "schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	decode(t, scheduleData, &saved)
	if !reflect.DeepEqual(saved, reduced.Minimization.Minimized) || !reflect.DeepEqual(saved, reduced.Schedule) {
		t.Fatalf("saved/CLI schedules do not match minimized schedule")
	}
	var execution capsule.Execution
	executionData, err := capsule.ReadArtifact(destination, "execution.json")
	if err != nil {
		t.Fatal(err)
	}
	decode(t, executionData, &execution)
	if execution.Status != "violation" || execution.ExitCode != 0 {
		t.Fatalf("final execution metadata=%#v", execution)
	}
	exact := 0
	for attempt := 0; attempt < 20; attempt++ {
		var result replay.Result
		decode(t, invoke(-1, "replay", destination, "--json"), &result)
		if result.Exact && lastExit != 2 || !result.Exact && lastExit != 3 {
			t.Fatalf("replay JSON verdict and process exit disagree: exact=%t status=%s exit=%d", result.Exact, result.Status, lastExit)
		}
		if result.Exact && !result.Comparative && result.Status == replay.StatusReproduced && result.Expected.Digest == manifest.FailureSignature.Digest && len(result.Observed) == 1 && result.Observed[0].Digest == result.Expected.Digest {
			exact++
		}
	}
	t.Logf("saved capsule exact replay stability: %d/20", exact)
	if exact < 19 {
		t.Fatalf("saved capsule replay stability=%d/20, want at least 19", exact)
	}
	var data report.Data
	decode(t, invoke(0, "report", destination, "--format", "json"), &data)
	if data.Status != "violation" || data.Signature.Digest != manifest.FailureSignature.Digest || !data.View.Complete() || data.View.RunID != manifest.RunID || data.View.AttemptID != manifest.AttemptID || !reflect.DeepEqual(data.Schedule, saved) {
		t.Fatalf("report is inconsistent with capsule execution: %#v", data)
	}
	witnessed := false
	for _, effect := range data.View.Effects {
		if effect.Kind == "payment.charge" && effect.State == model.EffectCommitted {
			witnessed = true
		}
	}
	if !witnessed || len(data.Evaluations) != 1 || data.Evaluations[0].Status != "violation" {
		t.Fatalf("report lacks committed offending effect/violation evidence: %#v", data)
	}
	rendered := html.UnescapeString(string(invoke(0, "report", destination, "--format", "html")))
	for _, required := range []string{data.Signature.Digest, "no-charge-after-cancel", "payment.charge", string(manifest.AttemptID), "violation"} {
		if !strings.Contains(rendered, required) {
			t.Fatalf("HTML report missing evidence %q", required)
		}
	}
	var clean native.CampaignResult
	decode(t, invoke(0, "run", "--campaign", "test/fixtures/checkout/campaign-clean.yaml", "--json"), &clean)
	if clean.Status != native.OverallPass || len(clean.Signatures) != 0 {
		t.Fatalf("clean CLI gate status=%s signatures=%d", clean.Status, len(clean.Signatures))
	}
	for _, scenario := range []struct {
		name   string
		exit   int
		status native.OverallStatus
	}{{"late", 2, native.OverallViolation}, {"stuck", 3, native.OverallInconclusive}, {"crash", 3, native.OverallInconclusive}} {
		t.Run("registered-descendant-"+scenario.name, func(t *testing.T) {
			var result native.CampaignResult
			decode(t, invoke(scenario.exit, "run", "--campaign", "test/fixtures/sdkdrain/campaign-"+scenario.name+".yaml", "--json"), &result)
			if result.Status != scenario.status {
				t.Fatalf("descendant scenario=%s status=%s expected=%s", scenario.name, result.Status, scenario.status)
			}
			if scenario.name == "late" {
				if len(result.Signatures) != 1 || result.Signatures[0].Contract != "no-email-after-cancel" || len(result.Schedules) != 1 {
					t.Fatalf("late effect violation lacks witness: %#v", result)
				}
				run := result.Schedules[0]
				returned := uint64(0)
				for _, event := range run.Evidence.Events {
					if event.Type == model.EventTargetReturned {
						returned = event.CanonicalOrder
					}
				}
				late := false
				for _, effect := range run.View.Effects {
					if effect.Kind == "email.send" && effect.State == model.EffectCommitted && effect.CommitOrder > returned {
						late = true
					}
				}
				if returned == 0 || !late || !run.View.Complete() {
					t.Fatal("late effect not authoritatively observed after root return with complete drain")
				}
			} else {
				if len(result.Signatures) != 0 {
					t.Fatal("incomplete drain emitted failure signature")
				}
				incomplete := len(result.Discovery.View.IncompleteReasons) > 0 || len(result.Discovery.View.Issues) > 0
				for _, run := range result.Schedules {
					incomplete = incomplete || !run.View.Complete()
				}
				if !incomplete {
					t.Fatal("inconclusive drain result lacks explicit incomplete evidence")
				}
			}
		})
	}
}

func decode(t *testing.T, data []byte, value any) {
	t.Helper()
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("decode CLI output: %v\n%s", err, data)
	}
}
func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
