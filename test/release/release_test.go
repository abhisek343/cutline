package release_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/adapters/native"
	"github.com/abhisek343/cutline/internal/campaign"
)

func TestReferenceCheckoutReleaseGate(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the reference fixture 21 times")
	}
	root := repositoryRoot(t)
	faulty, err := campaign.LoadFile(filepath.Join(root, "test", "fixtures", "checkout", "campaign-faulty.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	clean, err := campaign.LoadFile(filepath.Join(root, "test", "fixtures", "checkout", "campaign-clean.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	runner := native.Runner{BaseDirectory: root}
	digest := ""
	exact := 0
	for attempt := 0; attempt < 20; attempt++ {
		result, runErr := runner.Run(ctx, faulty)
		if runErr != nil || result.Status != native.OverallViolation || len(result.Signatures) != 1 {
			t.Fatalf("faulty attempt %d: status=%s signatures=%d error=%v", attempt+1, result.Status, len(result.Signatures), runErr)
		}
		if digest == "" {
			digest = result.Signatures[0].Digest
		}
		if result.Signatures[0].Digest == digest {
			exact++
		}
	}
	if exact < 19 {
		t.Fatalf("reference replay stability = %d/20, want at least 19", exact)
	}
	cleanResult, err := runner.Run(ctx, clean)
	if err != nil || cleanResult.Status != native.OverallPass || len(cleanResult.Signatures) != 0 {
		t.Fatalf("clean release gate: status=%s signatures=%d error=%v", cleanResult.Status, len(cleanResult.Signatures), err)
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
