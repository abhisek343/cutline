CREATE TABLE IF NOT EXISTS cutline_schema_migrations (
    version TEXT PRIMARY KEY,
    checksum CHAR(64) NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    applied_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE campaigns (
    digest CHAR(64) PRIMARY KEY CHECK (digest ~ '^[0-9a-f]{64}$'),
    api_version TEXT NOT NULL,
    name TEXT NOT NULL,
    specification JSONB NOT NULL CHECK (jsonb_typeof(specification) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE target_builds (
    digest CHAR(64) PRIMARY KEY CHECK (digest ~ '^[0-9a-f]{64}$'),
    adapter TEXT NOT NULL,
    adapter_version TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE runs (
    run_id TEXT PRIMARY KEY CHECK (run_id ~ '^run_[0-9a-f]{32}$'),
    campaign_digest CHAR(64) NOT NULL REFERENCES campaigns(digest),
    target_digest CHAR(64) NOT NULL REFERENCES target_builds(digest),
    seed BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE run_attempts (
    run_id TEXT NOT NULL REFERENCES runs(run_id),
    attempt_id TEXT NOT NULL CHECK (attempt_id ~ '^attempt_[0-9a-f]{32}$'),
    state TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'frozen')),
    next_canonical_order BIGINT NOT NULL DEFAULT 1 CHECK (next_canonical_order > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    frozen_at TIMESTAMPTZ,
    PRIMARY KEY (run_id, attempt_id),
    UNIQUE (attempt_id),
    CHECK (
        (state = 'open' AND frozen_at IS NULL) OR
        (state = 'frozen' AND frozen_at IS NOT NULL)
    )
);

CREATE TABLE sessions (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    session_id TEXT NOT NULL CHECK (session_id ~ '^session_[0-9a-f]{32}$'),
    capabilities JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(capabilities) = 'array'),
    started_order BIGINT NOT NULL CHECK (started_order > 0),
    ended_order BIGINT CHECK (ended_order > started_order),
    PRIMARY KEY (run_id, attempt_id, session_id),
    FOREIGN KEY (run_id, attempt_id) REFERENCES run_attempts(run_id, attempt_id)
);

CREATE TABLE tasks (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    task_id TEXT NOT NULL CHECK (task_id ~ '^task_[0-9a-f]{32}$'),
    parent_task_id TEXT,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('registered', 'started', 'completed', 'cancelled', 'failed', 'lost')),
    registered_order BIGINT NOT NULL CHECK (registered_order > 0),
    terminal_order BIGINT CHECK (terminal_order >= registered_order),
    metadata JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(metadata) = 'object'),
    PRIMARY KEY (run_id, attempt_id, task_id),
    FOREIGN KEY (run_id, attempt_id) REFERENCES run_attempts(run_id, attempt_id),
    FOREIGN KEY (run_id, attempt_id, parent_task_id)
        REFERENCES tasks(run_id, attempt_id, task_id)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE checkpoint_visits (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    visit_id TEXT NOT NULL CHECK (visit_id ~ '^visit_[0-9a-f]{32}$'),
    task_id TEXT NOT NULL,
    point_name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('reached', 'blocked', 'released', 'cancelled', 'disconnected')),
    reached_order BIGINT NOT NULL CHECK (reached_order > 0),
    terminal_order BIGINT CHECK (terminal_order >= reached_order),
    PRIMARY KEY (run_id, attempt_id, visit_id),
    FOREIGN KEY (run_id, attempt_id, task_id) REFERENCES tasks(run_id, attempt_id, task_id)
);

CREATE TABLE cancellations (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    cancellation_id TEXT NOT NULL CHECK (cancellation_id ~ '^cancel_[0-9a-f]{32}$'),
    target_task_id TEXT NOT NULL,
    trigger_class TEXT NOT NULL,
    requested_order BIGINT NOT NULL CHECK (requested_order > 0),
    delivered_order BIGINT CHECK (delivered_order >= requested_order),
    observed_order BIGINT CHECK (
        observed_order IS NULL OR
        delivered_order IS NOT NULL AND observed_order >= delivered_order
    ),
    PRIMARY KEY (run_id, attempt_id, cancellation_id),
    FOREIGN KEY (run_id, attempt_id, target_task_id) REFERENCES tasks(run_id, attempt_id, task_id)
);

CREATE TABLE effects (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    effect_id TEXT NOT NULL CHECK (effect_id ~ '^effect_[0-9a-f]{32}$'),
    owner_task_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    schema_version INTEGER NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    idempotency_key TEXT NOT NULL,
    evidence_source TEXT NOT NULL,
    current_state TEXT NOT NULL DEFAULT 'declared'
        CHECK (current_state IN ('declared', 'attempted', 'committed', 'failed', 'unknown', 'compensated')),
    declared_order BIGINT NOT NULL CHECK (declared_order > 0),
    PRIMARY KEY (run_id, attempt_id, effect_id),
    FOREIGN KEY (run_id, attempt_id, owner_task_id) REFERENCES tasks(run_id, attempt_id, task_id)
);

CREATE TABLE effect_transitions (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    effect_id TEXT NOT NULL,
    event_order BIGINT NOT NULL CHECK (event_order > 0),
    from_state TEXT NOT NULL
        CHECK (from_state IN ('declared', 'attempted', 'committed', 'failed', 'unknown', 'compensated')),
    to_state TEXT NOT NULL
        CHECK (to_state IN ('attempted', 'committed', 'failed', 'unknown', 'compensated')),
    PRIMARY KEY (run_id, attempt_id, effect_id, event_order),
    FOREIGN KEY (run_id, attempt_id, effect_id) REFERENCES effects(run_id, attempt_id, effect_id)
);

CREATE TABLE resources (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    resource_id TEXT NOT NULL CHECK (resource_id ~ '^resource_[0-9a-f]{32}$'),
    owner_task_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    current_state TEXT NOT NULL DEFAULT 'acquired'
        CHECK (current_state IN ('acquired', 'released', 'expired')),
    acquired_order BIGINT NOT NULL CHECK (acquired_order > 0),
    PRIMARY KEY (run_id, attempt_id, resource_id),
    FOREIGN KEY (run_id, attempt_id, owner_task_id) REFERENCES tasks(run_id, attempt_id, task_id)
);

CREATE TABLE resource_transitions (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    event_order BIGINT NOT NULL CHECK (event_order > 0),
    from_state TEXT NOT NULL CHECK (from_state IN ('acquired', 'released', 'expired')),
    to_state TEXT NOT NULL CHECK (to_state IN ('released', 'expired')),
    PRIMARY KEY (run_id, attempt_id, resource_id, event_order),
    FOREIGN KEY (run_id, attempt_id, resource_id) REFERENCES resources(run_id, attempt_id, resource_id)
);

CREATE TABLE events (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    canonical_order BIGINT NOT NULL CHECK (canonical_order > 0),
    session_id TEXT NOT NULL CHECK (session_id ~ '^session_[0-9a-f]{32}$'),
    local_sequence BIGINT NOT NULL CHECK (local_sequence > 0),
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    event_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    parent_entity_id TEXT,
    observed_at TIMESTAMPTZ NOT NULL,
    monotonic_nanos BIGINT NOT NULL DEFAULT 0 CHECK (monotonic_nanos >= 0),
    attributes JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(attributes) = 'object'),
    PRIMARY KEY (run_id, attempt_id, canonical_order),
    UNIQUE (run_id, attempt_id, session_id, local_sequence),
    FOREIGN KEY (run_id, attempt_id) REFERENCES run_attempts(run_id, attempt_id)
);

