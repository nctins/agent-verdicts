-- Retract, supersede, moderate, archive (spec docs/vi/specs/2026-11-15-retract-supersede-moderate.md).
-- A view's column list is frozen at creation, so contribution_v is recreated.
DROP VIEW contribution_v;

ALTER TABLE signal
  ADD COLUMN withdrawn_at timestamptz,
  ADD COLUMN hidden_at timestamptz,
  ADD COLUMN hidden_by uuid REFERENCES actor(id);
ALTER TABLE contribution
  ADD COLUMN superseded_by uuid REFERENCES contribution(id),
  ADD COLUMN superseded_at timestamptz,
  ADD COLUMN hidden_at timestamptz,
  ADD COLUMN hidden_by uuid REFERENCES actor(id);
ALTER TABLE project ADD COLUMN archived_at timestamptz;

-- A withdrawn assessment no longer occupies the actor's one active slot.
DROP INDEX signal_one_active_assessment;
CREATE UNIQUE INDEX signal_one_active_assessment ON signal (contribution_id, actor_id)
  WHERE kind = 'assessment' AND superseded_at IS NULL AND withdrawn_at IS NULL;

UPDATE policy SET actions = array_append(actions, 'content:Moderate')
  WHERE id = 'ProjectManagerAccess' AND NOT ('content:Moderate' = ANY(actions));

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
                      WHERE a.answers_id = c.id AND a.favorable_n > 0 AND a.unfavorable_n = 0
                        AND a.superseded_by IS NULL AND a.hidden_at IS NULL) THEN 'resolved'
         WHEN EXISTS (SELECT 1 FROM contribution a WHERE a.answers_id = c.id AND a.hidden_at IS NULL) THEN 'answered'
         ELSE 'open' END AS question_status
  FROM contribution c
) v;
