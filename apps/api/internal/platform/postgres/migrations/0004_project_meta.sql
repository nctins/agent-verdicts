-- Project metadata and slug ids (spec docs/vi/specs/2026-11-15-mcp-project-scope.md §2).
ALTER TABLE project ADD COLUMN name text, ADD COLUMN description text NOT NULL DEFAULT '';
UPDATE project SET name = id;
ALTER TABLE project ALTER COLUMN name SET NOT NULL;
ALTER TABLE project
  ADD CONSTRAINT project_name_length CHECK (char_length(name) BETWEEN 1 AND 100),
  ADD CONSTRAINT project_description_length CHECK (char_length(description) <= 500);

-- Re-slug ids that are not slugs (decision D7): the old id stays as the name.
-- No ON UPDATE CASCADE on the foreign keys, so insert the new row, repoint
-- the children, then delete the old row.
DO $$
DECLARE
  r record;
  base text;
  cand text;
  n int;
BEGIN
  FOR r IN SELECT * FROM project WHERE id !~ '^[a-z0-9][a-z0-9_-]{0,63}$' ORDER BY created_at, id LOOP
    base := lower(r.id);
    base := regexp_replace(base, '[^a-z0-9_-]+', '-', 'g');
    -- A slug starts with [a-z0-9], so leading '_' goes too, not only '-'.
    base := regexp_replace(regexp_replace(base, '^[-_]+', ''), '-+$', '');
    base := regexp_replace(left(base, 64), '-+$', '');
    IF base = '' THEN base := 'project'; END IF;
    cand := base;
    n := 1;
    WHILE EXISTS (SELECT 1 FROM project WHERE id = cand) LOOP
      n := n + 1;
      cand := regexp_replace(left(base, 64 - length('-' || n)), '-+$', '') || '-' || n;
    END LOOP;
    INSERT INTO project (id, name, description, created_at) VALUES (cand, r.name, r.description, r.created_at);
    UPDATE contribution SET project_id = cand WHERE project_id = r.id;
    UPDATE role_assignment SET project_id = cand WHERE project_id = r.id;
    DELETE FROM project WHERE id = r.id;
  END LOOP;
END $$;

ALTER TABLE project ADD CONSTRAINT project_id_slug CHECK (id ~ '^[a-z0-9][a-z0-9_-]{0,63}$');
