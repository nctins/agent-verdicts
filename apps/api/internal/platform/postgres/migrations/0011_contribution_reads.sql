-- Agent read counts (spec docs/vi/specs/2026-11-21-claim-read-counts.md §4):
-- per-actor daily detail kept read_detail_days days (per project), then
-- rolled up into monthly totals kept forever.
ALTER TABLE project
  ADD COLUMN read_detail_days int NOT NULL DEFAULT 180
  CHECK (read_detail_days BETWEEN 30 AND 3650);

CREATE TABLE contribution_read_day (
  project_id      text     NOT NULL REFERENCES project(id),
  contribution_id uuid     NOT NULL REFERENCES contribution(id),
  day             date     NOT NULL,           -- UTC
  kind            smallint NOT NULL CHECK (kind IN (1, 2, 3)), -- 1 observe, 2 reference, 3 search_hit
  actor_id        uuid     NOT NULL,           -- search_hit: the nil uuid (no actor kept)
  n               integer  NOT NULL,
  PRIMARY KEY (contribution_id, day, kind, actor_id)
);
CREATE INDEX ON contribution_read_day (project_id, day);

CREATE TABLE contribution_read_month (
  contribution_id uuid     NOT NULL REFERENCES contribution(id),
  month           date     NOT NULL,           -- first day of the month
  kind            smallint NOT NULL CHECK (kind IN (1, 2, 3)),
  reads           integer  NOT NULL,
  readers         integer  NOT NULL,           -- distinct actors that month; 0 for search_hit
  PRIMARY KEY (contribution_id, month, kind)
);
