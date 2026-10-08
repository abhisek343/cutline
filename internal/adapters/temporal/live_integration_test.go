//go:build temporal_integration

package temporal

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/model"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalsdk "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func TestLiveFetchReadsCanceledWorkflowFromTemporalServer(t *testing.T) {
	const namespace, taskQueue, workflowID = "default", "cutline-live", "cutline-live-cancel"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	api, err := newLiveClient(ctx, namespace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	w, err := startLiveWorker(ctx, api, taskQueue)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)

	run, err := startLiveWorkflow(ctx, api, workflowID, taskQueue)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.CancelWorkflow(ctx, workflowID, run.GetRunID()); err != nil {
		t.Fatal(err)
	}
	if err := run.Get(ctx, nil); err == nil {
		t.Fatal("canceled workflow completed successfully")
	}
	snapshot, err := Fetch(ctx, LiveOptions{Address: "127.0.0.1:7233", Namespace: namespace, WorkflowID: workflowID, RunID: run.GetRunID()})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Complete() {
		t.Fatalf("live history was incomplete: %#v", snapshot.IncompleteReasons)
	}
	seenCancel, seenReturn := false, false
	for _, event := range snapshot.Events {
		seenCancel = seenCancel || string(event.Type) == "cancel.requested"
		seenReturn = seenReturn || string(event.Type) == "target.returned"
	}
	if !seenCancel || !seenReturn {
		t.Fatalf("live history lacks cancellation evidence: %#v", snapshot.Events)
	}
}

func liveCancellationWorkflow(ctx workflow.Context) error {
	return workflow.Await(ctx, func() bool { return false })
}

func TestLiveFetchCorrelatesRetriedActivitiesFromTemporalServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	api, err := newLiveClient(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	w, err := startLiveWorker(ctx, api, "cutline-live-activities")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	run, err := api.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: "cutline-live-activities", TaskQueue: "cutline-live-activities"}, liveActivitiesWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Get(ctx, nil); err != nil {
		t.Fatal(err)
	}
	snapshot, err := FetchFrom(ctx, api, LiveOptions{WorkflowID: run.GetID(), RunID: run.GetRunID()})
	if err != nil || !snapshot.Complete() {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot})
	if len(view.Tasks) != 2 {
		t.Fatalf("tasks=%#v", view.Tasks)
	}
	for _, task := range view.Tasks {
		if task.State != model.TaskCompleted {
			t.Fatalf("activity lifecycle not correlated: %#v", task)
		}
	}
	starts := 0
	for _, event := range snapshot.Events {
		if event.Type == model.EventTaskStarted {
			starts++
			if event.Attributes["temporal.attempt"] != "3" {
				t.Fatalf("retry attempt not preserved: %#v", event)
			}
		}
	}
	if starts != 2 {
		t.Fatalf("recorded %d activity starts, want 2", starts)
	}
}

func liveActivitiesWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 5 * time.Second, RetryPolicy: &temporalsdk.RetryPolicy{InitialInterval: 10 * time.Millisecond, MaximumAttempts: 3}})
	first := workflow.ExecuteActivity(ctx, liveRetriedActivity)
	second := workflow.ExecuteActivity(ctx, liveRetriedActivity)
	if err := first.Get(ctx, nil); err != nil {
		return err
	}
	return second.Get(ctx, nil)
}

func liveRetriedActivity(ctx context.Context) error {
	if activity.GetInfo(ctx).Attempt < 3 {
		return fmt.Errorf("retry fixture attempt %d", activity.GetInfo(ctx).Attempt)
	}
	return nil
}

func startLiveWorkflow(ctx context.Context, api client.Client, workflowID, taskQueue string) (client.WorkflowRun, error) {
	for {
		run, err := api.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: workflowID, TaskQueue: taskQueue}, liveCancellationWorkflow)
		if err == nil {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func startLiveWorker(ctx context.Context, api client.Client, taskQueue string) (worker.Worker, error) {
	for {
		w := worker.New(api, taskQueue, worker.Options{})
		w.RegisterWorkflow(liveCancellationWorkflow)
		w.RegisterWorkflow(liveActivitiesWorkflow)
		w.RegisterActivity(liveRetriedActivity)
		if err := w.Start(); err == nil {
			return w, nil
		}
		w.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func newLiveClient(ctx context.Context, namespace string) (client.Client, error) {
	for {
		api, err := client.NewClient(client.Options{HostPort: "127.0.0.1:7233", Namespace: namespace})
		if err == nil {
			return api, nil
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(250 * time.Millisecond):
		}
	}
}
