-- Eval-only reads for the hybrid search arm.

-- name: ListTeamMemoryItems :many
-- Every item search_team_memory could return for one project, with the same
-- visibility rules as SearchTeamMemory minus the full-text match: masked
-- summaries of live team chats that are not excluded from memory, memos
-- whose source chat passes those checks, and digests. The hybrid arm embeds
-- these to search them by meaning.
SELECT items.kind::text AS kind, items.id, items.chat_id, items.body::text AS body,
       items.author_user_id::text AS author_user_id, items.level::text AS level, items.period_start,
       items.period_end, items.at::timestamptz AS at
FROM (
  SELECT 'chat' AS kind, c.id, NULL::uuid AS chat_id,
         c.summary AS body, '' AS author_user_id, '' AS level, NULL::date AS period_start, NULL::date AS period_end,
         COALESCE(c.summary_generated_at, c.updated_at) AS at
  FROM chats c
  JOIN projects p ON p.id = c.project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE
  LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
  WHERE c.project_id = @project_id AND c.organization_id = @organization_id
    AND c.deleted IS FALSE AND c.summary_redacted AND c.memory_excluded_at IS NULL
    AND c.summary IS NOT NULL AND octet_length(c.summary) <= 65536
    AND (ua.id IS NULL OR ua.account_type <> 'personal')
  UNION ALL
  SELECT 'memo', m.id, m.chat_id, m.content, COALESCE(m.author_user_id, ''), '', NULL::date, NULL::date,
         m.created_at
  FROM memory_memos m
  JOIN projects p ON p.id = m.project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE
  LEFT JOIN chats mc ON mc.id = m.chat_id
  LEFT JOIN user_accounts mua ON mua.id = mc.user_account_id
  WHERE m.project_id = @project_id AND m.organization_id = @organization_id AND m.deleted IS FALSE
    AND (m.chat_id IS NULL OR (mc.project_id = m.project_id AND mc.deleted IS FALSE AND mc.memory_excluded_at IS NULL
         AND (mua.id IS NULL OR mua.account_type <> 'personal')))
  UNION ALL
  SELECT 'digest', d.id, NULL::uuid, d.content, '', d.level, d.period_start, d.period_end,
         d.period_start::timestamp AT TIME ZONE 'UTC'
  FROM memory_digests d
  JOIN projects p ON p.id = d.project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE
  WHERE d.project_id = @project_id AND d.organization_id = @organization_id
    AND octet_length(d.content) <= 65536
) items
ORDER BY items.at, items.id;
