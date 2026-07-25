// Package cutline provides explicit instrumentation for cancellation-sensitive
// Go code. Instrumentation is inert when the target is not launched by Cutline.
package cutline

import "errors"

var (
	ErrInjectedCancellation = errors.New("cutline injected cancellation")
	ErrControlUnavailable   = errors.New("cutline control unavailable")
	ErrInvalidPoint         = errors.New("invalid cutline checkpoint")
	ErrInvalidEffect        = errors.New("invalid cutline effect")
	ErrInvalidResource      = errors.New("invalid cutline resource")
)
