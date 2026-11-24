-- Trust threshold and sources (spec docs/vi/specs/2026-11-15-trust-threshold-sources.md).
-- The state table below is mirrored by trust.StateOf; state_cases.json guards both.
DROP VIEW contribution_v;

ALTER TABLE project
  ADD COLUMN min_favorable int NOT NULL DEFAULT 1 CHECK (min_favorable BETWEEN 1 AND 10);
ALTER TABLE contribution
  ADD COLUMN favorable_actor_n int NOT NULL DEFAULT 0,
  ADD COLUMN sources text[] NOT NULL DEFAULT '{}';
ALTER TABLE signal ADD COLUMN sources text[] NOT NULL DEFAULT '{}';

UPDATE contribution c SET favorable_actor_n = n.k
  FROM (SELECT s.contribution_id, count(DISTINCT s.actor_id) AS k
        FROM signal s JOIN actor a ON a.id = s.actor_id
        WHERE s.stance = 'favorable' AND s.superseded_at IS NULL AND s.withdrawn_at IS NULL
          AND s.hidden_at IS NULL AND a.disabled_at IS NULL
        GROUP BY s.contribution_id) n
  WHERE c.id = n.contribution_id;

CREATE VIEW contribution_v AS
SELECT v.*,
  CASE v.state WHEN 'Favorable' THEN 0 WHEN 'Conflict' THEN 1
               WHEN 'InsufficientEvidence' THEN 2 WHEN 'Unfavorable' THEN 3 END AS rank
FROM (
  SELECT c.*,
    p.min_favorable,
    c.favorable_n + c.unfavorable_n + c.neutral_n AS signal_n,
    CASE WHEN c.kind = 'question' THEN NULL
         WHEN c.favorable_n > 0 AND c.unfavorable_n > 0 THEN 'Conflict'
         WHEN c.unfavorable_n = 0 AND c.favorable_actor_n >= p.min_favorable THEN 'Favorable'
         WHEN c.favorable_n = 0 AND c.unfavorable_n > 0 THEN 'Unfavorable'
         ELSE 'InsufficientEvidence' END AS state,
    CASE WHEN c.kind <> 'question' THEN NULL
         WHEN EXISTS (SELECT 1 FROM contribution a
                      WHERE a.answers_id = c.id AND a.unfavorable_n = 0 AND a.favorable_actor_n >= p.min_favorable
                        AND a.superseded_by IS NULL AND a.hidden_at IS NULL) THEN 'resolved'
         WHEN EXISTS (SELECT 1 FROM contribution a WHERE a.answers_id = c.id AND a.hidden_at IS NULL) THEN 'answered'
         ELSE 'open' END AS question_status
  FROM contribution c JOIN project p ON p.id = c.project_id
) v;
