-- Approval Engine schema (Turso / libsql, SQLite dialect).
-- Applied on startup; safe to re-run.

-- Consuming applications (Spring Boot apps, asset management, etc).
-- callback_url, if set, is where the engine POSTs a WebhookEvent whenever a
-- request/step belonging to this app changes state (see service.WebhookNotifier).
CREATE TABLE IF NOT EXISTS applications (
    id           TEXT PRIMARY KEY,
    code         TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    api_key      TEXT NOT NULL UNIQUE,
    callback_url TEXT,
    is_active    INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

-- People who can request or approve. Flat table: superior_id is the only
-- hierarchy we keep, and it is what makes "atasan +1 / +2" work for every
-- division without division-specific code.
CREATE TABLE IF NOT EXISTS participants (
    user_id    TEXT PRIMARY KEY,          -- NIK, the canonical id across all apps
    name       TEXT NOT NULL,
    email      TEXT,
    position   TEXT,
    department TEXT,
    superior_id TEXT REFERENCES participants(user_id),
    is_active  INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_participants_superior ON participants(superior_id);
CREATE INDEX IF NOT EXISTS idx_participants_position ON participants(position, department);

-- Workflow blueprint, per application + document type, versioned so that
-- in-flight requests keep the rules they started with.
CREATE TABLE IF NOT EXISTS workflow_definitions (
    id         TEXT PRIMARY KEY,
    app_id     TEXT NOT NULL REFERENCES applications(id),
    doc_type   TEXT NOT NULL,
    name       TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    is_active  INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (app_id, doc_type, version)
);

-- Steps of a blueprint. resolver_rule and condition are small JSON objects,
-- deliberately not a DSL.
--
-- resolver_rule:
--   {"type":"superior","level":2}
--       N steps up the requester's superior_id chain.
--   {"type":"role","position":"GDH","department":"System Support"}
--       Explicit department = a cross-function hop (e.g. sales chain handing
--       over to the system support asset team). Omit department and pass
--       "scope":"same_department" to stay in the requester's own department.
--   {"type":"static","user_id":"NIK001"}
--   {"type":"field","path":"approver_nik"}
--       Read the approver straight out of the request payload, for apps that
--       have not registered their people in participants yet.
--
-- condition: {"field":"amount","op":"gt","value":50000000}   (null = always run)
--
-- conditions + condition_logic: the compound form, for a step that needs more
-- than one payload field to gate it (e.g. amount AND category) without
-- forking into multiple doc_types to fake an AND. A step sets either
-- condition or conditions, never both (enforced by service.ValidateStep).
--   conditions: [{"field":"amount","op":"gt","value":50000000},
--                {"field":"category","op":"eq","value":"barcode"}]
--   condition_logic: "all" (default) or "any"
--
-- on_empty: what to do when the rule resolves to nobody (chain too short, no
-- one holds that position). 'fail' parks the request for a human to look at;
-- 'skip' moves on. Default 'fail' so an approval is never silently dropped.
CREATE TABLE IF NOT EXISTS workflow_steps (
    id              TEXT PRIMARY KEY,
    definition_id   TEXT NOT NULL REFERENCES workflow_definitions(id),
    step_order      INTEGER NOT NULL,
    name            TEXT NOT NULL,
    resolver_rule   TEXT NOT NULL,
    condition       TEXT,
    conditions      TEXT,
    condition_logic TEXT,
    approval_mode   TEXT NOT NULL DEFAULT 'any',   -- any | all
    on_empty        TEXT NOT NULL DEFAULT 'fail',  -- fail | skip
    UNIQUE (definition_id, step_order)
);

-- A running approval request (instance of a definition).
CREATE TABLE IF NOT EXISTS approval_requests (
    id                 TEXT PRIMARY KEY,
    app_id             TEXT NOT NULL REFERENCES applications(id),
    definition_id      TEXT NOT NULL REFERENCES workflow_definitions(id),
    doc_type           TEXT NOT NULL,
    resource_id        TEXT NOT NULL,          -- the id in the consuming app
    requester_id       TEXT NOT NULL,
    payload            TEXT NOT NULL DEFAULT '{}',
    status             TEXT NOT NULL DEFAULT 'pending', -- pending|approved|rejected|cancelled
    current_step_order INTEGER NOT NULL DEFAULT 0,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at       TEXT,
    UNIQUE (app_id, doc_type, resource_id)      -- idempotency for the consuming app
);
CREATE INDEX IF NOT EXISTS idx_requests_status ON approval_requests(status);
CREATE INDEX IF NOT EXISTS idx_requests_requester ON approval_requests(requester_id);

-- Steps materialised for one request.
CREATE TABLE IF NOT EXISTS approval_steps (
    id            TEXT PRIMARY KEY,
    request_id    TEXT NOT NULL REFERENCES approval_requests(id),
    step_order    INTEGER NOT NULL,
    name          TEXT NOT NULL,
    approval_mode TEXT NOT NULL DEFAULT 'any',
    status        TEXT NOT NULL DEFAULT 'waiting', -- waiting|active|approved|rejected|skipped|resolution_failed
    activated_at  TEXT,
    completed_at  TEXT,
    UNIQUE (request_id, step_order)
);

-- WHO was asked, snapshotted at the moment the step became active. Name and
-- position are copied on purpose: if someone moves department later, history
-- must not silently change. This is also the table the approver inbox reads.
CREATE TABLE IF NOT EXISTS approval_assignments (
    id            TEXT PRIMARY KEY,
    step_id       TEXT NOT NULL REFERENCES approval_steps(id),
    request_id    TEXT NOT NULL REFERENCES approval_requests(id),
    user_id       TEXT NOT NULL,
    user_name     TEXT,
    user_position TEXT,
    status        TEXT NOT NULL DEFAULT 'pending',  -- pending|approved|rejected
    comment       TEXT,
    acted_at      TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_assignments_inbox ON approval_assignments(user_id, status);
CREATE INDEX IF NOT EXISTS idx_assignments_step ON approval_assignments(step_id);

-- Append-only audit log. Never updated, never deleted.
CREATE TABLE IF NOT EXISTS approval_events (
    id         TEXT PRIMARY KEY,
    request_id TEXT NOT NULL REFERENCES approval_requests(id),
    step_id    TEXT,
    actor_id   TEXT,
    event_type TEXT NOT NULL,          -- request_created|step_activated|approved|rejected|step_skipped|request_completed|resolution_failed
    detail     TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_events_request ON approval_events(request_id, created_at);
