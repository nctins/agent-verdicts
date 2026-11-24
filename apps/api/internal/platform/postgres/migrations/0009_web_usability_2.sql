-- Web usability 2 (spec docs/vi/specs/2026-11-15-web-usability-2.md).

-- A reply is a challenge that answers one signal of the same claim (one level).
ALTER TABLE signal ADD COLUMN reply_to uuid REFERENCES signal(id);
CREATE INDEX signal_reply_to ON signal (reply_to) WHERE reply_to IS NOT NULL;

-- Actor names become unique, ignoring case. Later duplicates (by created_at,
-- id) get " (2)", " (3)"... appended, the base truncated to stay within 64
-- characters. A new name can itself collide with an existing one, so repeat
-- until none is left.
DO $$
BEGIN
  WHILE EXISTS (SELECT 1 FROM actor GROUP BY lower(name) HAVING count(*) > 1) LOOP
    UPDATE actor a SET name = left(d.name, 64 - length(' (' || d.n || ')')) || ' (' || d.n || ')'
    FROM (SELECT id, name, row_number() OVER (PARTITION BY lower(name) ORDER BY created_at, id) AS n FROM actor) d
    WHERE a.id = d.id AND d.n > 1;
  END LOOP;
END $$;
CREATE UNIQUE INDEX actor_name_unique ON actor (lower(name));
