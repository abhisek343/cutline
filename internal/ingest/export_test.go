package ingest

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/model"
)

func TestExportJSONAndJSONLKeepIncompleteEvidenceVisible(t *testing.T) {
	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	eventID, _ := model.ContentID("task", t.Name())
	snapshot := Snapshot{
		RunID: model.RunID(run), AttemptID: model.AttemptID(attempt),
		Events:            []model.Event{{SchemaVersion: model.EventSchemaVersion, RunID: model.RunID(run), AttemptID: model.AttemptID(attempt), SessionID: model.SessionID(session), LocalSequence: 1, CanonicalOrder: 1, Type: model.EventTaskStarted, EntityID: eventID, ObservedAt: time.Now().UTC()}},
		IncompleteReasons: []string{"target exited before drain"},
	}
	var document bytes.Buffer
	if err := WriteJSON(&document, snapshot); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(document.String(), "target exited before drain") {
		t.Fatal("JSON omitted incompleteness")
	}
	var lines bytes.Buffer
	if err := WriteJSONL(&lines, snapshot); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(lines.String(), "\n"); got != 2 {
		t.Fatalf("JSONL lines = %d, want 2", got)
	}
}
