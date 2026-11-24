-- Access control (spec docs/vi/specs/2026-11-14-access-control.md):
-- policy = set of actions, role = set of policies, assignment = actor +
-- role + scope (project_id NULL = every project). Replaces actor.is_admin.
CREATE TABLE policy (
  id text PRIMARY KEY, name text NOT NULL UNIQUE, managed boolean NOT NULL,
  actions text[] NOT NULL, created_at timestamptz NOT NULL
);
CREATE TABLE role (
  id text PRIMARY KEY, name text NOT NULL UNIQUE, managed boolean NOT NULL, created_at timestamptz NOT NULL
);
CREATE TABLE role_policy (
  role_id text NOT NULL REFERENCES role(id) ON DELETE CASCADE,
  policy_id text NOT NULL REFERENCES policy(id),
  PRIMARY KEY (role_id, policy_id)
);
CREATE TABLE role_assignment (
  id uuid PRIMARY KEY,
  actor_id uuid NOT NULL REFERENCES actor(id),
  role_id text NOT NULL REFERENCES role(id),
  project_id text REFERENCES project(id),
  created_by uuid REFERENCES actor(id),
  created_at timestamptz NOT NULL,
  UNIQUE NULLS NOT DISTINCT (actor_id, role_id, project_id)
);
CREATE INDEX ON role_assignment (actor_id);
CREATE INDEX ON role_assignment (project_id);

INSERT INTO policy VALUES
  ('ReadOnlyAccess', 'ReadOnlyAccess', true, '{contribution:Read}', now()),
  ('ContributorAccess', 'ContributorAccess', true, '{contribution:Read,contribution:Create,signal:Create}', now()),
  ('ProjectManagerAccess', 'ProjectManagerAccess', true, '{contribution:Read,contribution:Create,signal:Create,member:Manage}', now()),
  ('AdministratorAccess', 'AdministratorAccess', true, '{*}', now());
INSERT INTO role VALUES
  ('viewer', 'viewer', true, now()), ('contributor', 'contributor', true, now()),
  ('manager', 'manager', true, now()), ('admin', 'admin', true, now());
INSERT INTO role_policy VALUES
  ('viewer', 'ReadOnlyAccess'), ('contributor', 'ContributorAccess'),
  ('manager', 'ProjectManagerAccess'), ('admin', 'AdministratorAccess');

-- Former admins keep admin on every project; everyone else starts with no role (decision D8).
INSERT INTO role_assignment (id, actor_id, role_id, project_id, created_by, created_at)
  SELECT gen_random_uuid(), id, 'admin', NULL, NULL, now() FROM actor WHERE is_admin;
ALTER TABLE actor DROP COLUMN is_admin;
