package fixtureledger

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndRead(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "effects.jsonl")
	record := Record{
		EffectID:       "effect_0123456789abcdef0123456789abcdef",
		Kind:           "payment.charge",
		IdempotencyKey: "order-1",
		CommittedAt:    time.Now().UTC(),
		Source:         "fixture.payment-ledger",
	}
	if err := Append(path, record); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	records, err := Read(path, 10)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(records) != 1 || records[0].EffectID != record.EffectID {
		t.Fatalf("Read() = %#v", records)
	}
}
