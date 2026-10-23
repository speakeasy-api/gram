-- name: ListUpstreamClientBackfillCandidates :many
-- One row per live MCP server carrying a user session issuer and each client
-- bound to that issuer (client columns NULL when none is bound), for servers
-- after the cursor. Read-only: the runner decides from this evidence alone.
--
-- issuer_server_count counts the organization's live servers on the issuer
-- across its projects; issuer_has_gateway reports one of its gateways
-- fronting it. Either makes the binding
-- shared, so it no longer proves which server it was meant for.
--
-- metadata_match reports that the server's own upstream URL names a protected
-- resource whose RFC 9728 authorization_servers list this client's issuer.
-- Resource identifiers match exactly, as the serve-time lookup does.
--
-- grant_match and derived_match are the evidence the gateway candidates
-- carry, for the server's own issuer: the client holds a live grant, minted
-- in the server's organization, for the server's remote upstream URL, or the
-- client's authorization server is the one derived for the server.
--
-- pair_live is true when the pair already has a live association.
-- pair_recorded is true when an association row exists for the pair, live or
-- removed: a removed row on a shared issuer is an explicit detach the backfill
-- must not undo.
WITH page AS (
    SELECT s.id,
           s.project_id,
           p.organization_id,
           s.user_session_issuer_id,
           s.remote_mcp_server_id,
           s.toolset_id,
           s.remote_session_issuer_id AS derived_issuer_id,
           rms.url AS remote_url,
           EXISTS (
             SELECT 1
             FROM tunneled_mcp_servers AS tms
             WHERE tms.id = s.tunneled_mcp_server_id
               AND tms.project_id = s.project_id
               AND tms.deleted IS FALSE
           ) AS tunneled_live
    FROM mcp_servers AS s
    JOIN projects AS p
      ON p.id = s.project_id
     AND p.deleted IS FALSE
    -- A server's issuer reference is not tenant-checked by its foreign key,
    -- so only a live issuer of the server's own project or organization
    -- counts.
    JOIN user_session_issuers AS usi
      ON usi.id = s.user_session_issuer_id
     AND usi.deleted IS FALSE
     AND (usi.project_id = p.id
          OR (usi.project_id IS NULL AND usi.organization_id = p.organization_id))
    LEFT JOIN remote_mcp_servers AS rms
      ON rms.id = s.remote_mcp_server_id
     AND rms.project_id = s.project_id
     AND rms.deleted IS FALSE
    WHERE s.deleted IS FALSE
      AND s.id > @cursor::uuid
      AND (@organization_id::text = '' OR p.organization_id = @organization_id::text)
    ORDER BY s.id
    LIMIT @page_size::int
)
SELECT page.id AS mcp_server_id,
       page.project_id,
       page.organization_id,
       page.user_session_issuer_id::uuid AS user_session_issuer_id,
       (page.remote_mcp_server_id IS NOT NULL)::boolean AS remote_backed,
       (page.toolset_id IS NOT NULL)::boolean AS hosted,
       (page.remote_url IS NOT NULL OR page.tunneled_live)::boolean AS proxied,
       (
         SELECT count(*)
         FROM mcp_servers AS other
         JOIN projects AS other_project
           ON other_project.id = other.project_id
          AND other_project.organization_id = page.organization_id
          AND other_project.deleted IS FALSE
         WHERE other.user_session_issuer_id = page.user_session_issuer_id
           AND other.deleted IS FALSE
       )::bigint AS issuer_server_count,
       EXISTS (
         SELECT 1
         FROM meta_mcp_servers AS gateway
         JOIN projects AS gateway_project
           ON gateway_project.id = gateway.project_id
          AND gateway_project.deleted IS FALSE
         WHERE gateway.user_session_issuer_id = page.user_session_issuer_id
           AND gateway.organization_id = page.organization_id
           AND gateway.deleted IS FALSE
       )::boolean AS issuer_has_gateway,
       (
         SELECT count(*)
         FROM mcp_server_remote_session_clients AS live
         WHERE live.mcp_server_id = page.id
           AND live.deleted IS FALSE
       )::bigint AS live_associations,
       c.id AS remote_session_client_id,
       COALESCE((
         SELECT i.issuer = ANY (rpr.authorization_servers)
         FROM remote_session_issuers AS i
         JOIN remote_protected_resources AS rpr
           ON rpr.project_id = page.project_id
          AND rpr.resource_identifier = page.remote_url
          AND rpr.deleted IS FALSE
         WHERE i.id = c.remote_session_issuer_id
           AND i.deleted IS FALSE
           AND (i.project_id = page.project_id
                OR (i.project_id IS NULL AND i.organization_id = page.organization_id)
                OR (i.project_id IS NULL AND i.organization_id IS NULL))
         LIMIT 1
       ), false)::boolean AS metadata_match,
       (c.id IS NOT NULL AND page.remote_url IS NOT NULL AND EXISTS (
         SELECT 1
         FROM remote_sessions AS rs
         WHERE rs.remote_session_client_id = c.id
           AND rs.deleted IS FALSE
           AND rtrim(rs.resource, '/') = rtrim(page.remote_url, '/')
           AND EXISTS (
             SELECT 1
             FROM user_session_issuers AS minted
             LEFT JOIN projects AS minted_project ON minted_project.id = minted.project_id
             WHERE minted.id = rs.user_session_issuer_id
               AND COALESCE(minted.organization_id, minted_project.organization_id) = page.organization_id
           )
       ))::boolean AS grant_match,
       (c.id IS NOT NULL AND EXISTS (
         SELECT 1
         FROM remote_session_issuers AS derived
         WHERE derived.id = page.derived_issuer_id
           AND derived.id = c.remote_session_issuer_id
           AND derived.deleted IS FALSE
           AND (derived.project_id = page.project_id
                OR (derived.project_id IS NULL AND derived.organization_id = page.organization_id)
                OR (derived.project_id IS NULL AND derived.organization_id IS NULL))
       ))::boolean AS derived_match,
       (c.id IS NOT NULL AND EXISTS (
         SELECT 1
         FROM mcp_server_remote_session_clients AS recorded
         WHERE recorded.mcp_server_id = page.id
           AND recorded.remote_session_client_id = c.id
           AND recorded.deleted IS FALSE
       ))::boolean AS pair_live,
       (c.id IS NOT NULL AND EXISTS (
         SELECT 1
         FROM mcp_server_remote_session_clients AS recorded
         WHERE recorded.mcp_server_id = page.id
           AND recorded.remote_session_client_id = c.id
       ))::boolean AS pair_recorded
