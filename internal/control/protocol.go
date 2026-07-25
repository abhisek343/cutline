package control

import (
	"errors"

	"github.com/abhisek343/cutline/internal/model"
)

const ProtocolVersion = 1

var (
	ErrProtocol        = errors.New("control protocol error")
	ErrAuthentication  = errors.New("control authentication failed")
	ErrDisconnected    = errors.New("control channel disconnected")
	ErrSequence        = errors.New("control sequence mismatch")
	ErrPayloadTooLarge = errors.New("control payload too large")
)

type RequestKind string

const (
	RequestHello RequestKind = "hello"
	RequestEvent RequestKind = "event"
)

type Action string

const (
	ActionNone    Action = ""
	ActionRelease Action = "release"
	ActionCancel  Action = "cancel"
)

// Request is one target-to-coordinator RPC. Requests are strictly ordered
// within a logical session by Sequence.
type Request struct {
	ProtocolVersion int             `json:"protocolVersion"`
	RequestID       string          `json:"requestId"`
	Sequence        uint64          `json:"sequence"`
	Kind            RequestKind     `json:"kind"`
	Token           string          `json:"token,omitempty"`
	RunID           model.RunID     `json:"runId"`
	AttemptID       model.AttemptID `json:"attemptId"`
	SessionID       model.SessionID `json:"sessionId"`
	Event           *model.Event    `json:"event,omitempty"`
}

// Response acknowledges one request and may carry a checkpoint decision.
type Response struct {
	ProtocolVersion int                  `json:"protocolVersion"`
	ReplyTo         string               `json:"replyTo"`
	Action          Action               `json:"action,omitempty"`
	CancellationID  model.CancellationID `json:"cancellationId,omitempty"`
	Error           string               `json:"error,omitempty"`
}

// Decision is returned by the coordinator after an event is durably accepted.
type Decision struct {
	Action         Action
	CancellationID model.CancellationID
}

// Handler receives authenticated sessions and validated canonical events.
type Handler interface {
	SessionStarted(Hello) error
	EventAccepted(model.Event) (Decision, error)
}

// Hello identifies the target session after token authentication.
type Hello struct {
	RunID     model.RunID
	AttemptID model.AttemptID
	SessionID model.SessionID
}
