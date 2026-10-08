package temporal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/model"
	"go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fixtureHistory struct{ events []*historypb.HistoryEvent }

func (f fixtureHistory) GetWorkflowHistory(context.Context, string, string, bool, enums.HistoryEventFilterType) client.HistoryEventIterator {
	return &fixtureIterator{events: f.events}
}

type fixtureIterator struct{ events []*historypb.HistoryEvent }

func (f *fixtureIterator) HasNext() bool { return len(f.events) != 0 }
func (f *fixtureIterator) Next() (*historypb.HistoryEvent, error) {
	event := f.events[0]
	f.events = f.events[1:]
	return event, nil
}

func sdkEvent(id int64, typ enums.EventType) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: id, EventType: typ, EventTime: timestamppb.New(time.Unix(id, 0))}
}

func scheduled(id int64, activityID string) *historypb.HistoryEvent {
	event := sdkEvent(id, enums.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED)
	event.Attributes = &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{ActivityId: activityID}}
	return event
}

func started(id, schedule int64, attempt int32) *historypb.HistoryEvent {
	event := sdkEvent(id, enums.EVENT_TYPE_ACTIVITY_TASK_STARTED)
	event.Attributes = &historypb.HistoryEvent_ActivityTaskStartedEventAttributes{ActivityTaskStartedEventAttributes: &historypb.ActivityTaskStartedEventAttributes{ScheduledEventId: schedule, Attempt: attempt}}
	return event
}

func TestFetchCorrelatesInterleavedActivitiesAndRetryAttempt(t *testing.T) {
	completed := sdkEvent(5, enums.EVENT_TYPE_ACTIVITY_TASK_COMPLETED)
	completed.Attributes = &historypb.HistoryEvent_ActivityTaskCompletedEventAttributes{ActivityTaskCompletedEventAttributes: &historypb.ActivityTaskCompletedEventAttributes{ScheduledEventId: 2, StartedEventId: 3}}
	failed := sdkEvent(6, enums.EVENT_TYPE_ACTIVITY_TASK_FAILED)
	failed.Attributes = &historypb.HistoryEvent_ActivityTaskFailedEventAttributes{ActivityTaskFailedEventAttributes: &historypb.ActivityTaskFailedEventAttributes{ScheduledEventId: 1, StartedEventId: 4, RetryState: enums.RETRY_STATE_MAXIMUM_ATTEMPTS_REACHED}}
	snapshot, err := FetchFrom(context.Background(), fixtureHistory{[]*historypb.HistoryEvent{scheduled(1, "charge"), scheduled(2, "email"), started(3, 2, 1), started(4, 1, 3), completed, failed}}, LiveOptions{WorkflowID: "checkout", RunID: "run"})
	if err != nil || !snapshot.Complete() {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot})
	if len(view.Tasks) != 2 || view.Tasks[0].State != model.TaskFailed || view.Tasks[1].State != model.TaskCompleted {
		t.Fatalf("tasks=%#v issues=%#v", view.Tasks, view.Issues)
	}
	if snapshot.Events[0].EntityID != snapshot.Events[3].EntityID || snapshot.Events[0].EntityID != snapshot.Events[5].EntityID || snapshot.Events[1].EntityID != snapshot.Events[2].EntityID || snapshot.Events[1].EntityID != snapshot.Events[4].EntityID {
		t.Fatalf("activity lifecycle identities differ: %#v", snapshot.Events)
	}
	if snapshot.Events[3].Attributes["temporal.attempt"] != "3" {
		t.Fatal("retry attempt was lost")
	}
	for _, event := range snapshot.Events {
		if event.ParentEntityID == event.EntityID {
			t.Fatal("activity is its own parent")
		}
	}
}