FROM page
LEFT JOIN remote_session_client_user_session_issuers AS link
  ON link.user_session_issuer_id = page.user_session_issuer_id
LEFT JOIN remote_session_clients AS c
  ON c.id = link.remote_session_client_id
 AND c.deleted IS FALSE
 AND (c.project_id = page.project_id
      OR (c.project_id IS NULL AND c.organization_id = page.organization_id)
      OR (c.project_id IS NULL AND c.organization_id IS NULL))
ORDER BY page.id, c.id;

-- name: BackfillUpstreamClientAssociation :execrows
-- Writes one decided association, conditional on the evidence still holding:
-- the server is live on the same tenant issuer, the client is still bound to
-- it and in the server's tenant, with require_sole_consumer the server is
-- still the issuer's only consumer and no gateway fronts it, and with
-- respect_removed no row was recorded for the pair meanwhile, and with
-- require_metadata_match the server's upstream still names this client's
-- authorization server and no other client bound to the issuer, and with
-- require_evidence_match the server still has no live association and the
-- issuer's clients' grants and derived authorization servers still decide
-- this client the way Decide does. A concurrent configuration change
-- therefore wins and the write is skipped.
WITH target AS (
    SELECT s.id, s.project_id, p.organization_id, s.remote_session_issuer_id, r.url AS remote_url
    FROM mcp_servers AS s
    JOIN projects AS p
      ON p.id = s.project_id
    LEFT JOIN remote_mcp_servers AS r
      ON r.id = s.remote_mcp_server_id
     AND r.project_id = s.project_id
     AND r.deleted IS FALSE
    WHERE s.id = @mcp_server_id::uuid
),
evidence AS (
    SELECT ec.id AS client_id,
           (target.remote_url IS NOT NULL AND EXISTS (
             SELECT 1
             FROM remote_sessions AS rs
             WHERE rs.remote_session_client_id = ec.id
               AND rs.deleted IS FALSE
               AND rtrim(rs.resource, '/') = rtrim(target.remote_url, '/')
               AND EXISTS (
                 SELECT 1
                 FROM user_session_issuers AS minted
                 LEFT JOIN projects AS minted_project ON minted_project.id = minted.project_id
                 WHERE minted.id = rs.user_session_issuer_id
                   AND COALESCE(minted.organization_id, minted_project.organization_id) = target.organization_id
               )
           )) AS grant_match,
           EXISTS (
             SELECT 1
             FROM remote_session_issuers AS derived
             WHERE derived.id = target.remote_session_issuer_id
               AND derived.id = ec.remote_session_issuer_id
               AND derived.deleted IS FALSE
               AND (derived.project_id = target.project_id
                    OR (derived.project_id IS NULL AND derived.organization_id = target.organization_id)
                    OR (derived.project_id IS NULL AND derived.organization_id IS NULL))
           ) AS derived_match
    FROM target
    JOIN remote_session_client_user_session_issuers AS el
      ON el.user_session_issuer_id = @user_session_issuer_id::uuid
    JOIN remote_session_clients AS ec
      ON ec.id = el.remote_session_client_id
     AND ec.deleted IS FALSE
     AND (ec.project_id = target.project_id
          OR (ec.project_id IS NULL AND ec.organization_id = target.organization_id)
          OR (ec.project_id IS NULL AND ec.organization_id IS NULL))
),
decided AS (
    SELECT e.client_id
    FROM evidence AS e
    WHERE (
        e.grant_match
        AND (SELECT count(*) FROM evidence AS g WHERE g.grant_match) = 1
        AND NOT EXISTS (SELECT 1 FROM evidence AS o WHERE o.derived_match AND o.client_id <> e.client_id)
      )
      OR (
        e.derived_match
        AND NOT EXISTS (SELECT 1 FROM evidence AS g WHERE g.grant_match)
        AND (SELECT count(*) FROM evidence AS d WHERE d.derived_match) = 1
      )
)
INSERT INTO mcp_server_remote_session_clients (project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope)
SELECT s.project_id, p.organization_id, s.id, c.id, c.attachment_scope
FROM mcp_servers AS s
JOIN projects AS p
  ON p.id = s.project_id
 AND p.deleted IS FALSE
