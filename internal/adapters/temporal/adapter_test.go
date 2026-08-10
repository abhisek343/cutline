package temporal

import (
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/model"
	"go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestWorkflowAdapterTranslatesCancellationLifecycle(t *testing.T) {
	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	base := time.Unix(1, 0).UTC()
	history := WorkflowHistory{RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), WorkflowID: "checkout", Events: []HistoryEvent{
		{ID: "1", Type: "workflow.started", ObservedAt: base},
		{ID: "2", Type: "workflow.checkpoint_reached", ObservedAt: base.Add(time.Second), Attributes: map[string]string{"point": "before-charge"}},
		{ID: "3", Type: "workflow.cancel_requested", ObservedAt: base.Add(2 * time.Second), Attributes: map[string]string{"cancellationId": "workflow-cancel"}},
		{ID: "4", Type: "workflow.cancel_delivered", ObservedAt: base.Add(3 * time.Second), Attributes: map[string]string{"cancellationId": "workflow-cancel"}},
		{ID: "5", Type: "workflow.cancel_observed", ObservedAt: base.Add(4 * time.Second), Attributes: map[string]string{"cancellationId": "workflow-cancel"}},
		{ID: "6", Type: "workflow.completed", ObservedAt: base.Add(5 * time.Second)},
	}}
	snapshot, err := NewAdapter().TranslateWorkflow(history)
	if err != nil || !snapshot.Complete() {
		t.Fatalf("snapshot=%#v error=%v", snapshot, err)
	}
	if len(snapshot.Events) != 6 || snapshot.Events[2].Type != model.EventCancelRequested || snapshot.Events[4].Type != model.EventCancelObserved {
		t.Fatalf("events=%#v", snapshot.Events)
	}
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot, AuthoritativeEffectsComplete: true})
	if !view.HasCapability(evidence.CapabilityCancellationObserved) {
		t.Fatalf("capabilities=%v", view.Capabilities)
	}
}

func TestActivityUnknownEffectIsNotGuessed(t *testing.T) {
	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	base := time.Unix(1, 0).UTC()
	snapshot, err := NewAdapter().TranslateActivity(ActivityHistory{RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), ActivityID: "charge", Events: []HistoryEvent{
		{ID: "1", Type: "activity.scheduled", ObservedAt: base},
		{ID: "2", Type: "activity.started", ObservedAt: base.Add(time.Second)},
		{ID: "3", Type: "activity.effect_declared", ObservedAt: base.Add(2 * time.Second), Attributes: map[string]string{"effectId": "charge-1", "kind": "payment.charge"}},
		{ID: "4", Type: "activity.effect_attempted", ObservedAt: base.Add(3 * time.Second), Attributes: map[string]string{"effectId": "charge-1", "kind": "payment.charge"}},
		{ID: "5", Type: "activity.effect_unknown", ObservedAt: base.Add(4 * time.Second), Attributes: map[string]string{"effectId": "charge-1", "kind": "payment.charge"}},
	}})
	if err != nil || !snapshot.Complete() {
		t.Fatalf("snapshot=%#v error=%v", snapshot, err)
	}
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot, AuthoritativeEffectsComplete: false})
	if len(view.Effects) != 1 || view.Effects[0].State != model.EffectUnknown {
		t.Fatalf("effects=%#v", view.Effects)
	}
}

func TestTemporalAdapterReportsUnsupportedHistory(t *testing.T) {
	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	snapshot, err := NewAdapter().TranslateWorkflow(WorkflowHistory{RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), WorkflowID: "w", Events: []HistoryEvent{{ID: "1", Type: "future.event", ObservedAt: time.Unix(1, 0).UTC()}}})
	if err != nil || snapshot.Complete() {
		t.Fatalf("snapshot=%#v error=%v", snapshot, err)
	}
}

func TestActivityEffectHistoryRequiresKnownOutcome(t *testing.T) {
	value, err := EffectHistory(ActivityEffect{EffectID: "charge-1", TaskID: "charge", Kind: "payment.charge", Outcome: "unknown", ObservedAt: time.Unix(1, 0).UTC()})
	if err != nil || value.Type != "activity.effect_unknown" || value.Attributes["effectId"] != "charge-1" {
		t.Fatalf("history=%#v error=%v", value, err)
	}
	if _, err := EffectHistory(ActivityEffect{EffectID: "charge-1", Kind: "payment.charge", Outcome: "maybe", ObservedAt: time.Unix(1, 0).UTC()}); err == nil {
		t.Fatal("EffectHistory accepted unknown outcome")
	}
	compensated, err := EffectHistory(ActivityEffect{EffectID: "charge-1", TaskID: "charge", Kind: "payment.charge", Outcome: "compensated", ObservedAt: time.Unix(1, 0).UTC()})
	if err != nil || compensated.Type != "activity.effect_compensated" {
		t.Fatalf("compensated history=%#v error=%v", compensated, err)
	}
}

func TestLiveOptionsRejectRemoteEndpoint(t *testing.T) {
	_, err := (LiveOptions{Address: "temporal.example:7233", WorkflowID: "checkout"}).normalized()
	if err == nil {
		t.Fatal("remote Temporal endpoint was accepted")
	}
	options, err := (LiveOptions{Address: "localhost:7233", WorkflowID: "checkout"}).normalized()
	if err != nil || options.Namespace != "default" {
		t.Fatalf("options=%#v error=%v", options, err)
	}
}

func TestSDKHistoryTranslationPreservesKnownAndUnknownFacts(t *testing.T) {
	base := time.Unix(1, 0).UTC()
	known := translateSDKEvent(&historypb.HistoryEvent{EventId: 7, EventType: enums.EVENT_TYPE_WORKFLOW_EXECUTION_CANCEL_REQUESTED, EventTime: timestamppb.New(base)}, LiveOptions{WorkflowID: "checkout", RunID: "temporal-run"})
	if known.ID != "7" || known.Type != "workflow.cancel_requested" || !known.ObservedAt.Equal(base) {
		t.Fatalf("known event=%#v", known)
	}
	unknown := translateSDKEvent(&historypb.HistoryEvent{EventId: 8, EventType: enums.EVENT_TYPE_WORKFLOW_EXECUTION_TERMINATED, EventTime: timestamppb.New(base)}, LiveOptions{WorkflowID: "checkout"})
	if unknown.Type != "temporal.workflowexecutionterminated" {
		t.Fatalf("unknown event=%#v", unknown)
	}
}
