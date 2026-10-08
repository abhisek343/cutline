package cutline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/control"
	"github.com/abhisek343/cutline/internal/model"
)

type lifecycleHandler struct {
	mu            sync.Mutex
	events        []model.Event
	returned      chan struct{}
	childFinished chan struct{}
	cancelPoints  bool
	reject        model.EventType
}

func (h *lifecycleHandler) SessionStarted(control.Hello) error { return nil }

func (h *lifecycleHandler) EventAccepted(event model.Event) (control.Decision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
	if event.Type == model.EventTargetReturned {
		close(h.returned)
	}
	if event.Type == model.EventTaskFinished && event.ParentEntityID != "" {
		select {
		case <-h.childFinished:
		default:
			close(h.childFinished)
		}
	}
	if event.Type == h.reject && event.ParentEntityID != "" {
		return control.Decision{}, errors.New("interrupted child lifecycle evidence")
	}
	if event.Type == model.EventCheckpointReached {
		if h.cancelPoints {
			id, _ := model.ContentID("cancel", "child-race")
			return control.Decision{Action: control.ActionCancel, CancellationID: model.CancellationID(id)}, nil
		}
		return control.Decision{Action: control.ActionRelease}, nil
	}
	return control.Decision{}, nil
}

func (h *lifecycleHandler) snapshot() []model.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]model.Event(nil), h.events...)
}

func activeLifecycle(t *testing.T, timeout time.Duration) *lifecycleHandler {
	t.Helper()
	h := &lifecycleHandler{returned: make(chan struct{}), childFinished: make(chan struct{})}
	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	server, err := control.Listen(control.ServerConfig{
		Network: "tcp", Address: "127.0.0.1:0", Token: "test-secret",
		RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), Handler: h,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	network, endpoint := server.Endpoint()
	for key, value := range map[string]string{
		envNetwork: network, envEndpoint: endpoint, envToken: "test-secret",
		envRunID: run, envAttemptID: attempt, envSessionID: session,
		envDrainTimeout: timeout.String(),
	} {
		t.Setenv(key, value)
	}
	return h
}

func waitTask(t *testing.T, task *Task) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return task.Wait(ctx)
}

func assertDrain(t *testing.T, events []model.Event, want bool) {
	t.Helper()
	drain, ended := -1, -1
	for i, event := range events {
		switch event.Type {
		case model.EventDrainCompleted:
			drain = i
		case model.EventSessionEnded:
			ended = i
		case model.EventTaskFinished, model.EventEffectCommitted:
			if drain >= 0 {
				t.Fatal("terminal evidence arrived after successful drain")
			}
		}
	}
	if want && (drain < 0 || ended != len(events)-1 || ended <= drain) {
		t.Fatalf("successful drain/session end absent or out of order: drain=%d end=%d", drain, ended)
	}
	if !want && (drain >= 0 || ended >= 0) {
		t.Fatalf("incomplete run claimed completion: drain=%d end=%d", drain, ended)
	}
}

