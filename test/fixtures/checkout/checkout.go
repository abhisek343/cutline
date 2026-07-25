package checkout

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/pkg/cutline"
)

const (
	ModeFaulty = "faulty"
	ModeClean  = "clean"
)

// Execute models a checkout around the cancellation-sensitive payment boundary.
// The faulty variant deliberately ignores Point's cancellation error.
func Execute(ctx context.Context, mode, ledgerPath string) error {
	pointErr := cutline.Point(ctx, "before-charge")
	switch mode {
	case ModeClean:
		if pointErr != nil {
			return pointErr
		}
	case ModeFaulty:
		// Deliberate benchmark bug: cancellation is observed but ignored.
	default:
		return fmt.Errorf("unknown checkout mode %q", mode)
	}

	effect, err := cutline.BeginEffect(ctx, cutline.EffectSpec{
		Kind:           "payment.charge",
		IdempotencyKey: "order-1001",
		EvidenceSource: "fixture.payment-ledger",
	})
	if err != nil {
		return err
	}
	if err := effect.Attempt(ctx); err != nil {
		return err
	}
	record := fixtureledger.Record{
		EffectID:       effect.ID(),
		Kind:           "payment.charge",
		IdempotencyKey: "order-1001",
		CommittedAt:    time.Now().UTC(),
		Source:         "fixture.payment-ledger",
	}
	if err := fixtureledger.Append(ledgerPath, record); err != nil {
		return err
	}
	if err := effect.Commit(ctx); err != nil {
		return err
	}
	if pointErr != nil {
		return pointErr
	}
	return context.Cause(ctx)
}

func IsExpectedCancellation(err error) bool {
	return errors.Is(err, cutline.ErrInjectedCancellation) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}
