//go:build temporal_integration

package temporal

import (
	"context"
	"testing"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func TestLiveFetchReadsCanceledWorkflowFromTemporalServer(t *testing.T) {
	const namespace, taskQueue, workflowID = "default", "cutline-live", "cutline-live-cancel"
	// Temporal auto-setup applies its PostgreSQL schema before the frontend accepts gRPC.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
