-- Audit log, agent token metadata and database-backed sessions (spec
-- docs/vi/specs/2026-11-15-audit-tokens-sessions.md).

-- Append-only: rows are only inserted. actor_id NULL = the operator CLI.
CREATE TABLE audit_event (
  id uuid PRIMARY KEY,
  at timestamptz NOT NULL,
  actor_id uuid,
  action text NOT NULL,
  target_type text NOT NULL,
  target_id text NOT NULL,
  project_id text,
  detail jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_event_at ON audit_event (at DESC, id DESC);
CREATE INDEX audit_event_actor ON audit_event (actor_id, at DESC, id DESC);
CREATE INDEX audit_event_project ON audit_event (project_id, at DESC, id DESC);

-- Existing tokens keep an empty label and never expire.
ALTER TABLE credential
  ADD COLUMN label text NOT NULL DEFAULT '',
  ADD COLUMN expires_at timestamptz,
  ADD COLUMN last_used_at timestamptz;

-- Only the SHA-256 of the session id is stored, never the id itself.
CREATE TABLE session (
  id_hash text PRIMARY KEY,
  actor_id uuid NOT NULL REFERENCES actor(id),
  password_changed_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL
);
