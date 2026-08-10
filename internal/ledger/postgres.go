package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AttemptConfig struct {
	RunID            model.RunID
	AttemptID        model.AttemptID
	CampaignDigest   string
	CampaignName     string
	CampaignJSON     []byte
	TargetDigest     string
	Adapter          string
	AdapterVersion   string
	TargetMetadata   map[string]any
	Seed             int64
	MaxEvents        int
	OperationTimeout time.Duration
}

type Postgres struct {
	pool             *pgxpool.Pool
	runID            model.RunID
	attemptID        model.AttemptID
	maxEvents        int
	operationTimeout time.Duration
}

func NewPostgres(ctx context.Context, dsn string, config AttemptConfig) (*Postgres, error) {
	if err := validateAttemptConfig(dsn, config); err != nil {
		return nil, err
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL DSN: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	store := &Postgres{
		pool: pool, runID: config.RunID, attemptID: config.AttemptID,
		maxEvents: config.MaxEvents, operationTimeout: config.OperationTimeout,
	}
	if err := store.createAttempt(ctx, config); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func validateAttemptConfig(dsn string, config AttemptConfig) error {
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("PostgreSQL DSN is required")
	}
	for name, value := range map[string]string{
		"runId": string(config.RunID), "attemptId": string(config.AttemptID),
		"campaignDigest": config.CampaignDigest, "targetDigest": config.TargetDigest,
	} {
		if err := model.ValidateID(value); err != nil && (name == "runId" || name == "attemptId") {
			return fmt.Errorf("invalid %s: %w", name, err)
		}
	}
	for name, digest := range map[string]string{"campaignDigest": config.CampaignDigest, "targetDigest": config.TargetDigest} {
		if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if strings.TrimSpace(config.CampaignName) == "" || strings.TrimSpace(config.Adapter) == "" || strings.TrimSpace(config.AdapterVersion) == "" {
		return fmt.Errorf("campaign name, adapter, and adapter version are required")
	}
	if len(config.CampaignJSON) == 0 || !json.Valid(config.CampaignJSON) {
		return fmt.Errorf("campaign JSON must be valid")
	}
	return nil
}

func (p *Postgres) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}

func (p *Postgres) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if p.operationTimeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, p.operationTimeout)
}

