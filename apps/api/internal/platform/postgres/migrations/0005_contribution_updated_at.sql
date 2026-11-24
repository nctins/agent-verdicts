-- Last activity of a contribution (spec docs/vi/specs/2026-11-15-mcp-parity.md §3.1):
-- its creation, or its newest signal. A view's column list is frozen at
-- creation, so contribution_v is recreated to expose the new column.
DROP VIEW contribution_v;
ALTER TABLE contribution ADD COLUMN updated_at timestamptz;
UPDATE contribution c SET updated_at = greatest(c.created_at,
  (SELECT max(s.created_at) FROM signal s WHERE s.contribution_id = c.id));
ALTER TABLE contribution ALTER COLUMN updated_at SET NOT NULL;
CREATE INDEX ON contribution (project_id, updated_at DESC);
CREATE VIEW contribution_v AS
SELECT v.*,
  CASE v.state WHEN 'Favorable' THEN 0 WHEN 'Conflict' THEN 1
               WHEN 'InsufficientEvidence' THEN 2 WHEN 'Unfavorable' THEN 3 END AS rank
FROM (
  SELECT c.*,
    c.favorable_n + c.unfavorable_n + c.neutral_n AS signal_n,
    CASE WHEN c.kind = 'question' THEN NULL
         WHEN c.favorable_n > 0 AND c.unfavorable_n > 0 THEN 'Conflict'
         WHEN c.favorable_n > 0 THEN 'Favorable'
         WHEN c.unfavorable_n > 0 THEN 'Unfavorable'
         ELSE 'InsufficientEvidence' END AS state,
    CASE WHEN c.kind <> 'question' THEN NULL
         WHEN EXISTS (SELECT 1 FROM contribution a
                      WHERE a.answers_id = c.id AND a.favorable_n > 0 AND a.unfavorable_n = 0) THEN 'resolved'
         WHEN EXISTS (SELECT 1 FROM contribution a WHERE a.answers_id = c.id) THEN 'answered'
         ELSE 'open' END AS question_status
  FROM contribution c
) v;
