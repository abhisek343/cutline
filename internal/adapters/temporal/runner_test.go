package temporal

import (
	"testing"

	"github.com/abhisek343/cutline/internal/campaign"
)

func TestRunnerPrepareTemporalCampaign(t *testing.T) {
	spec := campaign.Campaign{Target: campaign.TargetSpec{Adapter: "temporal", Command: []string{"go", "test"}, Temporal: campaign.TemporalTargetSpec{Address: "127.0.0.1:7233", Namespace: "payments"}}}
	prepared, runner, err := (Runner{}).prepare(spec)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Target.Adapter != "go-command" || runner.AdapterVersion != AdapterMajorVersion {
		t.Fatalf("prepared=%#v runner=%#v", prepared.Target, runner)
	}
	if runner.Environment["CUTLINE_TEMPORAL_ADDRESS"] != "127.0.0.1:7233" || runner.Environment["CUTLINE_TEMPORAL_NAMESPACE"] != "payments" {
		t.Fatalf("environment=%#v", runner.Environment)
	}
}

func TestRunnerPrepareRejectsRemoteTemporal(t *testing.T) {
	spec := campaign.Campaign{Target: campaign.TargetSpec{Adapter: "temporal", Temporal: campaign.TemporalTargetSpec{Address: "temporal.example:7233"}}}
	if _, _, err := (Runner{}).prepare(spec); err == nil {
		t.Fatal("accepted remote Temporal address")
	}
}
