package scheduler

import (
	"errors"
	"sync"
	"testing"

	"github.com/abhisek343/cutline/internal/control"
)

func TestSingleCutCancelsExactlyOnce(t *testing.T) {
	t.Parallel()

	scheduler, err := NewSingleCut("before-charge")
	if err != nil {
		t.Fatal(err)
	}
	if got := scheduler.Decide("before-email"); got != control.ActionRelease {
		t.Fatalf("unrelated decision = %s", got)
	}
	if got := scheduler.Decide("before-charge"); got != control.ActionCancel {
		t.Fatalf("first matching decision = %s", got)
	}
	if got := scheduler.Decide("before-charge"); got != control.ActionRelease {
		t.Fatalf("second matching decision = %s", got)
	}
	if !scheduler.Injected() {
		t.Fatal("scheduler did not record injection")
	}
}

func TestSingleCutIsConcurrentSafe(t *testing.T) {
	t.Parallel()

	scheduler, _ := NewSingleCut("point")
	const calls = 32
	results := make(chan control.Action, calls)
	var group sync.WaitGroup
	for range calls {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- scheduler.Decide("point")
		}()
	}
	group.Wait()
	close(results)

	cancels := 0
	for result := range results {
		if result == control.ActionCancel {
			cancels++
		}
	}
	if cancels != 1 {
		t.Fatalf("cancel decisions = %d, want 1", cancels)
	}
}

func TestSingleCutRejectsBlankPoint(t *testing.T) {
	t.Parallel()

	if _, err := NewSingleCut(" "); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("NewSingleCut() error = %v", err)
	}
}