JOIN user_session_issuers AS usi
  ON usi.id = s.user_session_issuer_id
 AND usi.deleted IS FALSE
 AND (usi.project_id = p.id
      OR (usi.project_id IS NULL AND usi.organization_id = p.organization_id))
JOIN remote_session_client_user_session_issuers AS link
  ON link.user_session_issuer_id = usi.id
 AND link.remote_session_client_id = @remote_session_client_id::uuid
JOIN remote_session_clients AS c
  ON c.id = link.remote_session_client_id
 AND c.deleted IS FALSE
 AND (c.project_id = p.id
      OR (c.project_id IS NULL AND c.organization_id = p.organization_id)
      OR (c.project_id IS NULL AND c.organization_id IS NULL))
WHERE s.id = @mcp_server_id::uuid
  AND s.user_session_issuer_id = @user_session_issuer_id::uuid
  AND s.deleted IS FALSE
  AND (NOT @require_sole_consumer::boolean OR (
    NOT EXISTS (
      SELECT 1
      FROM mcp_servers AS other
      JOIN projects AS other_project
        ON other_project.id = other.project_id
       AND other_project.organization_id = p.organization_id
       AND other_project.deleted IS FALSE
      WHERE other.user_session_issuer_id = s.user_session_issuer_id
        AND other.id <> s.id
        AND other.deleted IS FALSE
    )
    AND NOT EXISTS (
      SELECT 1
      FROM meta_mcp_servers AS gateway
      JOIN projects AS gateway_project
        ON gateway_project.id = gateway.project_id
       AND gateway_project.deleted IS FALSE
      WHERE gateway.user_session_issuer_id = s.user_session_issuer_id
        AND gateway.organization_id = p.organization_id
        AND gateway.deleted IS FALSE
    )
  ))
  AND (NOT @respect_removed::boolean OR NOT EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS recorded
    WHERE recorded.mcp_server_id = s.id
      AND recorded.remote_session_client_id = c.id
  ))
  AND (NOT @require_metadata_match::boolean OR (
    EXISTS (
      SELECT 1
      FROM remote_mcp_servers AS rms
      JOIN remote_protected_resources AS rpr
        ON rpr.project_id = s.project_id
       AND rpr.resource_identifier = rms.url
       AND rpr.deleted IS FALSE
      JOIN remote_session_issuers AS i
        ON i.id = c.remote_session_issuer_id
       AND i.deleted IS FALSE
       AND i.issuer = ANY (rpr.authorization_servers)
       AND (i.project_id = p.id
            OR (i.project_id IS NULL AND i.organization_id = p.organization_id)
            OR (i.project_id IS NULL AND i.organization_id IS NULL))
      WHERE rms.id = s.remote_mcp_server_id
        AND rms.project_id = s.project_id
        AND rms.deleted IS FALSE
    )
    AND NOT EXISTS (
      SELECT 1
      FROM remote_session_client_user_session_issuers AS other_link
      JOIN remote_session_clients AS other
        ON other.id = other_link.remote_session_client_id
       AND other.id <> c.id
       AND other.deleted IS FALSE
       AND (other.project_id = p.id
            OR (other.project_id IS NULL AND other.organization_id = p.organization_id)
            OR (other.project_id IS NULL AND other.organization_id IS NULL))
      JOIN remote_mcp_servers AS rms
        ON rms.id = s.remote_mcp_server_id
       AND rms.project_id = s.project_id
       AND rms.deleted IS FALSE
      JOIN remote_protected_resources AS rpr
        ON rpr.project_id = s.project_id
       AND rpr.resource_identifier = rms.url
       AND rpr.deleted IS FALSE
      JOIN remote_session_issuers AS i
        ON i.id = other.remote_session_issuer_id
       AND i.deleted IS FALSE
       AND i.issuer = ANY (rpr.authorization_servers)
       AND (i.project_id = p.id
            OR (i.project_id IS NULL AND i.organization_id = p.organization_id)
            OR (i.project_id IS NULL AND i.organization_id IS NULL))
      WHERE other_link.user_session_issuer_id = usi.id
    )
  ))
  AND (NOT @require_evidence_match::boolean OR (
    c.id IN (SELECT decided.client_id FROM decided)
    AND NOT EXISTS (
      SELECT 1
      FROM mcp_server_remote_session_clients AS live
      WHERE live.mcp_server_id = s.id
        AND live.deleted IS FALSE
    )
  ))
