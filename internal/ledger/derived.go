package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/fixtureledger"
)

type DerivedStore interface {
	RecordAuthoritativeEffects([]fixtureledger.Record) error
	PersistGraph([]evidence.Edge) error
}

func (p *Postgres) RecordAuthoritativeEffects(records []fixtureledger.Record) error {
	ctx, cancel := p.operationContext(context.Background())
	defer cancel()
	for _, record := range records {
		metadata, err := json.Marshal(map[string]string{"idempotencyKey": record.IdempotencyKey})
		if err != nil {
			return fmt.Errorf("marshal authoritative effect metadata: %w", err)
		}
		if _, err := p.pool.Exec(ctx, `
			INSERT INTO authoritative_effects (run_id,attempt_id,effect_id,kind,idempotency_key,outcome,source,observed_at,metadata)
			VALUES ($1,$2,$3,$4,$5,'committed',$6,$7,$8::jsonb)
			ON CONFLICT (run_id,attempt_id,effect_id,source) DO NOTHING`,
			p.runID, p.attemptID, record.EffectID, record.Kind, record.IdempotencyKey, record.Source, record.CommittedAt, metadata); err != nil {
			return fmt.Errorf("persist authoritative effect %s: %w", record.EffectID, err)
		}
	}
	return nil
}

func (p *Postgres) PersistGraph(edges []evidence.Edge) error {
	ctx, cancel := p.operationContext(context.Background())
	defer cancel()
	for index, edge := range edges {
		if strings.TrimSpace(edge.From) == "" || strings.TrimSpace(edge.To) == "" || strings.TrimSpace(edge.Relation) == "" {
			return fmt.Errorf("invalid causal edge %d", index)
		}
		if _, err := p.pool.Exec(ctx, `
			INSERT INTO causal_edges (run_id,attempt_id,edge_ordinal,from_entity_id,to_entity_id,relation,evidence_order)
			VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,0))
			ON CONFLICT (run_id,attempt_id,from_entity_id,to_entity_id,relation) DO NOTHING`,
			p.runID, p.attemptID, index+1, edge.From, edge.To, edge.Relation, edge.EvidenceOrder); err != nil {
			return fmt.Errorf("persist causal edge %s -> %s: %w", edge.From, edge.To, err)
		}
	}
	return nil
}

var _ DerivedStore = (*Postgres)(nil)