func TestRunDrainsNestedChildrenAndOwnsLateEvidence(t *testing.T) {
	h := activeLifecycle(t, time.Second)
	var child, grandchild *Task
	err := Run(context.Background(), "root", func(ctx context.Context) error {
		var err error
		child, err = Spawn(ctx, "child", func(childCtx context.Context) error {
			<-h.returned
			var spawnErr error
			grandchild, spawnErr = Spawn(childCtx, "grandchild", func(grandchildCtx context.Context) error {
				<-h.childFinished
				if err := Point(grandchildCtx, "late-effect"); err != nil {
					return err
				}
				resource, err := Acquire(grandchildCtx, ResourceSpec{Kind: "lock", Name: "late"})
				if err != nil {
					return err
				}
				if err := resource.Release(grandchildCtx); err != nil {
					return err
				}
				effect, err := BeginEffect(grandchildCtx, EffectSpec{Kind: "email.send", EvidenceSource: "fixture"})
				if err != nil {
					return err
				}
				if err := effect.Attempt(grandchildCtx); err != nil {
					return err
				}
				return effect.Commit(grandchildCtx)
			})
			return spawnErr
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitTask(t, child); err != nil {
		t.Fatal(err)
	}
	if err := waitTask(t, grandchild); err != nil {
		t.Fatal(err)
	}
	events := h.snapshot()
	assertDrain(t, events, true)
	ids := make(map[string]string)
	parents := make(map[string]string)
	returned, committed := -1, -1
	for i, event := range events {
		if event.Type == model.EventTaskRegistered {
			ids[event.Attributes["name"]] = event.EntityID
			parents[event.EntityID] = event.ParentEntityID
		}
		if event.Type == model.EventTargetReturned {
			returned = i
		}
		if event.Type == model.EventEffectCommitted {
			committed = i
		}
	}
	if len(ids) != 3 || parents[ids["child"]] != ids["root"] || parents[ids["grandchild"]] != ids["child"] {
		t.Fatalf("incorrect nested ancestry: ids=%v parents=%v", ids, parents)
	}
	if committed <= returned {
		t.Fatalf("effect was not observed after root return: return=%d commit=%d", returned, committed)
	}
	for _, event := range events {
		switch event.Type {
		case model.EventCheckpointReached, model.EventEffectDeclared, model.EventEffectAttempted,
			model.EventEffectCommitted, model.EventResourceAcquired, model.EventResourceReleased:
			if event.ParentEntityID != ids["grandchild"] {
				t.Fatalf("%s owner=%s, want grandchild", event.Type, event.ParentEntityID)
			}
		}
	}
}

func TestRunDrainTimeoutAndClosedScope(t *testing.T) {
	h := activeLifecycle(t, 25*time.Millisecond)
	release := make(chan struct{})
	var child *Task
	var savedCtx context.Context
	started := time.Now()
	err := Run(context.Background(), "root", func(ctx context.Context) error {
		savedCtx = ctx
		var err error
		child, err = Spawn(ctx, "never-ending", func(context.Context) error { <-release; return nil })
		return err
	})
	if !errors.Is(err, ErrDrainTimeout) {
		t.Fatalf("Run()=%v, want drain timeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("unbounded drain: %s", elapsed)
	}
	assertDrain(t, h.snapshot(), false)
	incomplete := false
	for _, event := range h.snapshot() {
		if event.Type == model.EventEvidenceIncomplete && event.Attributes["code"] == "drain_timeout" {
			incomplete = true
		}
	}
	if !incomplete {
		t.Fatal("drain timeout has no explicit incomplete evidence")
	}
	if _, err := Spawn(savedCtx, "too-late", func(context.Context) error { return nil }); !errors.Is(err, ErrTaskScopeClosed) {
		t.Fatalf("Spawn(after Run)=%v", err)
	}
	close(release)
	if err := waitTask(t, child); !errors.Is(err, ErrControlUnavailable) {
		t.Fatalf("late child terminal evidence=%v", err)
	}
}

func TestRunCancelledParentStillDrains(t *testing.T) {
	h := activeLifecycle(t, time.Second)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var child *Task
	err := Run(parent, "root", func(ctx context.Context) error {
		var err error
		child, err = Spawn(ctx, "child", func(childCtx context.Context) error {
			<-childCtx.Done()
			return context.Cause(childCtx)
		})
		cancel()
		return errors.Join(err, context.Cause(ctx))
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run()=%v", err)
	}
	if err := waitTask(t, child); !errors.Is(err, context.Canceled) {
		t.Fatalf("child=%v", err)
	}
	assertDrain(t, h.snapshot(), true)
	for _, event := range h.snapshot() {
		if event.Type == model.EventTaskFinished && event.Attributes["status"] != string(model.TaskCancelled) {
			t.Fatalf("cancelled task status=%s", event.Attributes["status"])
		}
	}
}

func TestRunChildCancellationSharesScopeAndDrains(t *testing.T) {
	h := activeLifecycle(t, time.Second)
	h.cancelPoints = true
	var tasks []*Task
	err := Run(context.Background(), "root", func(ctx context.Context) error {
		for i := range 16 {
			task, err := Spawn(ctx, fmt.Sprintf("child-%d", i), func(childCtx context.Context) error {
				return Point(childCtx, "cancel-child")
			})
			if err != nil {
				return err
			}
			tasks = append(tasks, task)
		}
		<-ctx.Done()
		return context.Cause(ctx)
	})
	if !errors.Is(err, ErrInjectedCancellation) {
		t.Fatalf("Run()=%v", err)
	}
	for _, task := range tasks {
		if err := waitTask(t, task); !errors.Is(err, ErrInjectedCancellation) {
			t.Fatalf("child=%v", err)
		}
	}
	assertDrain(t, h.snapshot(), true)
}

func TestRunChildEvidenceFailureNeverClaimsDrain(t *testing.T) {
	for _, rejected := range []model.EventType{model.EventTaskRegistered, model.EventTaskStarted, model.EventTaskFinished} {
		t.Run(string(rejected), func(t *testing.T) {
			h := activeLifecycle(t, time.Second)
			h.reject = rejected
			var child *Task
			err := Run(context.Background(), "root", func(ctx context.Context) error {
				child, _ = Spawn(ctx, "child", func(context.Context) error { return nil })
				return nil // The SDK must retain the evidence gap even if target ignores it.
			})
			if !errors.Is(err, ErrControlUnavailable) {
				t.Fatalf("Run()=%v", err)
			}
			if child != nil {
				_ = waitTask(t, child)
			}
			assertDrain(t, h.snapshot(), false)
		})
	}
}

func TestRunRejectsRootSpawnDuringChildDrain(t *testing.T) {
	h := activeLifecycle(t, time.Second)
	var child *Task
	err := Run(context.Background(), "root", func(ctx context.Context) error {
		var err error
		child, err = Spawn(ctx, "child", func(context.Context) error {
			<-h.returned
			_, err := Spawn(ctx, "orphan", func(context.Context) error { return nil })
			if !errors.Is(err, ErrTaskScopeClosed) {
				return fmt.Errorf("returned root admitted child: %v", err)
			}
			return nil
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitTask(t, child); err != nil {
		t.Fatal(err)
	}
	assertDrain(t, h.snapshot(), true)
}

func TestRunRejectsInvalidDrainTimeout(t *testing.T) {
	for _, value := range []string{"0", "-1s", "invalid"} {
		t.Run(value, func(t *testing.T) {
			activeLifecycle(t, time.Second)
			t.Setenv(envDrainTimeout, value)
			called := false
			err := Run(context.Background(), "root", func(context.Context) error { called = true; return nil })
			if !errors.Is(err, ErrControlUnavailable) || called {
				t.Fatalf("Run()=%v called=%t", err, called)
			}
		})
	}
}
