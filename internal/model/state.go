package model

import "fmt"

type (
	RunState        string
	TaskState       string
	EffectState     string
	ResourceState   string
	CheckpointState string
)

const (
	RunCreated      RunState = "created"
	RunValidating   RunState = "validating"
	RunProvisioning RunState = "provisioning"
	RunDiscovering  RunState = "discovering"
	RunPlanned      RunState = "planned"
	RunExecuting    RunState = "executing"
	RunDraining     RunState = "draining"
	RunFrozen       RunState = "frozen"
	RunPassed       RunState = "passed"
	RunViolated     RunState = "violated"
	RunInconclusive RunState = "inconclusive"
	RunInvalid      RunState = "invalid"
	RunIncomplete   RunState = "incomplete"
	RunMinimizing   RunState = "minimizing"
	RunPackaged     RunState = "packaged"
	RunCleaned      RunState = "cleaned"
)

const (
	TaskRegistered TaskState = "registered"
	TaskStarted    TaskState = "started"
	TaskCompleted  TaskState = "completed"
	TaskCancelled  TaskState = "cancelled"
	TaskFailed     TaskState = "failed"
	TaskLost       TaskState = "lost"
)

const (
	EffectDeclared    EffectState = "declared"
	EffectAttempted   EffectState = "attempted"
	EffectCommitted   EffectState = "committed"
	EffectFailed      EffectState = "failed"
	EffectUnknown     EffectState = "unknown"
	EffectCompensated EffectState = "compensated"
)

const (
	ResourceDeclared ResourceState = "declared"
	ResourceAcquired ResourceState = "acquired"
	ResourceReleased ResourceState = "released"
	ResourceExpired  ResourceState = "expired"
	ResourceLost     ResourceState = "lost"
)

const (
	CheckpointReached      CheckpointState = "reached"
	CheckpointBlocked      CheckpointState = "blocked"
	CheckpointReleased     CheckpointState = "released"
	CheckpointCancelled    CheckpointState = "cancelled"
	CheckpointDisconnected CheckpointState = "disconnected"
)

func ValidateRunTransition(from, to RunState) error {
	if allowedRunTransition(from, to) {
		return nil
	}
	return transitionError("run", from, to)
}

func allowedRunTransition(from, to RunState) bool {
	if to == RunIncomplete && from != RunFrozen && !isRunTerminal(from) {
		return true
	}
	switch from {
	case RunCreated:
		return to == RunValidating
	case RunValidating:
		return to == RunProvisioning || to == RunInvalid
	case RunProvisioning:
		return to == RunDiscovering || to == RunPlanned
	case RunDiscovering:
		return to == RunPlanned
	case RunPlanned:
		return to == RunExecuting
	case RunExecuting:
		return to == RunDraining
	case RunDraining:
		return to == RunFrozen
	case RunFrozen:
		return to == RunPassed || to == RunViolated || to == RunInconclusive
	case RunViolated:
		return to == RunMinimizing || to == RunCleaned
	case RunMinimizing:
		return to == RunPackaged
	case RunPackaged, RunPassed, RunInconclusive, RunInvalid, RunIncomplete:
		return to == RunCleaned
	default:
		return false
	}
}

func isRunTerminal(state RunState) bool {
	return state == RunCleaned
}

func ValidateTaskTransition(from, to TaskState) error {
	ok := (from == TaskRegistered && (to == TaskStarted || to == TaskCancelled || to == TaskLost)) ||
		(from == TaskStarted && (to == TaskCompleted || to == TaskCancelled || to == TaskFailed || to == TaskLost))
	if ok {
		return nil
	}
	return transitionError("task", from, to)
}

func ValidateEffectTransition(from, to EffectState) error {
	ok := (from == EffectDeclared && to == EffectAttempted) ||
		(from == EffectAttempted && (to == EffectCommitted || to == EffectFailed || to == EffectUnknown)) ||
		(from == EffectCommitted && to == EffectCompensated)
	if ok {
		return nil
	}
	return transitionError("effect", from, to)
}

func ValidateResourceTransition(from, to ResourceState) error {
	ok := (from == ResourceDeclared && to == ResourceAcquired) ||
		(from == ResourceAcquired && (to == ResourceReleased || to == ResourceExpired || to == ResourceLost))
	if ok {
		return nil
	}
	return transitionError("resource", from, to)
}

func ValidateCheckpointTransition(from, to CheckpointState) error {
	ok := (from == CheckpointReached && (to == CheckpointBlocked || to == CheckpointCancelled)) ||
		(from == CheckpointBlocked && (to == CheckpointReleased || to == CheckpointCancelled || to == CheckpointDisconnected))
	if ok {
		return nil
	}
	return transitionError("checkpoint", from, to)
}

func transitionError[T ~string](kind string, from, to T) error {
	return invalid(
		ErrInvalidTransition,
		kind,
		fmt.Sprintf("%q -> %q is not allowed", from, to),
	)
}