func TestFetchCancellationAndTimeoutAreDifferentTerminalFacts(t *testing.T) {
	request := sdkEvent(3, enums.EVENT_TYPE_ACTIVITY_TASK_CANCEL_REQUESTED)
	request.Attributes = &historypb.HistoryEvent_ActivityTaskCancelRequestedEventAttributes{ActivityTaskCancelRequestedEventAttributes: &historypb.ActivityTaskCancelRequestedEventAttributes{ScheduledEventId: 1}}
	canceled := sdkEvent(4, enums.EVENT_TYPE_ACTIVITY_TASK_CANCELED)
	canceled.Attributes = &historypb.HistoryEvent_ActivityTaskCanceledEventAttributes{ActivityTaskCanceledEventAttributes: &historypb.ActivityTaskCanceledEventAttributes{ScheduledEventId: 1, StartedEventId: 2, LatestCancelRequestedEventId: 3}}
	timedOut := sdkEvent(7, enums.EVENT_TYPE_ACTIVITY_TASK_TIMED_OUT)
	timedOut.Attributes = &historypb.HistoryEvent_ActivityTaskTimedOutEventAttributes{ActivityTaskTimedOutEventAttributes: &historypb.ActivityTaskTimedOutEventAttributes{ScheduledEventId: 5, StartedEventId: 6, RetryState: enums.RETRY_STATE_TIMEOUT}}
	snapshot, err := FetchFrom(context.Background(), fixtureHistory{[]*historypb.HistoryEvent{scheduled(1, "charge"), started(2, 1, 1), request, canceled, scheduled(5, "charge"), started(6, 5, 1), timedOut}}, LiveOptions{WorkflowID: "checkout"})
	if err != nil || !snapshot.Complete() {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot})
	if len(view.Tasks) != 2 || view.Tasks[0].State != model.TaskCancelled || view.Tasks[1].State != model.TaskFailed || view.Tasks[0].ID == view.Tasks[1].ID {
		t.Fatalf("tasks=%#v", view.Tasks)
	}
	if len(view.Cancellations) != 1 || view.Cancellations[0].TargetTaskID != view.Tasks[0].ID {
		t.Fatalf("cancellations=%#v", view.Cancellations)
	}
	if snapshot.Events[6].Attributes["cause"] != "timeout" {
		t.Fatal("timeout cause was lost")
	}
	if view.HasCapability(evidence.CapabilityCancellationObserved) {
		t.Fatal("history invented activity cancellation observation")
	}
}

func TestFetchMissingCorrelationOrSequenceIsIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []*historypb.HistoryEvent
		reason string
	}{
		{"missing-schedule", []*historypb.HistoryEvent{started(1, 99, 1)}, "activity_missing_schedule"},
		{"missing-identity", []*historypb.HistoryEvent{scheduled(1, "")}, "activity_missing_identity"},
		{"missing-event", []*historypb.HistoryEvent{scheduled(1, "charge"), started(3, 1, 1)}, "history_sequence_gap"},
		{"duplicate-event", []*historypb.HistoryEvent{scheduled(1, "charge"), started(1, 1, 1)}, "history_sequence_gap"},
		{"unsupported-event", []*historypb.HistoryEvent{sdkEvent(1, enums.EVENT_TYPE_MARKER_RECORDED)}, "markerrecorded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := FetchFrom(context.Background(), fixtureHistory{tc.events}, LiveOptions{WorkflowID: "checkout"})
			if err != nil || snapshot.Complete() || !strings.Contains(strings.Join(snapshot.IncompleteReasons, " "), tc.reason) {
				t.Fatalf("snapshot=%#v err=%v", snapshot, err)
			}
		})
	}
}

func TestFetchActivityCanCancelOrTimeOutBeforeStarting(t *testing.T) {
	canceled := sdkEvent(2, enums.EVENT_TYPE_ACTIVITY_TASK_CANCELED)
	canceled.Attributes = &historypb.HistoryEvent_ActivityTaskCanceledEventAttributes{ActivityTaskCanceledEventAttributes: &historypb.ActivityTaskCanceledEventAttributes{ScheduledEventId: 1}}
	timedOut := sdkEvent(4, enums.EVENT_TYPE_ACTIVITY_TASK_TIMED_OUT)
	timedOut.Attributes = &historypb.HistoryEvent_ActivityTaskTimedOutEventAttributes{ActivityTaskTimedOutEventAttributes: &historypb.ActivityTaskTimedOutEventAttributes{ScheduledEventId: 3}}
	snapshot, err := FetchFrom(context.Background(), fixtureHistory{[]*historypb.HistoryEvent{scheduled(1, "never-started-cancel"), canceled, scheduled(3, "never-started-timeout"), timedOut}}, LiveOptions{WorkflowID: "checkout"})
	if err != nil || !snapshot.Complete() {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot})
	if len(view.Tasks) != 2 || view.Tasks[0].State != model.TaskCancelled || view.Tasks[1].State != model.TaskLost {
		t.Fatalf("tasks=%#v issues=%#v", view.Tasks, view.Issues)
	}
	for _, issue := range view.Issues {
		if issue.Code == "task_transition" || issue.Code == "missing_task" {
			t.Fatalf("terminal event failed to correlate: %#v", issue)
		}
	}
}
