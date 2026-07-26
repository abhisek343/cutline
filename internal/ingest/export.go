package ingest

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/abhisek343/cutline/internal/model"
)

// ExportRecord is a stable JSONL envelope. Keeping incomplete reasons as
// records makes a partial run impossible to mistake for an empty run.
type ExportRecord struct {
	Kind             string       `json:"kind"`
	Event            *model.Event `json:"event,omitempty"`
	IncompleteReason string       `json:"incompleteReason,omitempty"`
}

func WriteJSON(w io.Writer, snapshot Snapshot) error {
	if w == nil {
		return fmt.Errorf("write evidence JSON: nil writer")
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(snapshot); err != nil {
		return fmt.Errorf("encode evidence JSON: %w", err)
	}
	return nil
}

func WriteJSONL(w io.Writer, snapshot Snapshot) error {
	if w == nil {
		return fmt.Errorf("write evidence JSONL: nil writer")
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for i := range snapshot.Events {
		event := snapshot.Events[i]
		if err := encoder.Encode(ExportRecord{Kind: "event", Event: &event}); err != nil {
			return fmt.Errorf("encode event %d: %w", i+1, err)
		}
	}
	for _, reason := range snapshot.IncompleteReasons {
		if err := encoder.Encode(ExportRecord{Kind: "incomplete", IncompleteReason: reason}); err != nil {
			return fmt.Errorf("encode incomplete reason: %w", err)
		}
	}
	return nil
}
