package ledger

import (
	"strings"
	"testing"
)

func TestMigrationInventory(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}
	migration := migrations[0]
	if migration.Version != "001_initial.sql" {
		t.Fatalf("version = %q", migration.Version)
	}
	if len(migration.Checksum) != 64 {
		t.Fatalf("checksum length = %d", len(migration.Checksum))
	}

	requiredTables := []string{
		"campaigns", "target_builds", "runs", "run_attempts", "sessions",
		"tasks", "checkpoint_visits", "cancellations", "effects",
		"effect_transitions", "resources", "resource_transitions", "events",
		"causal_edges", "schedule_actions", "contract_evaluations",
		"failure_signatures", "capsules", "artifacts",
	}
	for _, table := range requiredTables {
		if !strings.Contains(migration.SQL, "CREATE TABLE "+table) {
			t.Errorf("migration does not create %s", table)
		}
	}
}
