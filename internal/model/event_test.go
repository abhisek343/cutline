package model

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validEvent(t *testing.T) Event {
	t.Helper()
	run, _ := ContentID("run", t.Name())
	attempt, _ := ContentID("attempt", t.Name())
	session, _ := ContentID("session", t.Name())
	entity, _ := ContentID("task", t.Name())
	return Event{
		SchemaVersion: EventSchemaVersion,
		RunID:         RunID(run),
		AttemptID:     AttemptID(attempt),
		SessionID:     SessionID(session),
		LocalSequence: 1,
		Type:          EventTaskStarted,
		EntityID:      entity,
		ObservedAt:    time.Unix(1, 0).UTC(),
	}
}

func TestEventValidate(t *testing.T) {
	t.Parallel()

	event := validEvent(t)
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestEventValidateRejectsInvalidFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Event)
	}{
		{"schema", func(e *Event) { e.SchemaVersion = 2 }},
		{"run", func(e *Event) { e.RunID = "" }},
		{"sequence", func(e *Event) { e.LocalSequence = 0 }},
		{"type", func(e *Event) { e.Type = "mystery" }},
		{"time", func(e *Event) { e.ObservedAt = time.Time{} }},
		{"attribute", func(e *Event) { e.Attributes = map[string]string{" ": "bad"} }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			event := validEvent(t)
			tt.mutate(&event)
			if err := event.Validate(); !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("Validate() error = %v, want ErrInvalidEvent", err)
			}
		})
	}
}

func TestCanonicalJSONSortsMapKeys(t *testing.T) {
	t.Parallel()

	data, err := CanonicalJSON(map[string]string{"z": "last", "a": "first"})
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	if got := string(data); got != `{"a":"first","z":"last"}` {
		t.Fatalf("CanonicalJSON() = %s", got)
	}
	if strings.Contains(string(data), "\n") {
		t.Fatal("CanonicalJSON() unexpectedly indented output")
	}
}
