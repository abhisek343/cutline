package ingest

import (
	"errors"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/model"
)

func TestMemoryAssignsOrderAndFreezes(t *testing.T) {
	t.Parallel()

	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	entity, _ := model.ContentID("task", t.Name())
	store := NewMemory(model.RunID(run), model.AttemptID(attempt), 10)
	event := model.Event{
		SchemaVersion: model.EventSchemaVersion,
		RunID:         model.RunID(run),
		AttemptID:     model.AttemptID(attempt),
		SessionID:     model.SessionID(session),
		LocalSequence: 1,
		Type:          model.EventTaskStarted,
		EntityID:      entity,
		ObservedAt:    time.Now().UTC(),
		Attributes:    map[string]string{"status": "running"},
	}
	accepted, err := store.Append(event)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if accepted.CanonicalOrder != 1 {
		t.Fatalf("CanonicalOrder = %d", accepted.CanonicalOrder)
	}

	snapshot, err := store.Freeze()
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}
	snapshot.Events[0].Attributes["status"] = "mutated"
	current, err := store.Current()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if got := current[0].Attributes["status"]; got != "running" {
		t.Fatalf("stored event mutated through snapshot: %q", got)
	}
	if _, err := store.Append(event); !errors.Is(err, ErrFrozen) {
		t.Fatalf("Append(after freeze) error = %v", err)
	}
}

func TestMemoryRejectsGapAndCannotPassAsComplete(t *testing.T) {
	t.Parallel()

	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	entity, _ := model.ContentID("task", t.Name())
	store := NewMemory(model.RunID(run), model.AttemptID(attempt), 10)
	event := model.Event{
		SchemaVersion: model.EventSchemaVersion,
		RunID:         model.RunID(run),
		AttemptID:     model.AttemptID(attempt),
		SessionID:     model.SessionID(session),
		LocalSequence: 2,
		Type:          model.EventTaskStarted,
		EntityID:      entity,
		ObservedAt:    time.Now().UTC(),
	}
	if _, err := store.Append(event); !errors.Is(err, ErrSequenceGap) {
		t.Fatalf("Append(gap) error = %v", err)
	}
	snapshot, err := store.Freeze()
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}
	if snapshot.Complete() {
		t.Fatal("snapshot with a sequence gap reported complete")
	}
}
