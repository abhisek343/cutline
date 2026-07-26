package temporalcheckout

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/pkg/cutline"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

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
	w := worker.New(api, queue, worker.Options{})
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
	if err == nil {
		t.Fatal("scheduled cancellation did not cancel workflow")
	}
}

func checkoutWorkflow(ctx workflow.Context) error {
	options := workflow.ActivityOptions{StartToCloseTimeout: 20 * time.Second, WaitForCancellation: true}
	ctx = workflow.WithActivityOptions(ctx, options)
	future := workflow.ExecuteActivity(ctx, checkoutActivity)
	if err := future.Get(ctx, nil); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func checkoutActivity(ctx context.Context) error {
	return cutline.Run(ctx, "temporal-checkout", func(ctx context.Context) error {
		pointErr := cutline.Point(ctx, "before-charge")
		if pointErr == nil {
			return nil
		}

		info := activity.GetInfo(ctx)
		if !errors.Is(pointErr, cutline.ErrInjectedCancellation) {
			return pointErr
		}
		if os.Getenv("CUTLINE_TEMPORAL_FIXTURE") == "faulty" {
			effect, effectErr := cutline.BeginEffect(context.WithoutCancel(ctx), cutline.EffectSpec{Kind: "payment.charge", IdempotencyKey: activity.GetInfo(ctx).WorkflowExecution.ID, EvidenceSource: "fixture-ledger"})
			if effectErr != nil {
				return effectErr
			}
			if effectErr = effect.Attempt(context.WithoutCancel(ctx)); effectErr != nil {
				return effectErr
			}
			if effectErr = effect.Commit(context.WithoutCancel(ctx)); effectErr != nil {
				return effectErr
			}
			if err := fixtureledger.Append(os.Getenv("CUTLINE_FIXTURE_LEDGER"), fixtureledger.Record{EffectID: effect.ID(), Kind: "payment.charge", IdempotencyKey: info.WorkflowExecution.ID, Source: "temporal-fixture", CommittedAt: time.Now().UTC()}); err != nil {
				return err
			}
		}
		api, err := client.NewClient(client.Options{HostPort: os.Getenv("CUTLINE_TEMPORAL_ADDRESS"), Namespace: os.Getenv("CUTLINE_TEMPORAL_NAMESPACE")})
		if err != nil {
			return pointErr
		}
		defer api.Close()
		if err := api.CancelWorkflow(context.Background(), info.WorkflowExecution.ID, info.WorkflowExecution.RunID); err != nil {
			return pointErr
		}
		return pointErr
	})
}
