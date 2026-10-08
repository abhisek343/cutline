//go:build integration

package ledger

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPostgresSchemaConstraintsAndFreeze(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dsn := postgresTestDSN(t, ctx)
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
		digest   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		target   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		session  = "session_33333333333333333333333333333333"
		task     = "task_44444444444444444444444444444444"
		effect   = "effect_55555555555555555555555555555555"
		observed = "2026-07-25T00:00:00Z"
		eventSQL = `INSERT INTO events (
			run_id, attempt_id, canonical_order, session_id, local_sequence,
			schema_version, event_type, entity_id, observed_at
		) VALUES ($1, $2, $3, $4, $5, 1, $6, $7, $8)`
	)
	run, err := model.NewID("run")
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := model.NewID("attempt")
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO campaigns (digest, api_version, name, specification)
		 VALUES ('` + digest + `', 'cutline.dev/v1alpha1', 'test', '{}') ON CONFLICT DO NOTHING`,
		`INSERT INTO target_builds (digest, adapter, adapter_version)
		 VALUES ('` + target + `', 'go-test', '1') ON CONFLICT DO NOTHING`,
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

func postgresTestDSN(t *testing.T, ctx context.Context) string {
	t.Helper()
	if dsn := os.Getenv("CUTLINE_TEST_POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	container, err := tcpostgres.Run(ctx, "postgres:17.5-alpine", tcpostgres.WithDatabase("cutline"), tcpostgres.WithUsername("cutline"), tcpostgres.WithPassword("cutline-test"), tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

func newTestPostgres(t *testing.T, ctx context.Context, dsn string, maxEvents int) *Postgres {
	t.Helper()
	run, err := model.NewID("run")
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := model.NewID("attempt")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewPostgres(ctx, dsn, AttemptConfig{
		RunID: model.RunID(run), AttemptID: model.AttemptID(attempt),
		CampaignDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CampaignName:   "test", CampaignJSON: []byte(`{}`),
		TargetDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Adapter:      "go-test", AdapterVersion: "native/go-test/1", MaxEvents: maxEvents, OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func testEvent(p *Postgres, sequence uint64, kind model.EventType, entity, parent string, attrs map[string]string) model.Event {
	return model.Event{SchemaVersion: model.EventSchemaVersion, RunID: p.runID, AttemptID: p.attemptID,
		SessionID: "session_33333333333333333333333333333333", LocalSequence: sequence, Type: kind, EntityID: entity, ParentEntityID: parent,
		ObservedAt: time.Date(2026, 7, 25, 0, 0, int(sequence), 0, time.UTC), Attributes: attrs}
}

func TestPostgresStoreLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn := postgresTestDSN(t, ctx)
	const session = "session_33333333333333333333333333333333"
	const task = "task_44444444444444444444444444444444"
	const effect = "effect_55555555555555555555555555555555"
	t.Run("append projects and frozen snapshot reads durable evidence", func(t *testing.T) {
		p := newTestPostgres(t, ctx, dsn, 100)
		events := []model.Event{
			testEvent(p, 1, model.EventSessionStarted, session, "", nil),
			testEvent(p, 2, model.EventTaskRegistered, task, "", map[string]string{"kind": "native.root", "name": "test"}),
			testEvent(p, 3, model.EventTaskStarted, task, "", nil),
			testEvent(p, 4, model.EventEffectDeclared, effect, task, map[string]string{"kind": "payment.charge", "idempotencyKey": "order-1", "evidenceSource": "fixture"}),
			testEvent(p, 5, model.EventEffectAttempted, effect, task, nil),
			testEvent(p, 6, model.EventEffectCommitted, effect, task, nil),
			testEvent(p, 7, model.EventTaskFinished, task, "", map[string]string{"status": "completed"}),
			testEvent(p, 8, model.EventSessionEnded, session, "", nil),
		}
		for i, event := range events {
			accepted, err := p.Append(event)
			if err != nil {
				t.Fatalf("append %s: %v", event.Type, err)
			}
			if accepted.CanonicalOrder != uint64(i+1) {
				t.Fatalf("order=%d", accepted.CanonicalOrder)
			}
		}
		var effectState, taskState string
		var transitions int
		if err := p.pool.QueryRow(ctx, `SELECT current_state FROM effects WHERE run_id=$1 AND effect_id=$2`, p.runID, effect).Scan(&effectState); err != nil {
			t.Fatal(err)
		}
		if err := p.pool.QueryRow(ctx, `SELECT state FROM tasks WHERE run_id=$1 AND task_id=$2`, p.runID, task).Scan(&taskState); err != nil {
			t.Fatal(err)
		}
		if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM effect_transitions WHERE run_id=$1`, p.runID).Scan(&transitions); err != nil {
			t.Fatal(err)
		}
		if effectState != "committed" || taskState != "completed" || transitions != 2 {
			t.Fatalf("projections effect=%s task=%s transitions=%d", effectState, taskState, transitions)
		}
		if err := p.MarkIncomplete("interrupted observation"); err != nil {
			t.Fatal(err)
		}
		if err := p.MarkIncomplete("interrupted observation"); err != nil {
			t.Fatal(err)
		}
		first, err := p.Freeze()
		if err != nil {
			t.Fatal(err)
		}
		second, err := p.Freeze()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, second) || len(first.Events) != len(events) || !reflect.DeepEqual(first.IncompleteReasons, []string{"interrupted observation"}) {
			t.Fatalf("frozen snapshot mismatch: %#v", first)
		}
		if _, err := p.Append(testEvent(p, 9, model.EventSessionEnded, session, "", nil)); !errors.Is(err, ingest.ErrFrozen) {
			t.Fatalf("post-freeze append=%v", err)
		}
		if err := p.MarkIncomplete("late"); !errors.Is(err, ingest.ErrFrozen) {
			t.Fatalf("post-freeze incomplete=%v", err)
		}
		current, err := p.Current()
		if err != nil || !reflect.DeepEqual(current, first.Events) {
			t.Fatalf("durable current=%v err=%v", current, err)
		}
		var state string
		var next int
		var frozen bool
		if err := p.pool.QueryRow(ctx, `SELECT state,next_canonical_order,frozen_at IS NOT NULL FROM run_attempts WHERE run_id=$1`, p.runID).Scan(&state, &next, &frozen); err != nil {
			t.Fatal(err)
		}
		if state != "frozen" || next != 9 || !frozen {
			t.Fatalf("attempt=%s next=%d frozen=%t", state, next, frozen)
		}
	})
	t.Run("invalid projection rolls back event and order", func(t *testing.T) {
		p := newTestPostgres(t, ctx, dsn, 100)
		event := testEvent(p, 1, model.EventEffectCommitted, effect, task, nil)
		if _, err := p.Append(event); err == nil {
			t.Fatal("unregistered effect commitment accepted")
		}
		accepted, err := p.Append(testEvent(p, 1, model.EventSessionStarted, session, "", nil))
		if err != nil || accepted.CanonicalOrder != 1 {
			t.Fatalf("rollback did not preserve first order: %v %v", accepted, err)
		}
		snapshot, err := p.Freeze()
		if err != nil || len(snapshot.Events) != 1 {
			t.Fatalf("partial transaction persisted: %#v %v", snapshot, err)
		}
	})
	t.Run("gaps and event limits never freeze complete", func(t *testing.T) {
		for _, test := range []struct {
			name     string
			max      int
			sequence uint64
			want     error
		}{
			{"sequence gap", 10, 3, ingest.ErrSequenceGap}, {"event limit", 1, 2, ingest.ErrEventLimit},
		} {
			t.Run(test.name, func(t *testing.T) {
				p := newTestPostgres(t, ctx, dsn, test.max)
				first := testEvent(p, 1, model.EventSessionStarted, session, "", nil)
				if _, err := p.Append(first); err != nil {
					t.Fatal(err)
				}
				if test.max > 1 {
					if _, err := p.Append(first); !errors.Is(err, ingest.ErrDuplicate) {
						t.Fatalf("duplicate=%v", err)
					}
				}
				if _, err := p.Append(testEvent(p, test.sequence, model.EventSessionEnded, session, "", nil)); !errors.Is(err, test.want) {
					t.Fatalf("rejection=%v want=%v", err, test.want)
				}
				frozen, err := p.Freeze()
				if err != nil {
					t.Fatal(err)
				}
				if frozen.Complete() || len(frozen.Events) != 1 {
					t.Fatalf("truncated evidence falsely complete: %#v", frozen)
				}
			})
		}
	})
	t.Run("interrupted pool reports write read and freeze errors", func(t *testing.T) {
		p := newTestPostgres(t, ctx, dsn, 100)
		p.Close()
		if _, err := p.Append(testEvent(p, 1, model.EventSessionStarted, session, "", nil)); err == nil {
			t.Fatal("closed pool append succeeded")
		}
		if err := p.MarkIncomplete("connection lost"); err == nil {
			t.Fatal("closed pool marker succeeded")
		}
		if events, err := p.Current(); err == nil || events != nil {
			t.Fatalf("closed pool read returned evidence: %v %v", events, err)
		}
		if snapshot, err := p.Freeze(); err == nil || snapshot.RunID != "" {
			t.Fatalf("closed pool freeze returned snapshot: %#v %v", snapshot, err)
		}
	})
	t.Run("blocked write timeout does not append or freeze", func(t *testing.T) {
		p := newTestPostgres(t, ctx, dsn, 100)
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `SELECT state FROM run_attempts WHERE run_id=$1 FOR UPDATE`, p.runID); err != nil {
			t.Fatal(err)
		}
		p.operationTimeout = 30 * time.Millisecond
		if _, err := p.Append(testEvent(p, 1, model.EventSessionStarted, session, "", nil)); err == nil {
			t.Fatal("blocked append unexpectedly succeeded")
		}
		if _, err := p.Freeze(); err == nil {
			t.Fatal("blocked freeze unexpectedly succeeded")
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		p.operationTimeout = time.Second
		current, err := p.Current()
		if err != nil || len(current) != 0 {
			t.Fatalf("timeout wrote evidence: %v %v", current, err)
		}
		var state string
		if err := p.pool.QueryRow(ctx, `SELECT state FROM run_attempts WHERE run_id=$1`, p.runID).Scan(&state); err != nil || state != "open" {
			t.Fatalf("timed out freeze changed state=%s err=%v", state, err)
		}
	})
	t.Run("freeze serializes concurrent append and incomplete marker", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			p := newTestPostgres(t, ctx, dsn, 100)
			start := make(chan struct{})
			appendResult := make(chan error, 1)
			markResult := make(chan error, 1)
			go func() {
				<-start
				_, err := p.Append(testEvent(p, 1, model.EventSessionStarted, session, "", nil))
				appendResult <- err
			}()
			go func() { <-start; markResult <- p.MarkIncomplete("concurrent interruption") }()
			close(start)
			snapshot, err := p.Freeze()
			if err != nil {
				t.Fatal(err)
			}
			appendErr, markErr := <-appendResult, <-markResult
			for _, err := range []error{appendErr, markErr} {
				if err != nil && !errors.Is(err, ingest.ErrFrozen) {
					t.Fatal(err)
				}
			}
			if (appendErr == nil) != (len(snapshot.Events) == 1) || (markErr == nil) != (len(snapshot.IncompleteReasons) == 1) {
				t.Fatalf("freeze lost accepted evidence: append=%v mark=%v snapshot=%#v", appendErr, markErr, snapshot)
			}
			again, err := p.Freeze()
			if err != nil || !reflect.DeepEqual(snapshot, again) {
				t.Fatalf("snapshot changed after freeze: %v", err)
			}
		}
	})
}
