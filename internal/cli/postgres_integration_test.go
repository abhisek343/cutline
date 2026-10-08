//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/adapters/native"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Run the shipped executable against the same fixture users run. Inspect the
// database independently, rather than trusting a successful in-memory result.
func TestPostgresCLIRealCampaignPersistence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	dsn := os.Getenv("CUTLINE_TEST_POSTGRES_DSN")
	if dsn == "" {
		container, err := tcpostgres.Run(ctx, "postgres:17.5-alpine", tcpostgres.WithDatabase("cutline"), tcpostgres.WithUsername("cutline"), tcpostgres.WithPassword("cutline-test"), tcpostgres.BasicWaitStrategies())
		if err != nil {
			t.Fatal(err)
		}
		testcontainers.CleanupContainer(t, container)
		dsn, err = container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := validateLocalPostgresDSN(dsn); err != nil {
		t.Fatalf("integration PostgreSQL must be local: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "cutline")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/cutline")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, fixture := range []struct {
		mode   string
		code   int
		status native.OverallStatus
	}{
		{"faulty", 2, native.OverallViolation}, {"clean", 0, native.OverallPass},
	} {
		t.Run(fixture.mode, func(t *testing.T) {
			command := exec.CommandContext(ctx, binary, "run", "--campaign", "test/fixtures/checkout/campaign-"+fixture.mode+".yaml", "--json")
			command.Dir = root
			command.Env = postgresCLIEnvironment(dsn)
			output, err := command.Output()
			code := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else {
					t.Fatalf("run CLI: %v", err)
				}
			}
			if code != fixture.code {
				t.Fatalf("CLI exit=%d want=%d error=%v output=%s", code, fixture.code, err, output)
			}
			var result native.CampaignResult
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatalf("decode CLI evidence: %v\n%s", err, output)
			}
			if result.Status != fixture.status || len(result.Schedules) != 1 {
				t.Fatalf("campaign result=%s schedules=%d", result.Status, len(result.Schedules))
			}
			runs := append([]native.Result{result.Discovery}, result.Schedules...)
			for _, run := range runs {
				if len(run.Evidence.Events) == 0 || !run.Evidence.Complete() {
					t.Fatalf("missing or incomplete CLI evidence: %#v", run.Evidence)
				}
				var state, adapter string
				var count, next int
				var frozen bool
				if err := pool.QueryRow(ctx, `SELECT a.state,a.frozen_at IS NOT NULL,a.next_canonical_order,t.adapter_version FROM run_attempts a JOIN runs r USING(run_id) JOIN target_builds t ON t.digest=r.target_digest WHERE a.run_id=$1 AND a.attempt_id=$2`, run.RunID, run.AttemptID).Scan(&state, &frozen, &next, &adapter); err != nil {
					t.Fatal(err)
				}
				if state != "frozen" || !frozen || next != len(run.Evidence.Events)+1 || adapter != "native/go-test/1" {
					t.Fatalf("persisted attempt state=%s frozen=%t next=%d adapter=%s", state, frozen, next, adapter)
				}
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE run_id=$1 AND attempt_id=$2`, run.RunID, run.AttemptID).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != len(run.Evidence.Events) {
					t.Fatalf("persisted events=%d CLI=%d", count, len(run.Evidence.Events))
				}
				for _, event := range run.Evidence.Events {
					attrs, err := json.Marshal(event.Attributes)
					if err != nil {
						t.Fatal(err)
					}
					if event.Attributes == nil {
						attrs = []byte(`{}`)
					}
					var matches int
					if err := pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE run_id=$1 AND attempt_id=$2 AND canonical_order=$3 AND session_id=$4 AND local_sequence=$5 AND event_type=$6 AND entity_id=$7 AND attributes=$8::jsonb`, run.RunID, run.AttemptID, event.CanonicalOrder, event.SessionID, event.LocalSequence, event.Type, event.EntityID, attrs).Scan(&matches); err != nil {
						t.Fatal(err)
					}
					if matches != 1 {
						t.Fatalf("event %d differs between CLI snapshot and PostgreSQL", event.CanonicalOrder)
					}
				}
				var tasks, edges, effects, authoritative int
				if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tasks WHERE run_id=$1 AND attempt_id=$2),(SELECT count(*) FROM causal_edges WHERE run_id=$1 AND attempt_id=$2),(SELECT count(*) FROM effects WHERE run_id=$1 AND attempt_id=$2 AND current_state='committed'),(SELECT count(*) FROM authoritative_effects WHERE run_id=$1 AND attempt_id=$2 AND outcome='committed')`, run.RunID, run.AttemptID).Scan(&tasks, &edges, &effects, &authoritative); err != nil {
					t.Fatal(err)
				}
				if tasks == 0 || edges == 0 || effects != len(run.Effects) || authoritative != len(run.Effects) {
					t.Fatalf("projections tasks=%d edges=%d committed=%d authoritative=%d CLI effects=%d", tasks, edges, effects, authoritative, len(run.Effects))
				}
			}
		})
	}
	t.Run("unavailable configured database cannot fall back to memory", func(t *testing.T) {
		// An absent role fails even when a configured endpoint is healthy.
		config, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		// Use the already validated local connection parameters.
		badDSN := "host='" + strings.ReplaceAll(config.ConnConfig.Host, "'", "\\'") + "' port=" + fmt.Sprint(config.ConnConfig.Port) + " user='cutline_missing_user' password='invalid' dbname='" + strings.ReplaceAll(config.ConnConfig.Database, "'", "\\'") + "' sslmode=disable connect_timeout=2"
		command := exec.CommandContext(ctx, binary, "run", "--campaign", "test/fixtures/checkout/campaign-clean.yaml", "--json")
		command.Dir = root
		command.Env = postgresCLIEnvironment(badDSN)
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "create evidence store") || strings.Contains(string(output), `"status":"pass"`) {
			t.Fatalf("PostgreSQL failure did not fail closed: err=%v output=%s", err, output)
		}
	})
}

func postgresCLIEnvironment(dsn string) []string {
	result := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, postgresDSNEnvironment+"=") {
			result = append(result, entry)
		}
	}
	return append(result, postgresDSNEnvironment+"="+dsn)
}
