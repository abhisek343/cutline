package model

import (
	"errors"
	"testing"
)

func TestRunTransitions(t *testing.T) {
	t.Parallel()

	valid := [][2]RunState{
		{RunCreated, RunValidating},
		{RunValidating, RunProvisioning},
		{RunProvisioning, RunDiscovering},
		{RunDiscovering, RunPlanned},
		{RunPlanned, RunExecuting},
		{RunExecuting, RunDraining},
		{RunDraining, RunFrozen},
		{RunFrozen, RunViolated},
		{RunViolated, RunMinimizing},
		{RunMinimizing, RunPackaged},
		{RunPackaged, RunCleaned},
		{RunExecuting, RunIncomplete},
	}
	for _, pair := range valid {
		if err := ValidateRunTransition(pair[0], pair[1]); err != nil {
			t.Errorf("%s -> %s error = %v", pair[0], pair[1], err)
		}
	}

	invalidTransitions := [][2]RunState{
		{RunCreated, RunPassed},
		{RunExecuting, RunPassed},
		{RunFrozen, RunIncomplete},
		{RunPassed, RunExecuting},
		{RunCleaned, RunExecuting},
	}
	for _, pair := range invalidTransitions {
		if err := ValidateRunTransition(pair[0], pair[1]); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%s -> %s error = %v, want ErrInvalidTransition", pair[0], pair[1], err)
		}
	}
}

func TestDomainTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		valid   func() error
		invalid func() error
	}{
		{"task", func() error { return ValidateTaskTransition(TaskStarted, TaskCompleted) }, func() error { return ValidateTaskTransition(TaskCompleted, TaskStarted) }},
		{"effect", func() error { return ValidateEffectTransition(EffectAttempted, EffectUnknown) }, func() error { return ValidateEffectTransition(EffectFailed, EffectCommitted) }},
		{"resource", func() error { return ValidateResourceTransition(ResourceAcquired, ResourceReleased) }, func() error { return ValidateResourceTransition(ResourceReleased, ResourceAcquired) }},
		{"checkpoint", func() error { return ValidateCheckpointTransition(CheckpointBlocked, CheckpointCancelled) }, func() error { return ValidateCheckpointTransition(CheckpointReleased, CheckpointBlocked) }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.valid(); err != nil {
				t.Fatalf("valid transition error = %v", err)
			}
			if err := tt.invalid(); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("invalid transition error = %v, want ErrInvalidTransition", err)
			}
		})
	}
}
