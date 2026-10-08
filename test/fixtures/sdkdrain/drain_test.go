package sdkdrain

import (
	"context"
	"errors"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/pkg/cutline"
)

var mode = flag.String("cutline-drain-mode", "late", "registered descendant fixture: late, stuck, or crash")

func TestCutlineTarget(t *testing.T) {
	if os.Getenv("CUTLINE_ENDPOINT") == "" {
		t.Skip("requires Cutline native adapter")
	}
	rootReturning := make(chan struct{})
	outcome := make(chan error, 1)
	err := cutline.Run(context.Background(), "root", func(ctx context.Context) error {
		defer close(rootReturning)
		_, err := cutline.Spawn(ctx, "child", func(childCtx context.Context) error {
			_, err := cutline.Spawn(childCtx, "grandchild", func(grandchildCtx context.Context) error {
				<-rootReturning
				// This delay simulates dependency work continuing after caller return;
				// exact SDK event ordering is asserted without sleeps in lifecycle tests.
				time.Sleep(25 * time.Millisecond)
				switch *mode {
				case "stuck":
					select {} // Intentionally ignores cancellation and cannot drain.
				case "crash":
					panic("fixture child crashed during drain")
				}
				result := commitLate(grandchildCtx)
				outcome <- result
				return result
			})
			return err
		})
		if err != nil {
			return err
		}
		return cutline.Point(ctx, "root-return")
	})
	if *mode == "stuck" {
		if !errors.Is(err, cutline.ErrDrainTimeout) {
			t.Fatalf("Run()=%v, want drain timeout", err)
		}
		return
	}
	if os.Getenv("CUTLINE_DISCOVERY") == "1" {
		if err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, cutline.ErrInjectedCancellation) {
		t.Fatalf("Run()=%v, want injected cancellation", err)
	}
	if err := <-outcome; err != nil {
		t.Fatal(err)
	}
}

func commitLate(ctx context.Context) error {
	effect, err := cutline.BeginEffect(ctx, cutline.EffectSpec{
		Kind: "email.send", IdempotencyKey: "late-mail", EvidenceSource: "fixture-ledger",
	})
	if err != nil {
		return err
	}
	if err := effect.Attempt(ctx); err != nil {
		return err
	}
	if err := fixtureledger.Append(os.Getenv("CUTLINE_FIXTURE_LEDGER"), fixtureledger.Record{
		EffectID: effect.ID(), Kind: "email.send", IdempotencyKey: "late-mail",
		CommittedAt: time.Now().UTC(), Source: "fixture-ledger",
	}); err != nil {
		return err
	}
	return effect.Commit(ctx)
}