ON CONFLICT (mcp_server_id, remote_session_client_id) WHERE deleted IS FALSE DO NOTHING;

-- name: ListGatewayAssociationCandidates :many
-- One row per proxied MCP server a gateway issuer fronts and each live client
-- bound to that issuer, in one organization or every organization. A gateway
-- issuer fronts the members of a live gateway that signs in through it, and
-- an organization's only organization-level issuer fronts every server of
-- the organization, as the organization gateway does.
--
-- grant_match reports that the client, bound to the issuer, holds a live
-- grant whose RFC 8707 resource is the server's remote upstream URL, the
-- comparison request routing makes. The issuer a grant was minted through is
-- provenance only: a grant is one subject's credential for one client, and
-- the gateway offers it whichever issuer minted it. Only grants minted in the
-- server's organization count, so another tenant's use of a shared platform
-- client is never evidence here. A tunnel's resource identifier is only
-- what the tunnel claims, independent of where it dials, so it proves no
-- client: request routing matches tunnels by derived issuer alone, and so
-- does this. derived_match reports that the client's authorization server is
-- the one derived for the server, the comparison consent claimed members by.
WITH fronted AS (
    SELECT g.user_session_issuer_id AS issuer_id, s.id AS server_id
    FROM meta_mcp_servers AS g
    JOIN projects AS gp
      ON gp.id = g.project_id
     AND gp.deleted IS FALSE
    JOIN user_session_issuers AS gi
      ON gi.id = g.user_session_issuer_id
     AND gi.deleted IS FALSE
     AND (gi.project_id = g.project_id
          OR (gi.project_id IS NULL AND gi.organization_id = g.organization_id))
    JOIN meta_mcp_server_members AS m
      ON m.meta_mcp_server_id = g.id
     AND m.project_id = g.project_id
     AND m.deleted IS FALSE
    JOIN mcp_servers AS s
      ON s.id = m.mcp_server_id
     AND s.project_id = m.project_id
     AND s.deleted IS FALSE
    WHERE g.deleted IS FALSE
      AND (@organization_id::text = '' OR g.organization_id = @organization_id::text)
    UNION
    SELECT oi.id AS issuer_id, s.id AS server_id
    FROM user_session_issuers AS oi
    JOIN projects AS p
      ON p.organization_id = oi.organization_id
     AND p.deleted IS FALSE
    JOIN mcp_servers AS s
      ON s.project_id = p.id
     AND s.deleted IS FALSE
    WHERE oi.project_id IS NULL
      AND oi.deleted IS FALSE
      AND (@organization_id::text = '' OR oi.organization_id = @organization_id::text)
      AND (
        SELECT count(*)
        FROM user_session_issuers AS sibling
        WHERE sibling.organization_id = oi.organization_id
          AND sibling.project_id IS NULL
          AND sibling.deleted IS FALSE
      ) = 1
)
SELECT f.issuer_id::uuid AS issuer_id,
       s.id AS mcp_server_id,
       s.project_id,
       p.organization_id,
       (t.id IS NOT NULL)::boolean AS tunneled,
       c.id AS remote_session_client_id,
       EXISTS (
         SELECT 1
         FROM remote_sessions AS rs
         WHERE rs.remote_session_client_id = c.id
           AND rs.deleted IS FALSE
           AND r.url IS NOT NULL
           AND rtrim(rs.resource, '/') = rtrim(r.url, '/')
           AND EXISTS (
             SELECT 1
             FROM user_session_issuers AS minted
             LEFT JOIN projects AS minted_project ON minted_project.id = minted.project_id
             WHERE minted.id = rs.user_session_issuer_id
               AND COALESCE(minted.organization_id, minted_project.organization_id) = p.organization_id
           )
       )::boolean AS grant_match,
       EXISTS (
         SELECT 1
         FROM remote_session_issuers AS derived
         WHERE derived.id = s.remote_session_issuer_id
           AND derived.id = c.remote_session_issuer_id
           AND derived.deleted IS FALSE
           AND (derived.project_id = s.project_id
                OR (derived.project_id IS NULL AND derived.organization_id = p.organization_id)
                OR (derived.project_id IS NULL AND derived.organization_id IS NULL))
       )::boolean AS derived_match,
       EXISTS (
         SELECT 1
         FROM mcp_server_remote_session_clients AS recorded
         WHERE recorded.mcp_server_id = s.id
           AND recorded.remote_session_client_id = c.id
           AND recorded.deleted IS FALSE
       )::boolean AS pair_live,
       EXISTS (
         SELECT 1
         FROM mcp_server_remote_session_clients AS recorded
         WHERE recorded.mcp_server_id = s.id
           AND recorded.remote_session_client_id = c.id
       )::boolean AS pair_recorded
