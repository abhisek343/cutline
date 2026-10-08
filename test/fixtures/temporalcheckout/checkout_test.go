package temporalcheckout

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/pkg/cutline"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalsdk "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// Business identity stays constant across executions; Temporal execution IDs
// are deliberately fresh and cannot identify the failure being replayed.
const fixtureOrderID = "temporal-checkout-order-42"

func TestCheckout(t *testing.T) {
	address := os.Getenv("CUTLINE_TEMPORAL_ADDRESS")
	if address == "" {
		t.Skip("Temporal campaign environment is not configured")
	}
	namespace := os.Getenv("CUTLINE_TEMPORAL_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}
	workflowID := os.Getenv("CUTLINE_RUN_ID")
	if workflowID == "" {
		t.Fatal("CUTLINE_RUN_ID is required")
	}
	queue := "cutline-temporal-" + workflowID
	api, err := client.NewClient(client.Options{HostPort: address, Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	w := worker.New(api, queue, worker.Options{MaxHeartbeatThrottleInterval: 100 * time.Millisecond, DefaultHeartbeatThrottleInterval: 100 * time.Millisecond})
	w.RegisterWorkflow(checkoutWorkflow)
	w.RegisterActivity(checkoutActivity)
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	run, err := api.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{ID: workflowID, TaskQueue: queue}, checkoutWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	err = run.Get(context.Background(), nil)
	if os.Getenv("CUTLINE_DISCOVERY") == "1" {
		if err != nil {
			t.Fatalf("discovery workflow failed: %v", err)
		}
		return
	}
	if !temporalsdk.IsCanceledError(err) {
		t.Fatalf("scheduled cancellation did not cancel workflow: %v", err)
	}
	// The campaign uses Cutline's checkpoint protocol, but it must also cause
	// an actual service cancellation and wait for the activity acknowledgement.
	requested, activityCanceled := false, false
	var requestedAt time.Time
	iter := api.GetWorkflowHistory(context.Background(), workflowID, run.GetRunID(), false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iter.HasNext() {
		event, err := iter.Next()
		if err != nil {
			t.Fatal(err)
		}
		requested = requested || event.GetEventType() == enums.EVENT_TYPE_WORKFLOW_EXECUTION_CANCEL_REQUESTED
		if event.GetEventType() == enums.EVENT_TYPE_WORKFLOW_EXECUTION_CANCEL_REQUESTED {
			requestedAt = event.GetEventTime().AsTime()
		}
		activityCanceled = activityCanceled || event.GetEventType() == enums.EVENT_TYPE_ACTIVITY_TASK_CANCELED
	}
	if !requested || !activityCanceled {
		t.Fatalf("missing Temporal cancellation request/acknowledgement: requested=%t activityCanceled=%t", requested, activityCanceled)
	}
	records, err := fixtureledger.Read(os.Getenv("CUTLINE_FIXTURE_LEDGER"), 2)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("CUTLINE_TEMPORAL_FIXTURE") == "faulty" {
		if len(records) != 1 || !records[0].CommittedAt.After(requestedAt) {
			t.Fatalf("faulty charge did not cross actual Temporal request: requestedAt=%v records=%#v", requestedAt, records)
		}
	} else if len(records) != 0 {
		t.Fatalf("clean fixture committed an effect: %#v", records)
	}
}

func TestAwaitTemporalCancellationUsesActivityContext(t *testing.T) {
	original, cancel := context.WithCancel(context.Background())
	defer cancel()
	injected, inject := context.WithCancelCause(original)
	inject(cutline.ErrInjectedCancellation)
	beats := 0
	err := awaitTemporalCancellation(original, func() {
		beats++
		if context.Cause(injected) == nil {
			t.Fatal("injected context must already be canceled")
		}
		if beats == 2 {
			cancel()
		}
	})
	if err != nil || beats != 2 {
		t.Fatalf("original cancellation was not awaited: beats=%d err=%v", beats, err)
	}
}

func TestAwaitTemporalCancellationDoesNotTreatDeadlineAsCancellation(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := awaitTemporalCancellation(ctx, func() {}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline was treated as explicit cancellation: %v", err)
	}
}

func checkoutWorkflow(ctx workflow.Context) error {
	options := workflow.ActivityOptions{StartToCloseTimeout: 20 * time.Second, HeartbeatTimeout: 2 * time.Second, WaitForCancellation: true, RetryPolicy: &temporalsdk.RetryPolicy{MaximumAttempts: 1}}
	ctx = workflow.WithActivityOptions(ctx, options)
	future := workflow.ExecuteActivity(ctx, checkoutActivity)
	if err := future.Get(ctx, nil); err != nil {
		if ctx.Err() != nil {
			// Root cancellation unblocks Get before the activity has drained.
			// A disconnected wait preserves the cancellation acknowledgement.
			drain, _ := workflow.NewDisconnectedContext(ctx)
			_ = future.Get(drain, nil)
			return ctx.Err()
		}
		return err
	}
	return nil
}

func checkoutActivity(ctx context.Context) error {
	activityCtx := ctx
	return cutline.Run(ctx, "temporal-checkout", func(ctx context.Context) error {
		pointErr := cutline.Point(ctx, "before-charge")
		if pointErr == nil {
			return nil
		}

		info := activity.GetInfo(ctx)
		if !errors.Is(pointErr, cutline.ErrInjectedCancellation) {
			return pointErr
		}
		api, err := client.NewClient(client.Options{HostPort: os.Getenv("CUTLINE_TEMPORAL_ADDRESS"), Namespace: os.Getenv("CUTLINE_TEMPORAL_NAMESPACE")})
		if err != nil {
			return err
		}
		defer api.Close()
		requestCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := api.CancelWorkflow(requestCtx, info.WorkflowExecution.ID, info.WorkflowExecution.RunID); err != nil {
			return err
		}
		// Observe the original Temporal activity context, not the child context
		// canceled by Cutline.Point. Heartbeats deliver service cancellation.
		if err := awaitTemporalCancellation(activityCtx, func() { activity.RecordHeartbeat(activityCtx, "waiting-for-cancel") }); err != nil {
			return err
		}
		if os.Getenv("CUTLINE_TEMPORAL_FIXTURE") == "faulty" {
			effect, effectErr := cutline.BeginEffect(context.WithoutCancel(ctx), cutline.EffectSpec{Kind: "payment.charge", IdempotencyKey: fixtureOrderID, EvidenceSource: "fixture-ledger"})
			if effectErr != nil {
				return effectErr
			}
			if effectErr = effect.Attempt(context.WithoutCancel(ctx)); effectErr != nil {
				return effectErr
			}
			if effectErr = effect.Commit(context.WithoutCancel(ctx)); effectErr != nil {
				return effectErr
			}
			if err := fixtureledger.Append(os.Getenv("CUTLINE_FIXTURE_LEDGER"), fixtureledger.Record{EffectID: effect.ID(), Kind: "payment.charge", IdempotencyKey: fixtureOrderID, Source: "fixture-ledger", CommittedAt: time.Now().UTC()}); err != nil {
				return err
			}
		}
		return temporalsdk.NewCanceledError()
	})
}

func awaitTemporalCancellation(ctx context.Context, heartbeat func()) error {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		heartbeat()
		select {
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
			return nil
		case <-deadline.C:
			return errors.New("Temporal activity did not observe workflow cancellation")
		case <-ticker.C:
		}
	}
}
