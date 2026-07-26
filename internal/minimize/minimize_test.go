package minimize

import (
	"context"
	"testing"

	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/signature"
)

func TestMinimizeRemovesUnnecessaryReleasePrefix(t *testing.T) {
	t.Parallel()

	target := signature.Signature{Digest: "sha256:target"}
	original := explorer.Schedule{ID: "schedule_original", Ordinal: 1, Strategy: "bounded-prefix", CancelAt: "cut", ReleasePrefix: []string{"a", "b", "c"}}
	result, err := Minimize(context.Background(), original, target, Config{MaxAttempts: 30, Confirmations: 3}, func(_ context.Context, candidate explorer.Schedule) ([]signature.Signature, error) {
		if len(candidate.ReleasePrefix) == 1 {
			return []signature.Signature{target}, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Minimized.ReleasePrefix) != 1 || !result.Stable || result.BudgetExhausted {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Attempts) == 0 {
		t.Fatal("minimizer made no attempts")
	}
}

func TestMinimizeReportsBudgetAndRejectsDifferentSignature(t *testing.T) {
	t.Parallel()

	target := signature.Signature{Digest: "sha256:target"}
	original := explorer.Schedule{ID: "schedule_original", Strategy: "single-cut", CancelAt: "cut", ReleasePrefix: []string{"a"}}
	result, err := Minimize(context.Background(), original, target, Config{MaxAttempts: 1, Confirmations: 3}, func(_ context.Context, _ explorer.Schedule) ([]signature.Signature, error) {
		return []signature.Signature{{Digest: "sha256:other"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.BudgetExhausted || result.Stable {
		t.Fatalf("result = %#v", result)
	}
}