FROM fronted AS f
JOIN mcp_servers AS s
  ON s.id = f.server_id
JOIN projects AS p
  ON p.id = s.project_id
LEFT JOIN remote_mcp_servers AS r
  ON r.id = s.remote_mcp_server_id
 AND r.project_id = s.project_id
 AND r.deleted IS FALSE
LEFT JOIN tunneled_mcp_servers AS t
  ON t.id = s.tunneled_mcp_server_id
 AND t.project_id = s.project_id
 AND t.deleted IS FALSE
JOIN remote_session_client_user_session_issuers AS link
  ON link.user_session_issuer_id = f.issuer_id
JOIN remote_session_clients AS c
  ON c.id = link.remote_session_client_id
 AND c.deleted IS FALSE
 AND (c.project_id = s.project_id
      OR (c.project_id IS NULL AND c.organization_id = p.organization_id)
      OR (c.project_id IS NULL AND c.organization_id IS NULL))
WHERE s.slug IS NOT NULL
  AND (r.id IS NOT NULL OR t.id IS NOT NULL)
ORDER BY f.issuer_id, s.id, c.id;

-- name: BackfillGatewayAssociation :execrows
-- Writes one decided gateway association, conditional on the evidence still
-- holding: the server is live with a live source of its own project, the
-- issuer is still a live gateway issuer fronting it (or still the
-- organization's only organization-level issuer), the client is still bound
-- to the issuer and in the server's tenant, the issuer's clients' grants and
-- derived authorization servers still decide this client the way
-- DecideGateway does, and no row was recorded for the pair meanwhile, so an
-- explicit detach is never undone.
WITH target AS (
    SELECT s.id, s.project_id, p.organization_id, s.remote_session_issuer_id, r.url AS remote_url
    FROM mcp_servers AS s
    JOIN projects AS p
      ON p.id = s.project_id
    LEFT JOIN remote_mcp_servers AS r
      ON r.id = s.remote_mcp_server_id
     AND r.project_id = s.project_id
     AND r.deleted IS FALSE
    WHERE s.id = @mcp_server_id::uuid
),
evidence AS (
    SELECT ec.id AS client_id,
           (target.remote_url IS NOT NULL AND EXISTS (
             SELECT 1
             FROM remote_sessions AS rs
             WHERE rs.remote_session_client_id = ec.id
               AND rs.deleted IS FALSE
               AND rtrim(rs.resource, '/') = rtrim(target.remote_url, '/')
               AND EXISTS (
                 SELECT 1
                 FROM user_session_issuers AS minted
                 LEFT JOIN projects AS minted_project ON minted_project.id = minted.project_id
                 WHERE minted.id = rs.user_session_issuer_id
                   AND COALESCE(minted.organization_id, minted_project.organization_id) = target.organization_id
               )
           )) AS grant_match,
           EXISTS (
             SELECT 1
             FROM remote_session_issuers AS derived
             WHERE derived.id = target.remote_session_issuer_id
               AND derived.id = ec.remote_session_issuer_id
               AND derived.deleted IS FALSE
               AND (derived.project_id = target.project_id
                    OR (derived.project_id IS NULL AND derived.organization_id = target.organization_id)
                    OR (derived.project_id IS NULL AND derived.organization_id IS NULL))
           ) AS derived_match
    FROM target
    JOIN remote_session_client_user_session_issuers AS el
      ON el.user_session_issuer_id = @user_session_issuer_id::uuid
    JOIN remote_session_clients AS ec
      ON ec.id = el.remote_session_client_id
     AND ec.deleted IS FALSE
     AND (ec.project_id = target.project_id
          OR (ec.project_id IS NULL AND ec.organization_id = target.organization_id)
          OR (ec.project_id IS NULL AND ec.organization_id IS NULL))
),
decided AS (
    SELECT e.client_id
    FROM evidence AS e
    WHERE (
        e.grant_match
        AND (SELECT count(*) FROM evidence AS g WHERE g.grant_match) = 1
        AND NOT EXISTS (SELECT 1 FROM evidence AS o WHERE o.derived_match AND o.client_id <> e.client_id)
      )
      OR (
        e.derived_match
        AND NOT EXISTS (SELECT 1 FROM evidence AS g WHERE g.grant_match)
        AND (SELECT count(*) FROM evidence AS d WHERE d.derived_match) = 1
      )
)
INSERT INTO mcp_server_remote_session_clients (project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope)
SELECT s.project_id, p.organization_id, s.id, c.id, c.attachment_scope
FROM mcp_servers AS s
JOIN projects AS p
  ON p.id = s.project_id
 AND p.deleted IS FALSE
JOIN remote_session_client_user_session_issuers AS link
  ON link.user_session_issuer_id = @user_session_issuer_id::uuid
 AND link.remote_session_client_id = @remote_session_client_id::uuid
JOIN remote_session_clients AS c
  ON c.id = link.remote_session_client_id
 AND c.deleted IS FALSE
 AND (c.project_id = p.id
      OR (c.project_id IS NULL AND c.organization_id = p.organization_id)
      OR (c.project_id IS NULL AND c.organization_id IS NULL))
WHERE s.id = @mcp_server_id::uuid
  AND s.deleted IS FALSE
  AND s.slug IS NOT NULL
  AND (
    EXISTS (
      SELECT 1
      FROM remote_mcp_servers AS r
      WHERE r.id = s.remote_mcp_server_id
        AND r.project_id = s.project_id
        AND r.deleted IS FALSE
    )
    OR EXISTS (
      SELECT 1
      FROM tunneled_mcp_servers AS t
      WHERE t.id = s.tunneled_mcp_server_id
        AND t.project_id = s.project_id
        AND t.deleted IS FALSE
    )
  )
  AND (
    EXISTS (
      SELECT 1
      FROM meta_mcp_servers AS g
      JOIN projects AS gp
        ON gp.id = g.project_id
       AND gp.deleted IS FALSE
      JOIN user_session_issuers AS gi
        ON gi.id = g.user_session_issuer_id
       AND gi.deleted IS FALSE
       AND (gi.project_id = g.project_id
            OR (gi.project_id IS NULL AND gi.organization_id = g.organization_id))
      JOIN meta_mcp_server_members AS m
        ON m.meta_mcp_server_id = g.id
       AND m.project_id = g.project_id
       AND m.deleted IS FALSE
      WHERE g.user_session_issuer_id = link.user_session_issuer_id
        AND g.deleted IS FALSE
        AND m.mcp_server_id = s.id
    )
    OR EXISTS (
      SELECT 1
      FROM user_session_issuers AS oi
      WHERE oi.id = link.user_session_issuer_id
        AND oi.project_id IS NULL
        AND oi.organization_id = p.organization_id
        AND oi.deleted IS FALSE
        AND (
          SELECT count(*)
          FROM user_session_issuers AS sibling
          WHERE sibling.organization_id = oi.organization_id
            AND sibling.project_id IS NULL
            AND sibling.deleted IS FALSE
        ) = 1
    )
  )
  AND c.id IN (SELECT decided.client_id FROM decided)
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_server_remote_session_clients AS recorded
    WHERE recorded.mcp_server_id = s.id
      AND recorded.remote_session_client_id = c.id
  )
