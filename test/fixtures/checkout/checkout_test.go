package checkout

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/pkg/cutline"
)

var fixtureMode = flag.String("cutline-fixture-mode", ModeFaulty, "checkout fixture mode")

func TestCutlineTarget(t *testing.T) {
	if os.Getenv("CUTLINE_ENDPOINT") == "" {
		t.Skip("requires Cutline native adapter")
	}
	ledgerPath := os.Getenv("CUTLINE_FIXTURE_LEDGER")
	if ledgerPath == "" {
		t.Fatal("CUTLINE_FIXTURE_LEDGER is missing")
	}
	err := cutline.Run(context.Background(), "checkout", func(ctx context.Context) error {
		return Execute(ctx, *fixtureMode, ledgerPath)
	})
	if !IsExpectedCancellation(err) {
		t.Fatalf("Execute() error = %v, want injected cancellation", err)
	}
}

func TestCleanAndFaultyBehaviorWithoutCoordinator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mode        string
		wantEffects int
	}{
		{name: "clean", mode: ModeClean, wantEffects: 0},
		{name: "faulty", mode: ModeFaulty, wantEffects: 1},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			ledgerPath := filepath.Join(t.TempDir(), "effects.jsonl")
			err := Execute(ctx, tt.mode, ledgerPath)
			if !IsExpectedCancellation(err) {
				t.Fatalf("Execute() error = %v", err)
			}
			records, err := fixtureledger.Read(ledgerPath, 10)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if len(records) != tt.wantEffects {
				t.Fatalf("effects = %d, want %d", len(records), tt.wantEffects)
			}
		})
	}
}
