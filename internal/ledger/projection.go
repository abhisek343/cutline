package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abhisek343/cutline/internal/model"
	"github.com/jackc/pgx/v5"
)

func projectEventTx(ctx context.Context, tx pgx.Tx, event model.Event) error {
	attrs, err := json.Marshal(event.Attributes)
	if err != nil {
		return fmt.Errorf("marshal projection attributes: %w", err)
	}
	name := event.Attributes["name"]
	kind := event.Attributes["kind"]
	switch event.Type {
	case model.EventSessionStarted:
		_, err = tx.Exec(ctx, `INSERT INTO sessions (run_id,attempt_id,session_id,capabilities,started_order) VALUES ($1,$2,$3,$4::jsonb,$5)`, event.RunID, event.AttemptID, event.SessionID, []byte(`[]`), event.CanonicalOrder)
	case model.EventSessionEnded:
		_, err = tx.Exec(ctx, `UPDATE sessions SET ended_order=$4 WHERE run_id=$1 AND attempt_id=$2 AND session_id=$3`, event.RunID, event.AttemptID, event.SessionID, event.CanonicalOrder)
	case model.EventTaskRegistered:
		_, err = tx.Exec(ctx, `INSERT INTO tasks (run_id,attempt_id,task_id,parent_task_id,kind,name,state,registered_order,metadata) VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,'registered',$7,$8::jsonb)`, event.RunID, event.AttemptID, event.EntityID, event.ParentEntityID, kind, name, event.CanonicalOrder, attrs)
	case model.EventTaskStarted:
		_, err = tx.Exec(ctx, `UPDATE tasks SET state='started' WHERE run_id=$1 AND attempt_id=$2 AND task_id=$3`, event.RunID, event.AttemptID, event.EntityID)
	case model.EventTaskFinished:
		state := event.Attributes["status"]
		if state == "" {
			state = string(model.TaskFailed)
		}
		if state != string(model.TaskCompleted) && state != string(model.TaskCancelled) && state != string(model.TaskFailed) {
			state = string(model.TaskLost)
		}
		_, err = tx.Exec(ctx, `UPDATE tasks SET state=$4, terminal_order=$5 WHERE run_id=$1 AND attempt_id=$2 AND task_id=$3`, event.RunID, event.AttemptID, event.EntityID, state, event.CanonicalOrder)
	case model.EventCheckpointReached:
		_, err = tx.Exec(ctx, `INSERT INTO checkpoint_visits (run_id,attempt_id,visit_id,task_id,point_name,state,reached_order) VALUES ($1,$2,$3,$4,$5,'reached',$6)`, event.RunID, event.AttemptID, event.EntityID, event.ParentEntityID, event.Attributes["point"], event.CanonicalOrder)
	case model.EventCheckpointReleased:
		_, err = tx.Exec(ctx, `UPDATE checkpoint_visits SET state='released', terminal_order=$5 WHERE run_id=$1 AND attempt_id=$2 AND visit_id=$3 AND task_id=$4`, event.RunID, event.AttemptID, event.EntityID, event.ParentEntityID, event.CanonicalOrder)
	case model.EventCancelRequested:
		_, err = tx.Exec(ctx, `INSERT INTO cancellations (run_id,attempt_id,cancellation_id,target_task_id,trigger_class,requested_order) VALUES ($1,$2,$3,$4,$5,$6)`, event.RunID, event.AttemptID, event.EntityID, event.Attributes["targetTask"], event.Attributes["trigger"], event.CanonicalOrder)
	case model.EventCancelDelivered:
		_, err = tx.Exec(ctx, `UPDATE cancellations SET delivered_order=$4 WHERE run_id=$1 AND attempt_id=$2 AND cancellation_id=$3`, event.RunID, event.AttemptID, event.EntityID, event.CanonicalOrder)
	case model.EventCancelObserved:
		_, err = tx.Exec(ctx, `UPDATE cancellations SET observed_order=$4 WHERE run_id=$1 AND attempt_id=$2 AND cancellation_id=$3`, event.RunID, event.AttemptID, event.EntityID, event.CanonicalOrder)
	case model.EventEffectDeclared:
		_, err = tx.Exec(ctx, `INSERT INTO effects (run_id,attempt_id,effect_id,owner_task_id,kind,idempotency_key,evidence_source,declared_order) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, event.RunID, event.AttemptID, event.EntityID, event.ParentEntityID, kind, event.Attributes["idempotencyKey"], event.Attributes["evidenceSource"], event.CanonicalOrder)
	case model.EventEffectAttempted, model.EventEffectCommitted, model.EventEffectFailed, model.EventEffectUnknown, model.EventEffectCompensated:
		toState := effectStateForEvent(event.Type)
		var fromState string
		if err := tx.QueryRow(ctx, `SELECT current_state FROM effects WHERE run_id=$1 AND attempt_id=$2 AND effect_id=$3 FOR UPDATE`, event.RunID, event.AttemptID, event.EntityID).Scan(&fromState); err != nil {
			return fmt.Errorf("read effect %s state: %w", event.EntityID, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO effect_transitions (run_id,attempt_id,effect_id,event_order,from_state,to_state) VALUES ($1,$2,$3,$4,$5,$6)`, event.RunID, event.AttemptID, event.EntityID, event.CanonicalOrder, fromState, toState); err != nil {
			return fmt.Errorf("project effect %s transition: %w", event.EntityID, err)
		}
	case model.EventResourceAcquired:
		_, err = tx.Exec(ctx, `INSERT INTO resources (run_id,attempt_id,resource_id,owner_task_id,kind,name,acquired_order) VALUES ($1,$2,$3,$4,$5,$6,$7)`, event.RunID, event.AttemptID, event.EntityID, event.ParentEntityID, kind, name, event.CanonicalOrder)
	case model.EventResourceReleased:
		var fromState string
		if err := tx.QueryRow(ctx, `SELECT current_state FROM resources WHERE run_id=$1 AND attempt_id=$2 AND resource_id=$3 FOR UPDATE`, event.RunID, event.AttemptID, event.EntityID).Scan(&fromState); err != nil {
			return fmt.Errorf("read resource %s state: %w", event.EntityID, err)
		}
		if fromState != string(model.ResourceAcquired) {
			return fmt.Errorf("resource %s transition %s -> released is invalid", event.EntityID, fromState)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO resource_transitions (run_id,attempt_id,resource_id,event_order,from_state,to_state) VALUES ($1,$2,$3,$4,$5,'released')`, event.RunID, event.AttemptID, event.EntityID, event.CanonicalOrder, fromState); err != nil {
			return fmt.Errorf("project resource %s transition: %w", event.EntityID, err)
		}
		_, err = tx.Exec(ctx, `UPDATE resources SET current_state='released' WHERE run_id=$1 AND attempt_id=$2 AND resource_id=$3`, event.RunID, event.AttemptID, event.EntityID)
	case model.EventSchedulerAction:
		_, err = tx.Exec(ctx, `INSERT INTO schedule_actions (run_id,attempt_id,action_ordinal,action_type,target_entity_id,event_order) VALUES ($1,$2,$3,$4,$5,$6)`, event.RunID, event.AttemptID, event.CanonicalOrder, event.Attributes["action"], event.EntityID, event.CanonicalOrder)
	}
	if err != nil {
		return fmt.Errorf("project %s: %w", event.Type, err)
	}
	return nil
}

func effectStateForEvent(eventType model.EventType) string {
	switch eventType {
	case model.EventEffectAttempted:
		return string(model.EffectAttempted)
	case model.EventEffectCommitted:
		return string(model.EffectCommitted)
	case model.EventEffectFailed:
		return string(model.EffectFailed)
	case model.EventEffectUnknown:
		return string(model.EffectUnknown)
	case model.EventEffectCompensated:
		return string(model.EffectCompensated)
	default:
		return strings.TrimSpace(string(eventType))
	}
}