ON CONFLICT (mcp_server_id, remote_session_client_id) WHERE deleted IS FALSE DO NOTHING;

-- Test fixtures. Each seeds the one row its name says; nothing else uses them.

-- name: SeedOrganizationFixture :exec
INSERT INTO organization_metadata (id, name, slug) VALUES (@id, @id, @id);

-- name: SeedProjectFixture :exec
INSERT INTO projects (id, name, slug, organization_id) VALUES (@id, @slug, @slug, @organization_id);

-- name: SeedUserSessionIssuerFixture :exec
INSERT INTO user_session_issuers (id, project_id, organization_id, slug, authn_challenge_mode, session_duration)
VALUES (
    @id, @project_id,
    (SELECT p.organization_id FROM projects AS p WHERE p.id = @project_id),
    @slug, 'interactive', interval '1 hour'
);

-- name: SeedRemoteSessionIssuerFixture :exec
INSERT INTO remote_session_issuers (id, project_id, slug, issuer) VALUES (@id, @project_id, @slug, @issuer);

-- name: SeedRemoteSessionClientFixture :exec
INSERT INTO remote_session_clients (id, project_id, organization_id, remote_session_issuer_id, client_id)
VALUES (@id, @project_id, @organization_id, @remote_session_issuer_id, @client_id);