func (p *Postgres) createAttempt(parent context.Context, config AttemptConfig) error {
	ctx, cancel := p.operationContext(parent)
	defer cancel()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin attempt: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck -- commit determines the result
	metadata, err := json.Marshal(config.TargetMetadata)
	if err != nil {
		return fmt.Errorf("marshal target metadata: %w", err)
	}
	if len(metadata) == 0 || string(metadata) == "null" {
		metadata = []byte(`{}`)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO campaigns (digest, api_version, name, specification)
		VALUES ($1, 'cutline.dev/v1alpha1', $2, $3::jsonb)
		ON CONFLICT (digest) DO NOTHING`, config.CampaignDigest, config.CampaignName, config.CampaignJSON); err != nil {
		return fmt.Errorf("store campaign: %w", err)
	}
	var existingCampaign []byte
	if err := tx.QueryRow(ctx, `SELECT specification::text FROM campaigns WHERE digest = $1`, config.CampaignDigest).Scan(&existingCampaign); err != nil {
		return fmt.Errorf("read campaign identity: %w", err)
	}
	var want, got any
	if json.Unmarshal(existingCampaign, &got) != nil || json.Unmarshal(config.CampaignJSON, &want) != nil {
		return fmt.Errorf("decode campaign identity")
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		return fmt.Errorf("campaign digest collision")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO target_builds (digest, adapter, adapter_version, metadata)
		VALUES ($1, $2, $3, $4::jsonb)
		ON CONFLICT (digest) DO NOTHING`, config.TargetDigest, config.Adapter, config.AdapterVersion, metadata); err != nil {
		return fmt.Errorf("store target build: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO runs (run_id, campaign_digest, target_digest, seed)
		VALUES ($1, $2, $3, $4)`, config.RunID, config.CampaignDigest, config.TargetDigest, config.Seed); err != nil {
		return fmt.Errorf("store run: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO run_attempts (run_id, attempt_id) VALUES ($1, $2)`, config.RunID, config.AttemptID); err != nil {
		return fmt.Errorf("store attempt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit attempt: %w", err)
	}
	return nil
}

func (p *Postgres) Append(event model.Event) (model.Event, error) {
	ctx, cancel := p.operationContext(context.Background())
	defer cancel()
	if event.RunID != p.runID || event.AttemptID != p.attemptID {
		return model.Event{}, ingest.ErrIdentity
	}
	if err := event.Validate(); err != nil {
		return model.Event{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return model.Event{}, fmt.Errorf("begin evidence append: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var state string
	var next uint64
	if err := tx.QueryRow(ctx, `
		SELECT state, next_canonical_order FROM run_attempts
		WHERE run_id = $1 AND attempt_id = $2 FOR UPDATE`, p.runID, p.attemptID).Scan(&state, &next); err != nil {
		return model.Event{}, fmt.Errorf("lock attempt: %w", err)
	}
	if state != "open" {
		return model.Event{}, ingest.ErrFrozen
	}
	if p.maxEvents > 0 && next > uint64(p.maxEvents) {
		if err := markIncompleteTx(ctx, tx, p.runID, p.attemptID, ingest.ErrEventLimit.Error()); err != nil {
			return model.Event{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return model.Event{}, fmt.Errorf("commit event-limit evidence: %w", err)
		}
		return model.Event{}, ingest.ErrEventLimit
	}
	var last uint64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(local_sequence), 0) FROM events
		WHERE run_id = $1 AND attempt_id = $2 AND session_id = $3`, p.runID, p.attemptID, event.SessionID).Scan(&last); err != nil {
		return model.Event{}, fmt.Errorf("read local evidence sequence: %w", err)
	}
	if event.LocalSequence == last {
		return model.Event{}, ingest.ErrDuplicate
	}
	if event.LocalSequence != last+1 {
		reason := fmt.Sprintf("%v: session %s got %d after %d", ingest.ErrSequenceGap, event.SessionID, event.LocalSequence, last)
		if err := markIncompleteTx(ctx, tx, p.runID, p.attemptID, reason); err != nil {
			return model.Event{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return model.Event{}, fmt.Errorf("commit sequence-gap evidence: %w", err)
		}
		return model.Event{}, ingest.ErrSequenceGap
	}
	attributes, err := json.Marshal(event.Attributes)
	if err != nil {
		return model.Event{}, fmt.Errorf("marshal event attributes: %w", err)
	}
	if string(attributes) == "null" {
		attributes = []byte(`{}`)
	}
	event.CanonicalOrder = next
	if _, err := tx.Exec(ctx, `
		INSERT INTO events (run_id, attempt_id, canonical_order, session_id, local_sequence, schema_version, event_type, entity_id, parent_entity_id, observed_at, monotonic_nanos, attributes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,$11,$12::jsonb)`,
		event.RunID, event.AttemptID, event.CanonicalOrder, event.SessionID, event.LocalSequence,
		event.SchemaVersion, event.Type, event.EntityID, event.ParentEntityID, event.ObservedAt,
		event.MonotonicNanos, attributes); err != nil {
		return model.Event{}, fmt.Errorf("insert evidence event: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE run_attempts SET next_canonical_order = next_canonical_order + 1 WHERE run_id = $1 AND attempt_id = $2`, p.runID, p.attemptID); err != nil {
		return model.Event{}, fmt.Errorf("advance evidence order: %w", err)
	}
	if err := projectEventTx(ctx, tx, event); err != nil {
		return model.Event{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Event{}, fmt.Errorf("commit evidence event: %w", err)
	}
	return event, nil
}

func markIncompleteTx(ctx context.Context, tx pgx.Tx, runID model.RunID, attemptID model.AttemptID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO evidence_incomplete_reasons (run_id, attempt_id, reason)
		VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, runID, attemptID, reason); err != nil {
		return fmt.Errorf("record incomplete evidence: %w", err)
	}
	return nil
}

func (p *Postgres) MarkIncomplete(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	ctx, cancel := p.operationContext(context.Background())
	defer cancel()
	_, err := p.pool.Exec(ctx, `
		INSERT INTO evidence_incomplete_reasons (run_id, attempt_id, reason)
		VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, p.runID, p.attemptID, reason)
	if err != nil {
		if strings.Contains(err.Error(), "frozen") {
			return ingest.ErrFrozen
		}
		return fmt.Errorf("record incomplete evidence: %w", err)
	}
	return nil
}

func (p *Postgres) Current() ([]model.Event, error) {
	ctx, cancel := p.operationContext(context.Background())
	defer cancel()
	return p.events(ctx, p.pool)
}

func (p *Postgres) Freeze() (ingest.Snapshot, error) {
	ctx, cancel := p.operationContext(context.Background())
	defer cancel()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return ingest.Snapshot{}, fmt.Errorf("begin evidence freeze: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM run_attempts WHERE run_id=$1 AND attempt_id=$2 FOR UPDATE`, p.runID, p.attemptID).Scan(&state); err != nil {
		return ingest.Snapshot{}, fmt.Errorf("lock evidence freeze: %w", err)
	}
	if state == "open" {
		if _, err := tx.Exec(ctx, `UPDATE run_attempts SET state='frozen', frozen_at=clock_timestamp() WHERE run_id=$1 AND attempt_id=$2`, p.runID, p.attemptID); err != nil {
			return ingest.Snapshot{}, fmt.Errorf("freeze attempt: %w", err)
		}
	} else if state != "frozen" {
		return ingest.Snapshot{}, fmt.Errorf("unknown attempt state %q", state)
	}
	events, err := p.events(ctx, tx)
	if err != nil {
		return ingest.Snapshot{}, err
	}
	rows, err := tx.Query(ctx, `SELECT reason FROM evidence_incomplete_reasons WHERE run_id=$1 AND attempt_id=$2 ORDER BY reason`, p.runID, p.attemptID)
	if err != nil {
		return ingest.Snapshot{}, fmt.Errorf("read incomplete reasons: %w", err)
	}
	reasons, err := pgStrings(rows)
	if err != nil {
		return ingest.Snapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ingest.Snapshot{}, fmt.Errorf("commit evidence freeze: %w", err)
	}
	return ingest.Snapshot{RunID: p.runID, AttemptID: p.attemptID, Events: events, IncompleteReasons: reasons}, nil
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (p *Postgres) events(ctx context.Context, q queryer) ([]model.Event, error) {
	rows, err := q.Query(ctx, `
		SELECT session_id, local_sequence, canonical_order, schema_version, event_type, entity_id,
		       COALESCE(parent_entity_id,''), observed_at, monotonic_nanos, attributes::text
		FROM events WHERE run_id=$1 AND attempt_id=$2 ORDER BY canonical_order`, p.runID, p.attemptID)
	if err != nil {
		return nil, fmt.Errorf("read evidence events: %w", err)
	}
	defer rows.Close()
	result := make([]model.Event, 0)
	for rows.Next() {
		var event model.Event
		var attributes string
		if err := rows.Scan(&event.SessionID, &event.LocalSequence, &event.CanonicalOrder, &event.SchemaVersion, &event.Type, &event.EntityID, &event.ParentEntityID, &event.ObservedAt, &event.MonotonicNanos, &attributes); err != nil {
			return nil, fmt.Errorf("scan evidence event: %w", err)
		}
		event.RunID, event.AttemptID = p.runID, p.attemptID
		if err := json.Unmarshal([]byte(attributes), &event.Attributes); err != nil {
			return nil, fmt.Errorf("decode evidence attributes: %w", err)
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read evidence events: %w", err)
	}
	return result, nil
}

func pgStrings(rows pgx.Rows) ([]string, error) {
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan incomplete reason: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read incomplete reasons: %w", err)
	}
	return result, nil
}

var _ ingest.Store = (*Postgres)(nil)
