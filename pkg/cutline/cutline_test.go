package cutline

import (
	"context"
	"errors"
	"testing"
)

func TestInactivePointHonorsContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Point(ctx, "before-charge"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Point() error = %v", err)
	}
	if err := Point(context.Background(), "bad point!"); !errors.Is(err, ErrInvalidPoint) {
		t.Fatalf("Point(invalid) error = %v", err)
	}
}

func TestInactiveEffectStateMachine(t *testing.T) {
	t.Parallel()

	effect, err := BeginEffect(context.Background(), EffectSpec{
		Kind: "email.send", EvidenceSource: "fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if effect.ID() == "" {
		t.Fatal("inactive effect has no identity")
	}
	if err := effect.Commit(context.Background()); !errors.Is(err, ErrInvalidEffect) {
		t.Fatalf("Commit(before attempt) error = %v", err)
	}
	if err := effect.Attempt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := effect.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := effect.Fail(context.Background(), errors.New("late")); !errors.Is(err, ErrInvalidEffect) {
		t.Fatalf("Fail(after commit) error = %v", err)
	}
}

func TestInactiveSpawnAndResource(t *testing.T) {
	t.Parallel()

	task, err := Spawn(context.Background(), "child", func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	resource, err := Acquire(context.Background(), ResourceSpec{Kind: "lock", Name: "order"})
	if err != nil {
		t.Fatal(err)
	}
	if err := resource.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := resource.Release(context.Background()); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("second Release() error = %v", err)
	}
}
