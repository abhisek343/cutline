package explorer

import (
	"testing"

	"github.com/abhisek343/cutline/internal/model"
)

func TestDiscoverPreservesCanonicalCheckpointOrder(t *testing.T) {
	t.Parallel()

	events := []model.Event{
		{CanonicalOrder: 3, Type: model.EventCheckpointReached, Attributes: map[string]string{"point": "before-email"}},
		{CanonicalOrder: 1, Type: model.EventCheckpointReached, Attributes: map[string]string{"point": "before-charge"}},
		{CanonicalOrder: 2, Type: model.EventCheckpointReleased, Attributes: map[string]string{"point": "before-charge"}},
		{CanonicalOrder: 4, Type: model.EventCheckpointReached, Attributes: map[string]string{"point": "before-email"}},
	}
	discovery := Discover(events, nil, 10)
	if len(discovery.Checkpoints) != 2 {
		t.Fatalf("checkpoints = %#v", discovery.Checkpoints)
	}
	if discovery.Checkpoints[0].Name != "before-charge" || discovery.Checkpoints[1].Name != "before-email" {
		t.Fatalf("order = %#v", discovery.Checkpoints)
	}
	if discovery.Checkpoints[1].Visits != 2 {
		t.Fatalf("before-email visits = %d", discovery.Checkpoints[1].Visits)
	}
	if !discovery.Complete() {
		t.Fatalf("discovery = %#v", discovery)
	}
}

func TestDiscoverDisclosesPointVisitBound(t *testing.T) {
	t.Parallel()

	events := []model.Event{
		{CanonicalOrder: 1, Type: model.EventCheckpointReached, Attributes: map[string]string{"point": "a"}},
		{CanonicalOrder: 2, Type: model.EventCheckpointReached, Attributes: map[string]string{"point": "b"}},
	}
	discovery := Discover(events, []string{"target stopped"}, 1)
	if !discovery.Truncated || len(discovery.Checkpoints) != 1 || discovery.Checkpoints[0].Name != "a" {
		t.Fatalf("discovery = %#v", discovery)
	}
	if discovery.Complete() {
		t.Fatal("truncated discovery reported complete")
	}
}

func TestEnumerateSingleCutFiltersInDiscoveryOrder(t *testing.T) {
	t.Parallel()

	discovery := Discovery{Checkpoints: []Checkpoint{
		{Name: "first", FirstOrder: 2, Visits: 1},
		{Name: "second", FirstOrder: 4, Visits: 1},
		{Name: "third", FirstOrder: 6, Visits: 1},
	}}
	plan, err := EnumerateSingleCut(discovery, []string{"third", "first", "second", "missing"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Schedules) != 2 || plan.Schedules[0].CancelAt != "first" || plan.Schedules[1].CancelAt != "second" {
		t.Fatalf("schedules = %#v", plan.Schedules)
	}
	if len(plan.Unreachable) != 1 || plan.Unreachable[0] != "missing" {
		t.Fatalf("unreachable = %#v", plan.Unreachable)
	}
	if !plan.ScheduleLimitReached || plan.Complete() {
		t.Fatalf("plan = %#v", plan)
	}
	again, err := EnumerateSingleCut(discovery, []string{"third", "first", "second", "missing"}, 2)
	if err != nil || plan.Schedules[0].ID != again.Schedules[0].ID {
		t.Fatalf("schedule identity is not stable: %#v %#v", plan, again)
	}
}

func TestEnumerateSingleCutRejectsInvalidBound(t *testing.T) {
	t.Parallel()

	if _, err := EnumerateSingleCut(Discovery{}, nil, 0); err == nil {
		t.Fatal("zero max schedules accepted")
	}
}
