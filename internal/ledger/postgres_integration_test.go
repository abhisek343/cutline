//go:build integration

package ledger

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPostgresSchemaConstraintsAndFreeze(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		"postgres:17.5-alpine",
		tcpostgres.WithDatabase("cutline"),
		tcpostgres.WithUsername("cutline"),
		tcpostgres.WithPassword("cutline-test"),
		tcpostgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("second migration application: %v", err)
	}

	const (
		digest   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		target   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		run      = "run_11111111111111111111111111111111"
		attempt  = "attempt_22222222222222222222222222222222"
		session  = "session_33333333333333333333333333333333"
		task     = "task_44444444444444444444444444444444"
		effect   = "effect_55555555555555555555555555555555"
		observed = "2026-07-25T00:00:00Z"
		eventSQL = `INSERT INTO events (
			run_id, attempt_id, canonical_order, session_id, local_sequence,
			schema_version, event_type, entity_id, observed_at
		) VALUES ($1, $2, $3, $4, $5, 1, $6, $7, $8)`
	)
	statements := []string{
		`INSERT INTO campaigns (digest, api_version, name, specification)
		 VALUES ('` + digest + `', 'cutline.dev/v1alpha1', 'test', '{}')`,
		`INSERT INTO target_builds (digest, adapter, adapter_version)
		 VALUES ('` + target + `', 'go-test', '1')`,
		`INSERT INTO runs (run_id, campaign_digest, target_digest, seed)
		 VALUES ('` + run + `', '` + digest + `', '` + target + `', 1)`,
		`INSERT INTO run_attempts (run_id, attempt_id)
		 VALUES ('` + run + `', '` + attempt + `')`,
		`INSERT INTO tasks (
			run_id, attempt_id, task_id, kind, name, state, registered_order
		 ) VALUES (
			'` + run + `', '` + attempt + `', '` + task + `',
			'native.root', 'test', 'started', 1
		 )`,
		`INSERT INTO effects (
			run_id, attempt_id, effect_id, owner_task_id, kind,
			idempotency_key, evidence_source, declared_order
		 ) VALUES (
			'` + run + `', '` + attempt + `', '` + effect + `', '` + task + `',
			'payment.charge', 'order-1', 'fixture', 2
		 )`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed schema: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, eventSQL, run, attempt, 1, session, 1, "session.started", session, observed); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, eventSQL, run, attempt, 1, session, 2, "task.started", task, observed); err == nil {
		t.Fatal("duplicate canonical order was accepted")
	}
	if _, err := pool.Exec(ctx, eventSQL, run, attempt, 2, session, 1, "task.started", task, observed); err == nil {
		t.Fatal("duplicate local sequence was accepted")
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO effect_transitions (
			run_id, attempt_id, effect_id, event_order, from_state, to_state
		) VALUES ($1, $2, $3, 3, 'declared', 'committed')`,
		run, attempt, effect,
	); err == nil {
		t.Fatal("invalid effect transition was accepted")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO effect_transitions (
			run_id, attempt_id, effect_id, event_order, from_state, to_state
		) VALUES ($1, $2, $3, 3, 'declared', 'attempted')`,
		run, attempt, effect,
	); err != nil {
		t.Fatal(err)
	}

	tag, err := pool.Exec(ctx, `
		UPDATE run_attempts
		   SET state = 'frozen', frozen_at = clock_timestamp()
		 WHERE run_id = $1 AND attempt_id = $2 AND state = 'open'`,
		run, attempt,
	)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("freeze attempt: tag=%v err=%v", tag, err)
	}
	if _, err := pool.Exec(ctx, eventSQL, run, attempt, 2, session, 2, "task.started", task, observed); err == nil {
		t.Fatal("event was accepted after freeze")
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("test context expired: %v", err)
	}
}