CREATE TABLE schedule_actions (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    action_ordinal BIGINT NOT NULL CHECK (action_ordinal > 0),
    action_type TEXT NOT NULL,
    target_entity_id TEXT NOT NULL,
    precondition_digest CHAR(64) CHECK (
        precondition_digest IS NULL OR precondition_digest ~ '^[0-9a-f]{64}$'
    ),
    event_order BIGINT NOT NULL CHECK (event_order > 0),
    PRIMARY KEY (run_id, attempt_id, action_ordinal),
    UNIQUE (run_id, attempt_id, event_order),
    FOREIGN KEY (run_id, attempt_id) REFERENCES run_attempts(run_id, attempt_id)
);

CREATE TABLE authoritative_effects (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    effect_id TEXT NOT NULL CHECK (effect_id ~ '^effect_[0-9a-f]{32}$'),
    kind TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('committed', 'failed', 'unknown', 'compensated')),
    source TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(metadata) = 'object'),
    PRIMARY KEY (run_id, attempt_id, effect_id, source),
    FOREIGN KEY (run_id, attempt_id) REFERENCES run_attempts(run_id, attempt_id)
);

CREATE TABLE evidence_incomplete_reasons (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 4096),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (run_id, attempt_id, reason),
    FOREIGN KEY (run_id, attempt_id) REFERENCES run_attempts(run_id, attempt_id)
);

