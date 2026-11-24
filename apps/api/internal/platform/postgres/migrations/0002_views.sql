-- contribution_v: contribution plus derived values shared by List, Answers and search.
--   signal_n        total signals (sort key for debated/least)
--   state           trust state (= trust.StateOf); NULL for questions
--   rank            state order, lower is better (= trust.Rank); NULL for questions
--   question_status open/answered/resolved; NULL for claims
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
