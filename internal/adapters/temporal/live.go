package temporal

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
	"go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
)

// LiveOptions identifies one workflow execution on a local Temporal Service.
// Cutline deliberately rejects non-loopback endpoints: this command reads
// evidence only and is intended for development and test Temporal servers.
type LiveOptions struct {
	Address    string
	Namespace  string
	WorkflowID string
	RunID      string
}

func (o LiveOptions) normalized() (LiveOptions, error) {
	o.Address = strings.TrimSpace(o.Address)
	if o.Address == "" {
		o.Address = "127.0.0.1:7233"
	}
	host, _, err := net.SplitHostPort(o.Address)
	if err != nil || host == "" {
		return LiveOptions{}, fmt.Errorf("Temporal address must be host:port: %q", o.Address)
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return LiveOptions{}, fmt.Errorf("Temporal address must be loopback, got %q", host)
		}
	}
	o.Namespace = strings.TrimSpace(o.Namespace)
	if o.Namespace == "" {
		o.Namespace = "default"
	}
	o.WorkflowID = strings.TrimSpace(o.WorkflowID)
	if o.WorkflowID == "" {
		return LiveOptions{}, fmt.Errorf("Temporal workflow ID is required")
	}
	return o, nil
}

type historySource interface {
	GetWorkflowHistory(context.Context, string, string, bool, enums.HistoryEventFilterType) client.HistoryEventIterator
}

// Fetch reads authoritative workflow history through the Temporal Go SDK and
// translates it through Cutline's runtime-independent adapter boundary.
func Fetch(ctx context.Context, options LiveOptions) (ingest.Snapshot, error) {
	options, err := options.normalized()
	if err != nil {
		return ingest.Snapshot{}, err
	}
	api, err := client.NewClient(client.Options{HostPort: options.Address, Namespace: options.Namespace})
	if err != nil {
		return ingest.Snapshot{}, fmt.Errorf("connect to Temporal: %w", err)
	}
	defer api.Close()
	return FetchFrom(ctx, api, options)
}

// FetchFrom makes the live boundary testable with an SDK client connected to
// an ephemeral local server. It never starts, cancels, or otherwise mutates a
// workflow execution.
func FetchFrom(ctx context.Context, source historySource, options LiveOptions) (ingest.Snapshot, error) {
	options, err := options.normalized()
	if err != nil {
		return ingest.Snapshot{}, err
	}
	run, err := model.ContentID("run", options.WorkflowID, options.RunID)
	if err != nil {
		return ingest.Snapshot{}, err
	}
	attempt, err := model.ContentID("attempt", options.WorkflowID, options.RunID)
	if err != nil {
		return ingest.Snapshot{}, err
	}
	session, err := model.ContentID("session", options.Namespace, options.WorkflowID, options.RunID)
	if err != nil {
		return ingest.Snapshot{}, err
	}
	history := WorkflowHistory{RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), WorkflowID: options.WorkflowID}
	iterator := source.GetWorkflowHistory(ctx, options.WorkflowID, options.RunID, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iterator.HasNext() {
		event, err := iterator.Next()
		if err != nil {
			return ingest.Snapshot{}, fmt.Errorf("read Temporal workflow history: %w", err)
		}
		history.Events = append(history.Events, translateSDKEvent(event, options))
	}
	if len(history.Events) == 0 {
		return ingest.Snapshot{}, fmt.Errorf("Temporal workflow history is empty")
	}
	return NewAdapter().TranslateWorkflow(history)
}

func translateSDKEvent(event *historypb.HistoryEvent, options LiveOptions) HistoryEvent {
	attributes := map[string]string{
		"temporal.workflowId": options.WorkflowID,
		"temporal.runId":      options.RunID,
	}
	if event.GetEventTime() == nil {
		attributes["temporal.timestampMissing"] = "true"
	}
	raw := HistoryEvent{ID: strconv.FormatInt(event.GetEventId(), 10), Type: "temporal." + strings.ToLower(event.GetEventType().String()), Attributes: attributes}
	if event.GetEventTime() != nil {
		raw.ObservedAt = event.GetEventTime().AsTime()
	}
	switch event.GetEventType() {
	case enums.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED:
		raw.Type = "workflow.started"
	case enums.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED:
		raw.Type = "workflow.task_scheduled"
	case enums.EVENT_TYPE_WORKFLOW_TASK_STARTED:
		raw.Type = "workflow.task_started"
	case enums.EVENT_TYPE_WORKFLOW_TASK_COMPLETED:
		raw.Type = "workflow.task_completed"
	case enums.EVENT_TYPE_WORKFLOW_EXECUTION_CANCEL_REQUESTED:
		raw.Type = "workflow.cancel_requested"
	case enums.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED:
		raw.Type = "workflow.completed"
	case enums.EVENT_TYPE_WORKFLOW_EXECUTION_FAILED:
		raw.Type = "workflow.failed"
	case enums.EVENT_TYPE_WORKFLOW_EXECUTION_CANCELED:
		raw.Type = "workflow.canceled"
	case enums.EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED:
		raw.Type = "workflow.signaled"
	case enums.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED:
		raw.Type = "activity.scheduled"
		attributes["activityId"] = event.GetActivityTaskScheduledEventAttributes().GetActivityId()
		attributes["taskId"] = attributes["activityId"]
		attributes["scheduledEventId"] = raw.ID
	case enums.EVENT_TYPE_ACTIVITY_TASK_STARTED:
		raw.Type = "activity.started"
		setActivityCorrelation(attributes, event.GetActivityTaskStartedEventAttributes().GetScheduledEventId(), raw.ID)
	case enums.EVENT_TYPE_ACTIVITY_TASK_COMPLETED:
		raw.Type = "activity.completed"
		setActivityCorrelation(attributes, event.GetActivityTaskCompletedEventAttributes().GetScheduledEventId(), raw.ID)
	case enums.EVENT_TYPE_ACTIVITY_TASK_FAILED:
		raw.Type = "activity.failed"
		setActivityCorrelation(attributes, event.GetActivityTaskFailedEventAttributes().GetScheduledEventId(), raw.ID)
	case enums.EVENT_TYPE_ACTIVITY_TASK_CANCEL_REQUESTED:
		raw.Type = "activity.cancel_requested"
		setActivityCorrelation(attributes, event.GetActivityTaskCancelRequestedEventAttributes().GetScheduledEventId(), raw.ID)
	case enums.EVENT_TYPE_ACTIVITY_TASK_CANCELED:
		raw.Type = "activity.canceled"
		setActivityCorrelation(attributes, event.GetActivityTaskCanceledEventAttributes().GetScheduledEventId(), raw.ID)
	}
	return raw
}

func setActivityCorrelation(attributes map[string]string, scheduledEventID int64, historyEventID string) {
	attributes["temporal.historyEventId"] = historyEventID
	if scheduledEventID > 0 {
		attributes["scheduledEventId"] = strconv.FormatInt(scheduledEventID, 10)
		return
	}
	attributes["temporal.scheduledEventIdMissing"] = "true"
}
