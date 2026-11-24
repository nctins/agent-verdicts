-- The author's own favorable signals no longer count (spec
-- docs/vi/specs/2026-11-21-ignore-self-support.md): recount every
-- contribution with the query recount (signal_repo.go) now uses. Signals are kept.
UPDATE contribution c SET favorable_n = n.f, unfavorable_n = n.u, neutral_n = n.z, favorable_actor_n = n.fa
  FROM (SELECT o.id,
          count(*) FILTER (WHERE s.stance = 'favorable' AND s.actor_id <> o.actor_id) AS f,
          count(DISTINCT s.actor_id) FILTER (WHERE s.stance = 'favorable' AND s.actor_id <> o.actor_id) AS fa,
          count(*) FILTER (WHERE s.stance = 'unfavorable') AS u,
          count(*) FILTER (WHERE s.stance = 'neutral') AS z
        FROM contribution o
        LEFT JOIN (signal s JOIN actor a ON a.id = s.actor_id AND a.disabled_at IS NULL)
          ON s.contribution_id = o.id AND s.superseded_at IS NULL AND s.withdrawn_at IS NULL AND s.hidden_at IS NULL
        GROUP BY o.id) n
  WHERE c.id = n.id
    AND (c.favorable_n, c.unfavorable_n, c.neutral_n, c.favorable_actor_n) IS DISTINCT FROM (n.f, n.u, n.z, n.fa);