-- name: SeedClientBindingFixture :exec
INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id, user_session_issuer_id)
VALUES (@remote_session_client_id, @user_session_issuer_id);

-- name: DeleteClientBindingsFixture :exec
DELETE FROM remote_session_client_user_session_issuers WHERE remote_session_client_id = @remote_session_client_id;

-- name: SeedRemoteMCPServerFixture :exec
INSERT INTO remote_mcp_servers (id, project_id, transport_type, url) VALUES (@id, @project_id, 'streamable-http', @url);

-- name: SeedMCPServerFixture :exec
INSERT INTO mcp_servers (id, project_id, slug, remote_mcp_server_id, user_session_issuer_id, visibility)
VALUES (@id, @project_id, @slug, @remote_mcp_server_id, @user_session_issuer_id, 'private');

-- name: SeedProtectedResourceFixture :exec
INSERT INTO remote_protected_resources (project_id, organization_id, resource_identifier, authorization_servers)
VALUES (@project_id, @organization_id, @resource_identifier, @authorization_servers::text[]);

-- name: SeedAssociationFixture :exec
INSERT INTO mcp_server_remote_session_clients (project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope, deleted_at)
VALUES (
    @project_id, @organization_id, @mcp_server_id, @remote_session_client_id,
    (SELECT c.attachment_scope FROM remote_session_clients AS c WHERE c.id = @remote_session_client_id),
    CASE WHEN @removed::boolean THEN clock_timestamp() END
);

