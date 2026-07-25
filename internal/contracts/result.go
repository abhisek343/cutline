package contracts

type Status string

const (
	StatusPass         Status = "pass"
	StatusViolation    Status = "violation"
	StatusInconclusive Status = "inconclusive"
	StatusInvalid      Status = "invalid"
)

type Result struct {
	Contract          string `json:"contract"`
	Status            Status `json:"status"`
	Message           string `json:"message"`
	OffendingEffectID string `json:"offendingEffectId,omitempty"`
	BoundaryOrder     uint64 `json:"boundaryOrder,omitempty"`
	EffectOrder       uint64 `json:"effectOrder,omitempty"`
}