CREATE TABLE causal_edges (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    edge_ordinal BIGINT NOT NULL CHECK (edge_ordinal > 0),
    from_entity_id TEXT NOT NULL,
    to_entity_id TEXT NOT NULL,
    relation TEXT NOT NULL,
    evidence_order BIGINT CHECK (evidence_order > 0),
    PRIMARY KEY (run_id, attempt_id, edge_ordinal),
    UNIQUE (run_id, attempt_id, from_entity_id, to_entity_id, relation),
    FOREIGN KEY (run_id, attempt_id) REFERENCES run_attempts(run_id, attempt_id),
    CHECK (from_entity_id <> to_entity_id)
);

CREATE TABLE contract_evaluations (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    contract_name TEXT NOT NULL,
    contract_version INTEGER NOT NULL CHECK (contract_version > 0),
    status TEXT NOT NULL CHECK (status IN ('pass', 'violation', 'inconclusive', 'invalid')),
    message TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(details) = 'object'),
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (run_id, attempt_id, contract_name, contract_version),
    FOREIGN KEY (run_id, attempt_id) REFERENCES run_attempts(run_id, attempt_id)
);

CREATE TABLE failure_signatures (
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    digest CHAR(64) NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    signature JSONB NOT NULL CHECK (jsonb_typeof(signature) = 'object'),
    PRIMARY KEY (run_id, attempt_id, digest),
    FOREIGN KEY (run_id, attempt_id) REFERENCES run_attempts(run_id, attempt_id)
);

CREATE TABLE capsules (
    digest CHAR(64) PRIMARY KEY CHECK (digest ~ '^[0-9a-f]{64}$'),
    run_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    manifest JSONB NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (run_id, attempt_id) REFERENCES run_attempts(run_id, attempt_id)
);

CREATE TABLE artifacts (
    digest CHAR(64) PRIMARY KEY CHECK (digest ~ '^[0-9a-f]{64}$'),
    capsule_digest CHAR(64) REFERENCES capsules(digest),
    media_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    relative_path TEXT NOT NULL CHECK (
        relative_path <> '' AND
        relative_path !~ '(^|/)\.\.(/|$)' AND
        relative_path !~ '^/'
    ),
    metadata JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE OR REPLACE FUNCTION cutline_attempt_is_open(p_run_id TEXT, p_attempt_id TEXT)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM run_attempts
        WHERE run_id = p_run_id
          AND attempt_id = p_attempt_id
          AND state = 'open'
    )
$$;

CREATE OR REPLACE FUNCTION cutline_guard_evidence_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    guarded_run_id TEXT;
    guarded_attempt_id TEXT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        guarded_run_id := OLD.run_id;
        guarded_attempt_id := OLD.attempt_id;
    ELSE
        guarded_run_id := NEW.run_id;
        guarded_attempt_id := NEW.attempt_id;
    END IF;

    IF NOT cutline_attempt_is_open(guarded_run_id, guarded_attempt_id) THEN
        RAISE EXCEPTION 'attempt %/% is frozen or missing',
            guarded_run_id, guarded_attempt_id
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END
$$;

CREATE OR REPLACE FUNCTION cutline_guard_append_only()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = '55000';
    END IF;
    IF NOT cutline_attempt_is_open(NEW.run_id, NEW.attempt_id) THEN
        RAISE EXCEPTION 'attempt %/% is frozen or missing', NEW.run_id, NEW.attempt_id
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$;

