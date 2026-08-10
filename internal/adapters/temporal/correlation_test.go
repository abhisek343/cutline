package temporal

import (
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/model"
)

func TestTemporalActivityCorrelationUsesScheduledEventID(t *testing.T) {
	t.Parallel()

	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	base := time.Unix(3_000, 0).UTC()
	history := RunHistory{
		Workflow: WorkflowHistory{
			RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), WorkflowID: "checkout",
			Events: []HistoryEvent{
				{ID: "1", Type: "workflow.started", ObservedAt: base},
				{ID: "100", Type: "workflow.completed", ObservedAt: base.Add(10 * time.Second)},
				{ID: "101", Type: "workflow.drain_completed", ObservedAt: base.Add(11 * time.Second)},
			},
		},
		Activities: []ActivityHistory{{
			RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), ActivityID: "charge",
			Events: []HistoryEvent{
				{ID: "10", Type: "activity.scheduled", ObservedAt: base.Add(time.Second), Attributes: map[string]string{"activityId": "charge", "activityType": "payment.charge"}},
				{ID: "20", Type: "activity.started", ObservedAt: base.Add(2 * time.Second), Attributes: map[string]string{"scheduledEventId": "10"}},
				{ID: "30", Type: "activity.failed", ObservedAt: base.Add(3 * time.Second), Attributes: map[string]string{"scheduledEventId": "10"}},
				{ID: "40", Type: "activity.scheduled", ObservedAt: base.Add(4 * time.Second), Attributes: map[string]string{"activityId": "charge", "activityType": "payment.charge"}},
				{ID: "50", Type: "activity.started", ObservedAt: base.Add(5 * time.Second), Attributes: map[string]string{"scheduledEventId": "40"}},
				{ID: "60", Type: "activity.completed", ObservedAt: base.Add(6 * time.Second), Attributes: map[string]string{"scheduledEventId": "40"}},
				{ID: "70", Type: "activity.cancel_requested", ObservedAt: base.Add(7 * time.Second), Attributes: map[string]string{"scheduledEventId": "10"}},
				{ID: "80", Type: "activity.cancel_requested", ObservedAt: base.Add(8 * time.Second), Attributes: map[string]string{"scheduledEventId": "40"}},
			},
		}},
	}
	snapshot, err := NewAdapter().Translate(history)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Complete() {
		t.Fatalf("snapshot incomplete: %v", snapshot.IncompleteReasons)
	}

	firstTask, err := activityTaskID("checkout", "10")
	if err != nil {
		t.Fatal(err)
	}
	secondTask, err := activityTaskID("checkout", "40")
	if err != nil {
		t.Fatal(err)
	}
	if firstTask == secondTask {
		t.Fatal("retry attempts unexpectedly share a task identity")
	}
	for _, want := range []struct {
		entity string
		status string
	}{
		{entity: firstTask, status: string(model.TaskFailed)},
		{entity: secondTask, status: string(model.TaskCompleted)},
	} {
		found := false
		for _, event := range snapshot.Events {
			if event.Type == model.EventTaskFinished && event.EntityID == want.entity {
				found = true
				if event.Attributes["status"] != want.status {
					t.Fatalf("task %s status = %q, want %q", want.entity, event.Attributes["status"], want.status)
				}
			}
		}
		if !found {
			t.Fatalf("no terminal event for scheduled activity %s: %#v", want.entity, snapshot.Events)
		}
	}

	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot, AuthoritativeEffectsComplete: true})
	if len(view.Tasks) != 2 {
		t.Fatalf("tasks = %#v, want two independently correlated attempts", view.Tasks)
	}
	for _, cancellation := range view.Cancellations {
		if cancellation.TargetTaskID != firstTask && cancellation.TargetTaskID != secondTask {
			t.Fatalf("cancellation target %q was not correlated to a scheduled activity", cancellation.TargetTaskID)
		}
	}
}
func TestTemporalActivityCorrelationFailsClosedWhenScheduleIsAmbiguous(t *testing.T) {
	t.Parallel()

	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	base := time.Unix(4_000, 0).UTC()
	history := RunHistory{
		Workflow: WorkflowHistory{
			RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), WorkflowID: "checkout",
			Events: []HistoryEvent{
				{ID: "1", Type: "workflow.started", ObservedAt: base},
				{ID: "100", Type: "workflow.completed", ObservedAt: base.Add(10 * time.Second)},
				{ID: "101", Type: "workflow.drain_completed", ObservedAt: base.Add(11 * time.Second)},
			},
		},
		Activities: []ActivityHistory{{
			RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), ActivityID: "charge",
			Events: []HistoryEvent{
				{ID: "10", Type: "activity.scheduled", ObservedAt: base.Add(time.Second)},
				{ID: "20", Type: "activity.scheduled", ObservedAt: base.Add(2 * time.Second)},
				{ID: "30", Type: "activity.started", ObservedAt: base.Add(3 * time.Second)},
			},
		}},
	}
	snapshot, err := NewAdapter().Translate(history)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Complete() {
		t.Fatalf("ambiguous activity event was accepted: %#v", snapshot.Events)
	}
	for _, event := range snapshot.Events {
		if event.Type == model.EventTaskStarted {
			t.Fatalf("ambiguous activity start was attributed: %#v", event)
		}
	}
}
