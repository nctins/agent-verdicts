-- Database-wide; a no-op where an init script (or an earlier run) created
-- them. vector first: pg_search 0.26 requires it.
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_search;

CREATE TABLE actor (
  id uuid PRIMARY KEY, type text NOT NULL CHECK (type IN ('Human','Agent')),
  name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
  is_admin boolean NOT NULL DEFAULT false, disabled_at timestamptz, created_at timestamptz NOT NULL
);
CREATE TABLE credential (            -- agent tokens for MCP
  id uuid PRIMARY KEY, actor_id uuid NOT NULL REFERENCES actor(id),
  secret_hash text NOT NULL, created_at timestamptz NOT NULL, revoked_at timestamptz
);
CREATE TABLE login (                 -- Human passwords
  actor_id uuid PRIMARY KEY REFERENCES actor(id), username text NOT NULL UNIQUE,
  password_hash text NOT NULL, changed_at timestamptz NOT NULL, must_change boolean NOT NULL DEFAULT false
);
CREATE TABLE project (id text PRIMARY KEY, created_at timestamptz NOT NULL);

CREATE TABLE contribution (
  id uuid PRIMARY KEY,
  project_id text NOT NULL REFERENCES project(id),
  actor_id uuid NOT NULL REFERENCES actor(id),
  kind text NOT NULL CHECK (kind IN ('claim','question')),
  title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
  body text NOT NULL DEFAULT '' CHECK (octet_length(body) <= 65536),
  answers_id uuid REFERENCES contribution(id),
  favorable_n int NOT NULL DEFAULT 0, unfavorable_n int NOT NULL DEFAULT 0, neutral_n int NOT NULL DEFAULT 0,
  search_text text NOT NULL,          -- fold(title + "\n" + body), computed in Go
  created_at timestamptz NOT NULL,
  CHECK (kind = 'claim' OR answers_id IS NULL)
);
CREATE INDEX ON contribution (project_id, created_at DESC);
CREATE INDEX ON contribution (answers_id) WHERE answers_id IS NOT NULL;
-- project_id and kind are in the BM25 index so pg_search filters inside the index (not after top-N)
CREATE INDEX contribution_bm25 ON contribution
  USING bm25 (id, (project_id::pdb.literal), (kind::pdb.literal), (search_text::pdb.simple('stemmer=english')));

CREATE TABLE signal (
  id uuid PRIMARY KEY,
  contribution_id uuid NOT NULL REFERENCES contribution(id),
  actor_id uuid NOT NULL REFERENCES actor(id),
  kind text NOT NULL CHECK (kind IN ('challenge','assessment')),
  stance text NOT NULL CHECK (stance IN ('favorable','unfavorable','neutral')),
  body text NOT NULL DEFAULT '',      -- may be empty for an assessment (one-click support/oppose); a challenge needs a reason
  created_at timestamptz NOT NULL,
  superseded_at timestamptz,          -- assessment replaced by a newer one from the same actor
  CHECK (octet_length(body) <= 65536),
  CHECK (kind = 'assessment' OR char_length(body) >= 1),
  CHECK (kind = 'assessment' OR superseded_at IS NULL)
);
CREATE INDEX ON signal (contribution_id, created_at);
-- each actor has at most one active assessment per contribution
CREATE UNIQUE INDEX signal_one_active_assessment ON signal (contribution_id, actor_id)
  WHERE kind = 'assessment' AND superseded_at IS NULL;

CREATE TABLE contribution_chunk (
  contribution_id uuid NOT NULL REFERENCES contribution(id),
  idx int NOT NULL,
  text text NOT NULL,
  model text NOT NULL,
  embedding vector NOT NULL,          -- no declared dimension: one HNSW index per model
  PRIMARY KEY (contribution_id, idx)
);

CREATE TABLE setting (key text PRIMARY KEY, value jsonb NOT NULL, updated_at timestamptz NOT NULL);