CREATE OR REPLACE FUNCTION cutline_validate_effect_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    actual_state TEXT;
BEGIN
    SELECT current_state
      INTO actual_state
      FROM effects
     WHERE run_id = NEW.run_id
       AND attempt_id = NEW.attempt_id
       AND effect_id = NEW.effect_id
     FOR UPDATE;

    IF actual_state IS NULL OR actual_state <> NEW.from_state THEN
        RAISE EXCEPTION 'effect transition expected %, found %',
            NEW.from_state, COALESCE(actual_state, '<missing>')
            USING ERRCODE = '23514';
    END IF;
    IF NOT (
        (NEW.from_state = 'declared' AND NEW.to_state = 'attempted') OR
        (NEW.from_state = 'attempted' AND NEW.to_state IN ('committed', 'failed', 'unknown')) OR
        (NEW.from_state = 'committed' AND NEW.to_state = 'compensated')
    ) THEN
        RAISE EXCEPTION 'invalid effect transition % -> %', NEW.from_state, NEW.to_state
            USING ERRCODE = '23514';
    END IF;

    UPDATE effects
       SET current_state = NEW.to_state
     WHERE run_id = NEW.run_id
       AND attempt_id = NEW.attempt_id
       AND effect_id = NEW.effect_id;
    RETURN NEW;
END
$$;

CREATE TRIGGER events_append_only
BEFORE INSERT OR UPDATE OR DELETE ON events
FOR EACH ROW EXECUTE FUNCTION cutline_guard_append_only();

CREATE TRIGGER effect_transitions_append_only
BEFORE INSERT OR UPDATE OR DELETE ON effect_transitions
FOR EACH ROW EXECUTE FUNCTION cutline_guard_append_only();

CREATE TRIGGER resource_transitions_append_only
BEFORE INSERT OR UPDATE OR DELETE ON resource_transitions
FOR EACH ROW EXECUTE FUNCTION cutline_guard_append_only();

CREATE TRIGGER authoritative_effects_append_only
BEFORE INSERT OR UPDATE OR DELETE ON authoritative_effects
FOR EACH ROW EXECUTE FUNCTION cutline_guard_append_only();

CREATE TRIGGER incomplete_reasons_append_only
BEFORE INSERT OR UPDATE OR DELETE ON evidence_incomplete_reasons
FOR EACH ROW EXECUTE FUNCTION cutline_guard_append_only();

CREATE TRIGGER validate_effect_transition
BEFORE INSERT ON effect_transitions
FOR EACH ROW EXECUTE FUNCTION cutline_validate_effect_transition();

CREATE TRIGGER sessions_open_attempt
BEFORE INSERT OR UPDATE OR DELETE ON sessions
FOR EACH ROW EXECUTE FUNCTION cutline_guard_evidence_write();

CREATE TRIGGER tasks_open_attempt
BEFORE INSERT OR UPDATE OR DELETE ON tasks
FOR EACH ROW EXECUTE FUNCTION cutline_guard_evidence_write();

CREATE TRIGGER checkpoint_visits_open_attempt
BEFORE INSERT OR UPDATE OR DELETE ON checkpoint_visits
FOR EACH ROW EXECUTE FUNCTION cutline_guard_evidence_write();

CREATE TRIGGER cancellations_open_attempt
BEFORE INSERT OR UPDATE OR DELETE ON cancellations
FOR EACH ROW EXECUTE FUNCTION cutline_guard_evidence_write();

CREATE TRIGGER effects_open_attempt
BEFORE INSERT OR UPDATE OR DELETE ON effects
FOR EACH ROW EXECUTE FUNCTION cutline_guard_evidence_write();

CREATE TRIGGER resources_open_attempt
BEFORE INSERT OR UPDATE OR DELETE ON resources
FOR EACH ROW EXECUTE FUNCTION cutline_guard_evidence_write();

CREATE TRIGGER schedule_actions_open_attempt
BEFORE INSERT OR UPDATE OR DELETE ON schedule_actions
FOR EACH ROW EXECUTE FUNCTION cutline_guard_evidence_write();

CREATE INDEX events_entity_lookup
    ON events (run_id, attempt_id, entity_id, canonical_order);
CREATE INDEX events_type_lookup
    ON events (run_id, attempt_id, event_type, canonical_order);
CREATE INDEX causal_edges_from_lookup
    ON causal_edges (run_id, attempt_id, from_entity_id);
CREATE INDEX causal_edges_to_lookup
    ON causal_edges (run_id, attempt_id, to_entity_id);
