package contracts

import (
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
)

func TestCELTemporalHelpersRespectBoundaries(t *testing.T) {
	t.Parallel()

	boundary := time.Unix(2_000, 0).UTC()
	cases := []struct {
		name       string
		expression string
		transition func(time.Time) time.Time
		want       Status
	}{
		{name: "terminal before", expression: "tasks.all(t, t.isTerminalAt(run.drainedAt))", transition: func(at time.Time) time.Time { return at.Add(-time.Second) }, want: StatusPass},
		{name: "terminal exactly at", expression: "tasks.all(t, t.isTerminalAt(run.drainedAt))", transition: func(at time.Time) time.Time { return at }, want: StatusPass},
		{name: "terminal after", expression: "tasks.all(t, t.isTerminalAt(run.drainedAt))", transition: func(at time.Time) time.Time { return at.Add(time.Second) }, want: StatusViolation},
		{name: "compensated before", expression: "effects.all(e, e.wasCompensatedBefore(run.drainedAt))", transition: func(at time.Time) time.Time { return at.Add(-time.Second) }, want: StatusPass},
		{name: "compensated exactly at", expression: "effects.all(e, e.wasCompensatedBefore(run.drainedAt))", transition: func(at time.Time) time.Time { return at }, want: StatusViolation},
		{name: "compensated after", expression: "effects.all(e, e.wasCompensatedBefore(run.drainedAt))", transition: func(at time.Time) time.Time { return at.Add(time.Second) }, want: StatusViolation},
		{name: "released before", expression: "resources.all(r, r.isReleasedAt(run.drainedAt))", transition: func(at time.Time) time.Time { return at.Add(-time.Second) }, want: StatusPass},
		{name: "released exactly at", expression: "resources.all(r, r.isReleasedAt(run.drainedAt))", transition: func(at time.Time) time.Time { return at }, want: StatusPass},
		{name: "released after", expression: "resources.all(r, r.isReleasedAt(run.drainedAt))", transition: func(at time.Time) time.Time { return at.Add(time.Second) }, want: StatusViolation},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			view := temporalBoundaryView(t, boundary, testCase.transition(boundary), testCase.transition(boundary), testCase.transition(boundary))
			result := Evaluate(view, []campaign.ContractSpec{{
				Name: "boundary", Boundary: "run-drained", Expression: testCase.expression,
			}})[0]
			if result.Status != testCase.want {
				t.Fatalf("status = %s, message = %s, want %s", result.Status, result.Message, testCase.want)
			}
		})
	}
}

func TestCELTemporalHelpersFailClosedForMalformedOrMissingBoundaries(t *testing.T) {
	t.Parallel()

	boundary := time.Unix(2_000, 0).UTC()
	view := temporalBoundaryView(t, boundary, boundary, boundary, boundary)
	malformed := Evaluate(view, []campaign.ContractSpec{{
		Name: "malformed", Boundary: "run-drained",
		Expression: "tasks.all(t, t.isTerminalAt(\"not-a-timestamp\"))",
	}})[0]
	if malformed.Status != StatusInconclusive {
		t.Fatalf("malformed boundary status = %s, message = %s", malformed.Status, malformed.Message)
	}

	missingTimestamp := view
	missingTimestamp.Events = append([]model.Event(nil), view.Events...)
	for index := range missingTimestamp.Events {
		if missingTimestamp.Events[index].Type == model.EventDrainCompleted {
			missingTimestamp.Events[index].ObservedAt = time.Time{}
		}
	}
	missing := Evaluate(missingTimestamp, []campaign.ContractSpec{{
		Name: "missing", Boundary: "run-drained", Expression: "true",
	}})[0]
	if missing.Status != StatusInconclusive {
		t.Fatalf("missing boundary status = %s, message = %s", missing.Status, missing.Message)
	}
}

func temporalBoundaryView(t *testing.T, boundary, terminalAt, compensatedAt, releasedAt time.Time) evidence.View {
	t.Helper()
	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	task, _ := model.ContentID("task", t.Name())
	cancel, _ := model.ContentID("cancel", t.Name())
	effect, _ := model.ContentID("effect", t.Name())
	resource, _ := model.ContentID("resource", t.Name())
	base := boundary.Add(-10 * time.Second)
	at := func(sequence uint64, typ model.EventType, id, parent string, observed time.Time, attributes map[string]string) model.Event {
		return model.Event{
			SchemaVersion: model.EventSchemaVersion, RunID: model.RunID(run), AttemptID: model.AttemptID(attempt),
			SessionID: model.SessionID(session), LocalSequence: sequence, CanonicalOrder: sequence,
			Type: typ, EntityID: id, ParentEntityID: parent, ObservedAt: observed, Attributes: attributes,
		}
	}
	events := []model.Event{
		at(1, model.EventSessionStarted, session, "", base, nil),
		at(2, model.EventTaskRegistered, task, "", base, map[string]string{"kind": "native.root", "name": "test"}),
		at(3, model.EventTaskStarted, task, "", base, nil),
		at(4, model.EventCancelRequested, cancel, "", base.Add(time.Second), map[string]string{"targetTask": task, "trigger": "explicit"}),
		at(5, model.EventCancelDelivered, cancel, "", base.Add(2*time.Second), nil),
		at(6, model.EventCancelObserved, cancel, task, base.Add(3*time.Second), nil),
		at(7, model.EventEffectDeclared, effect, task, base, map[string]string{"kind": "inventory.reserve", "idempotencyKey": "order-1", "evidenceSource": "fixture"}),
		at(8, model.EventEffectAttempted, effect, task, base, nil),
		at(9, model.EventEffectCommitted, effect, task, base, nil),
		at(10, model.EventEffectCompensated, effect, task, compensatedAt, nil),
		at(11, model.EventResourceAcquired, resource, task, base, map[string]string{"kind": "lock", "name": "order"}),
		at(12, model.EventResourceReleased, resource, task, releasedAt, nil),
		at(13, model.EventTaskFinished, task, "", terminalAt, map[string]string{"status": string(model.TaskCompleted)}),
		at(14, model.EventDrainCompleted, task, "", boundary, nil),
		at(15, model.EventSessionEnded, session, "", boundary.Add(time.Second), nil),
	}
	return evidence.Build(evidence.BuildInput{
		Snapshot:                     ingest.Snapshot{RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), Events: events},
		AuthoritativeEffectsComplete: true,
	})
}
