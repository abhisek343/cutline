package scheduler

import (
	"testing"

	"github.com/abhisek343/cutline/internal/control"
)

func TestPrefixCutReleasesOnlyDeclaredPrefix(t *testing.T) {
	t.Parallel()

	cut, err := NewPrefixCut("third", []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if got := cut.Decide("first"); got != control.ActionRelease {
		t.Fatalf("first action = %s", got)
	}
	if got := cut.Decide("second"); got != control.ActionRelease {
		t.Fatalf("second action = %s", got)
	}
	if got := cut.Decide("third"); got != control.ActionCancel {
		t.Fatalf("third action = %s", got)
	}
	if got := cut.Decide("fourth"); got != control.ActionRelease {
		t.Fatalf("post-cancel action = %s", got)
	}
}

func TestPrefixCutRemovingReleaseMovesCutEarlier(t *testing.T) {
	t.Parallel()

	cut, err := NewPrefixCut("third", []string{"second"})
	if err != nil {
		t.Fatal(err)
	}
	if got := cut.Decide("first"); got != control.ActionCancel || !cut.Injected() {
		t.Fatalf("unexpected early action = %s", got)
	}
}
