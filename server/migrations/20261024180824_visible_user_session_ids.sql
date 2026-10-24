-- Create "visible_user_session_ids" function
CREATE FUNCTION "visible_user_session_ids" ("caller_project_id" uuid, "caller_organization_id" text, "search_pattern" text, "agent_subject_urns" text[], "first_party_client_name" text) RETURNS TABLE ("user_session_id" uuid) LANGUAGE sql STABLE PARALLEL SAFE AS $$
SELECT s.id
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
LEFT JOIN user_session_clients AS c ON c.id = s.user_session_client_id
LEFT JOIN users AS u
  ON s.subject_urn::text LIKE 'user:%'
  AND u.id = split_part(s.subject_urn::text, ':', 2)
LEFT JOIN api_keys AS k
  ON k.id = CASE
             WHEN s.subject_urn::text LIKE 'apikey:%'
             THEN split_part(s.subject_urn::text, ':', 2)::uuid
           END
WHERE (iss.project_id = caller_project_id OR (iss.project_id IS NULL AND iss.organization_id = caller_organization_id))
  AND iss.deleted IS FALSE
  AND (
    search_pattern IS NULL
    OR s.subject_urn::text = ANY(agent_subject_urns)
    OR (CASE
          WHEN s.subject_urn::text LIKE 'user:%' THEN COALESCE(NULLIF(u.display_name, ''), NULLIF(u.email, ''), s.subject_urn::text)
          WHEN s.subject_urn::text LIKE 'apikey:%' THEN COALESCE(NULLIF(k.name, ''), 'API key')
          WHEN s.subject_urn::text LIKE 'anonymous%' THEN 'Anonymous client'
          WHEN s.subject_urn::text LIKE 'workload:%' THEN substr(s.subject_urn::text, 47)
          ELSE s.subject_urn::text
        END) ILIKE search_pattern
    OR COALESCE(c.client_name, CASE WHEN s.refresh_token_hash LIKE 'dashboard-mint:%' THEN first_party_client_name END) ILIKE search_pattern
    OR iss.slug ILIKE search_pattern
    OR (
      s.subject_urn::text LIKE 'workload:%'
      AND (
        EXISTS (
          SELECT 1
          FROM workload_issuers AS wi
          WHERE wi.id = substr(s.subject_urn::text, 10, 36)::uuid
            AND wi.organization_id = caller_organization_id
            AND (wi.project_id = caller_project_id OR wi.project_id IS NULL)
            AND wi.deleted IS FALSE
            AND (wi.name ILIKE search_pattern OR wi.issuer ILIKE search_pattern)
        )
        OR EXISTS (
          SELECT 1
          FROM workload_issuers AS live
          JOIN workload_agent_assignments AS wa
            ON wa.organization_id = live.organization_id
            AND wa.workload_issuer_id = live.id
            AND wa.deleted IS FALSE
          JOIN agents AS a
            ON a.organization_id = wa.organization_id
            AND a.id = wa.agent_id
            AND a.deleted IS FALSE
          WHERE live.id = substr(s.subject_urn::text, 10, 36)::uuid
            AND live.organization_id = caller_organization_id
            AND live.deleted IS FALSE
            AND wa.subject = substr(s.subject_urn::text, 47)
            AND a.name ILIKE search_pattern
        )
      )
    )
    OR (
      s.subject_urn::text NOT LIKE 'agent:%'
      AND EXISTS (
        SELECT 1
        FROM remote_sessions AS rs
        JOIN remote_session_clients AS rc ON rc.id = rs.remote_session_client_id
        JOIN remote_session_issuers AS ri ON ri.id = rc.remote_session_issuer_id
        WHERE ri.slug ILIKE search_pattern
          AND rs.subject_urn = s.subject_urn::text
          AND (rc.project_id = caller_project_id OR (rc.project_id IS NULL AND (rc.organization_id IS NULL OR rc.organization_id = caller_organization_id)))
          AND (
            EXISTS (
              SELECT 1
              FROM remote_session_client_user_session_issuers AS blink
              WHERE blink.remote_session_client_id = rs.remote_session_client_id
                AND blink.user_session_issuer_id = s.user_session_issuer_id
            )
            OR rs.user_session_issuer_id = s.user_session_issuer_id
          )
          AND rs.deleted IS FALSE
          AND rc.deleted IS FALSE
          AND ri.deleted IS FALSE
      )
    )
    OR (
      s.subject_urn::text LIKE 'agent:%'
      AND EXISTS (
        SELECT 1
        FROM remote_sessions AS rs
        JOIN remote_session_clients AS rc ON rc.id = rs.remote_session_client_id
        JOIN remote_session_issuers AS ri ON ri.id = rc.remote_session_issuer_id
        WHERE ri.slug ILIKE search_pattern
          AND EXISTS (
            SELECT 1
            FROM principal_remote_session_bindings AS b
            JOIN agents AS a ON a.id = b.principal_id AND a.organization_id = b.organization_id
              AND a.deleted IS FALSE AND a.revoked_at IS NULL AND a.suspended_at IS NULL
              AND a.owner_reassignment_required_at IS NULL
            JOIN users AS owner ON owner.id = a.owner_user_id AND owner.deleted_at IS NULL
            JOIN organization_user_relationships AS membership ON membership.organization_id = a.organization_id
              AND membership.user_id = a.owner_user_id AND membership.deleted_at IS NULL
            JOIN remote_sessions AS bs ON bs.id = b.remote_session_id
              AND bs.subject_urn = 'user:' || a.owner_user_id
              AND b.attached_by_subject_id = bs.subject_urn
              AND bs.grant_generation = b.grant_generation
              AND bs.remote_session_client_id = b.remote_session_client_id
            JOIN remote_session_clients AS bc ON bc.id = bs.remote_session_client_id
            JOIN user_session_issuers AS target ON target.id = b.user_session_issuer_id
            JOIN user_session_issuers AS provenance ON provenance.id = bs.user_session_issuer_id
            JOIN projects AS p ON p.id = b.project_id AND p.organization_id = b.organization_id AND p.deleted IS FALSE
            JOIN remote_session_client_user_session_issuers AS link ON link.remote_session_client_id = bc.id AND link.user_session_issuer_id = target.id
            JOIN remote_session_issuers AS issuer ON issuer.id = bc.remote_session_issuer_id
            WHERE b.project_id = caller_project_id
              AND b.organization_id = caller_organization_id
              AND 'agent:' || b.principal_id::text = s.subject_urn::text
              AND b.user_session_issuer_id = s.user_session_issuer_id
              AND b.remote_session_client_id = rs.remote_session_client_id
              AND b.revoked_at IS NULL
              AND bs.deleted IS FALSE
              AND bc.deleted IS FALSE
              AND target.deleted IS FALSE
              AND provenance.deleted IS FALSE
              AND issuer.deleted IS FALSE
              AND (issuer.project_id = p.id OR (issuer.project_id IS NULL AND (issuer.organization_id IS NULL OR issuer.organization_id = p.organization_id)))
              AND (provenance.project_id = caller_project_id OR (provenance.project_id IS NULL AND provenance.organization_id = caller_organization_id))
              AND (target.project_id = caller_project_id OR (target.project_id IS NULL AND target.organization_id = caller_organization_id))
              AND (bc.project_id = caller_project_id OR (bc.project_id IS NULL AND (bc.organization_id IS NULL OR bc.organization_id = caller_organization_id)))
              AND bs.id = rs.id
          )
          AND (rc.project_id = caller_project_id OR (rc.project_id IS NULL AND (rc.organization_id IS NULL OR rc.organization_id = caller_organization_id)))
          AND (
            EXISTS (
              SELECT 1
              FROM remote_session_client_user_session_issuers AS blink
              WHERE blink.remote_session_client_id = rs.remote_session_client_id
                AND blink.user_session_issuer_id = s.user_session_issuer_id
            )
            OR rs.user_session_issuer_id = s.user_session_issuer_id
          )
          AND rs.deleted IS FALSE
          AND rc.deleted IS FALSE
          AND ri.deleted IS FALSE
      )
    )
  )
$$;
