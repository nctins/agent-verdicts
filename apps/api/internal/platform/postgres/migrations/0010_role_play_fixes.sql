-- Role-play fixes (spec docs/vi/specs/2026-11-15-role-play-fixes.md §A.5):
-- why a moderator hid a contribution or a signal; cleared on unhide.
-- A view's column list is frozen at creation, so contribution_v is recreated
-- (same definition as 0007).
DROP VIEW contribution_v;

ALTER TABLE contribution ADD COLUMN hidden_reason text;
ALTER TABLE signal ADD COLUMN hidden_reason text;

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