-- name: ListLiveAssociationsFixture :many
SELECT remote_session_client_id
FROM mcp_server_remote_session_clients
WHERE mcp_server_id = @mcp_server_id AND deleted IS FALSE
ORDER BY remote_session_client_id;

-- name: SeedOrganizationIssuerFixture :exec
INSERT INTO user_session_issuers (id, organization_id, slug, authn_challenge_mode, session_duration)
VALUES (@id, @organization_id, @slug, 'interactive', interval '1 hour');

-- name: SeedGatewayFixture :exec
INSERT INTO meta_mcp_servers (id, organization_id, project_id, user_session_issuer_id, name)
VALUES (@id, @organization_id, @project_id, @user_session_issuer_id, 'gateway');

-- name: SeedGatewayMemberFixture :exec
INSERT INTO meta_mcp_server_members (project_id, meta_mcp_server_id, mcp_server_id)
VALUES (@project_id, @meta_mcp_server_id, @mcp_server_id);

-- name: SeedGrantFixture :exec
INSERT INTO remote_sessions (subject_urn, user_session_issuer_id, remote_session_client_id, access_token_encrypted, resource)
VALUES (@subject_urn, @user_session_issuer_id, @remote_session_client_id, 'encrypted', @resource);

-- name: SetDerivedIssuerFixture :exec
UPDATE mcp_servers SET remote_session_issuer_id = @remote_session_issuer_id WHERE id = @id;

-- name: SoftDeleteProjectFixture :exec
UPDATE projects SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: SoftDeleteRemoteMCPServersOfServerFixture :exec
UPDATE remote_mcp_servers SET deleted_at = clock_timestamp()
WHERE id = (SELECT s.remote_mcp_server_id FROM mcp_servers AS s WHERE s.id = @mcp_server_id);

-- name: SeedTunneledMCPServerFixture :exec
INSERT INTO tunneled_mcp_servers (id, project_id, name, key_hash, key_prefix, resource_identifier)
VALUES (@id, @project_id, @name, @key_hash, 'prefix', @resource_identifier);

-- name: SeedTunneledServerFixture :exec
INSERT INTO mcp_servers (id, project_id, slug, tunneled_mcp_server_id, user_session_issuer_id, visibility)
VALUES (@id, @project_id, @slug, @tunneled_mcp_server_id, @user_session_issuer_id, 'private');

-- name: RemoveGatewayMemberFixture :exec
UPDATE meta_mcp_server_members SET deleted_at = clock_timestamp() WHERE mcp_server_id = @mcp_server_id AND deleted IS FALSE;
