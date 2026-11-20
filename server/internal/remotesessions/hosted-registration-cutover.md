# Hosted remote-session registration: cutover assessment

**Hold:** this assessment does not change serving, consent, writers, or the
legacy endpoints. A hosted toolset must not dispatch more than one
remote-session token; this work does not add typed offers or auth-client RBAC. Source,
toolset, and attached environments, and toolset OAuth configuration, stay on
their existing paths. Proxied selection is out of scope.

## What the current data proves

`mcp_server_remote_session_clients` can name a registration for an exact
server, project, and tenant. The legacy sign-in junction names only a
registration and an issuer; the server's derived remote-session issuer names
the provider, not the registration. Neither an issuer match nor one remaining
issuer-linked registration proves a per-server assignment. In particular:

- A hosted server without an association but with a legacy sign-in binding
  can be an older, unmaterialized configuration or a server using the same
  sign-in as a sibling that owns the binding. Those histories can have the
  same current rows but require different behavior. A pair-level association
  tombstone does not establish that the entire server has no upstream: it
  records removal of one pair, and association cleanup also soft-deletes rows.
- A live association can come from compatibility capture, not necessarily
  from an explicit operator choice. A singleton is a parity candidate, not a
  read-mode marker. Do not backfill missing placements from sibling bindings,
  current user grants, an issuer match, or a sole remaining candidate.
- An empty remote-session token result is not evidence of explicit no-upstream
  configuration. No session for the subject, an unusable grant, ambiguity, and
  a server that does not use remote-session OAuth are distinct cases.

## Read-only parity inventory

For an approved assessment, take a consistent snapshot of active hosted MCP
servers and tenant/project scope. Compare **registration IDs**, not issuer
URLs, names, client display IDs, or token values. Keep detailed results in
restricted internal systems; report aggregate counts outside them. Do not
read token material or perform an OAuth refresh to generate this inventory.

1. For each direct hosted route, collect live, in-scope server association IDs
   and compare with the legacy candidate set using the direct sign-in, project,
   and tenant filters in `ListRemoteSessionClientsForUserSessionIssuer`.
   Track association history separately from live rows. Record invalid or
   missing configuration as its own category, not as no-upstream.
2. Repeat per placement in a stored or derived gateway using that gateway's
   effective sign-in and the member's project, tenant, admission, and legacy
   hosted provider filter. Check where either resolver has no candidate,
   disagrees, or can see more than one registration. Include shared sign-in and
   unrelated siblings; do not infer one placement from another.
3. Report exact singleton candidates, legacy-only, association-only,
   mismatched, ambiguous, removed, and empty states separately. Require
   authoritative operator/configuration evidence for explicit placements and
   for servers intentionally configured without remote-session OAuth. Empty
   rows alone cannot establish that classification. Report unresolved cases
   without changing them or treating this inventory as a backfill input.

### Direct hosted configuration check

Run this only on an approved read-only snapshot, with `org` and `project`
set as `psql` variables for one authorized organization and project. Run
`psql` with `-v ON_ERROR_STOP=1` so errors cannot be mistaken for zero
coverage. The query reads no grants or secret columns, and returns only counts. A zero `scoped_projects`
count is an invalid scope, not a successful check. Use a restricted process and
keep invocation inputs out of shared logs.

```sql
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '30s';

WITH scope AS (
    SELECT id, organization_id
    FROM projects
    WHERE id = :'project'::uuid AND organization_id = :'org' AND NOT deleted
), hosted AS (
    SELECT s.id, p.id AS project_id, p.organization_id, s.user_session_issuer_id
    FROM scope AS p
    JOIN mcp_servers AS s ON s.project_id = p.id AND NOT s.deleted
    JOIN toolsets AS t ON t.id = s.toolset_id AND NOT t.deleted
        AND t.project_id = p.id AND t.organization_id = p.organization_id
), registrations AS (
    SELECT c.id
    FROM scope AS p
    JOIN remote_session_clients AS c ON NOT c.deleted
        AND (c.project_id = p.id
            OR (c.project_id IS NULL AND (c.organization_id IS NULL OR c.organization_id = p.organization_id)))
    JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id AND NOT i.deleted
        AND (i.project_id = p.id
            OR (i.project_id IS NULL AND (i.organization_id IS NULL OR i.organization_id = p.organization_id)))
), compared AS (
    SELECT s.id, s.user_session_issuer_id, u.id IS NOT NULL AS live_sign_in,
           ARRAY(SELECT a.remote_session_client_id
                 FROM mcp_server_remote_session_clients AS a
                 JOIN registrations AS r ON r.id = a.remote_session_client_id
                 WHERE a.mcp_server_id = s.id AND a.project_id = s.project_id
                     AND a.organization_id = s.organization_id AND NOT a.deleted
                 ORDER BY a.remote_session_client_id) AS associated,
           ARRAY(SELECT l.remote_session_client_id
                 FROM remote_session_client_user_session_issuers AS l
                 JOIN registrations AS r ON r.id = l.remote_session_client_id
                 WHERE l.user_session_issuer_id = u.id
                 ORDER BY l.remote_session_client_id) AS legacy,
           EXISTS (SELECT 1 FROM mcp_server_remote_session_clients AS a
                   WHERE a.mcp_server_id = s.id AND a.project_id = s.project_id
                       AND a.organization_id = s.organization_id AND a.deleted) AS removed_history,
           EXISTS (SELECT 1 FROM mcp_server_remote_session_clients AS a
                   WHERE a.mcp_server_id = s.id AND a.project_id = s.project_id
                       AND a.organization_id = s.organization_id AND NOT a.deleted
                       AND NOT EXISTS (SELECT 1 FROM registrations AS r WHERE r.id = a.remote_session_client_id))
               AS invalid_association
    FROM hosted AS s
    LEFT JOIN user_session_issuers AS u ON u.id = s.user_session_issuer_id AND NOT u.deleted
        AND (u.project_id = s.project_id
            OR (u.project_id IS NULL AND u.organization_id = s.organization_id))
), classified AS (
    SELECT CASE
               WHEN user_session_issuer_id IS NOT NULL AND NOT live_sign_in THEN 'invalid_sign_in'
               WHEN invalid_association THEN 'invalid_association'
               WHEN cardinality(associated) > 1 OR cardinality(legacy) > 1 THEN 'ambiguous'
               WHEN cardinality(associated) = 0 AND cardinality(legacy) = 0 THEN 'empty_unverified'
               WHEN cardinality(associated) = 0 THEN 'legacy_only'
               WHEN cardinality(legacy) = 0 THEN 'association_only'
               WHEN associated = legacy THEN 'singleton_match'
               ELSE 'singleton_mismatch'
           END AS result,
           removed_history
    FROM compared
)
SELECT (SELECT count(*) FROM scope) AS scoped_projects,
       count(*) AS hosted_configurations,
       count(*) FILTER (WHERE result = 'invalid_sign_in') AS invalid_sign_in,
       count(*) FILTER (WHERE result = 'invalid_association') AS invalid_association,
       count(*) FILTER (WHERE result = 'ambiguous') AS ambiguous,
       count(*) FILTER (WHERE result = 'empty_unverified') AS empty_unverified,
       count(*) FILTER (WHERE result = 'legacy_only') AS legacy_only,
       count(*) FILTER (WHERE result = 'association_only') AS association_only,
       count(*) FILTER (WHERE result = 'singleton_match') AS singleton_match,
       count(*) FILTER (WHERE result = 'singleton_mismatch') AS singleton_mismatch,
       count(*) FILTER (WHERE removed_history) AS with_removed_history
FROM classified;

ROLLBACK;
```

The eight result classes are disjoint; `with_removed_history` overlaps them.
This is configuration coverage, not route admission, intent, or grant parity.
For example, two hosted servers that share a sign-in with two legacy bindings
remain ambiguous here even if each already has a different exact association.
A removed row does not prove an explicit detach or authorize zero credentials.
The query intentionally does not infer configuration from toolset environment,
protected resource metadata, or the presence of a usable user session.

For gateway verification, enumerate the admitted hosted placements separately
with the exact deployed member snapshot (`resolveEndpointMemberSnapshot` for
consent; `resolveGateMemberSnapshot` for dispatch). For each placement use the
gateway's effective sign-in, member project and organization, and the effective
provider (`upstreamIssuerKeys.authoritative`) for the existing `memberUsesClient`
filter. The organization gateway enables the organization-wide lookup in
`ListRemoteSessionClientsForUserSessionIssuer`, then qualifies each member;
the stored gateway uses its project lookup. Compare those IDs with the same
per-server association set and aggregate the same classes independently of
this direct check. Do not union projects or treat a direct check as evidence
of gateway parity. Verify grant parity separately with approved, redacted
status and revocation evidence, not by reading tokens.

## Contract and release gates

The intended configuration methods are `remoteSessions.listServerClientAssociations`,
`remoteSessions.attachServerClientAssociation`, and
`remoteSessions.detachServerClientAssociation`, with the existing project and
server authorization checks. They describe or change a server's placement,
not its downstream issuer, a gateway's membership, a registration's lifetime,
or a user's grant. They are **not** runtime-equivalent for hosted servers yet.

Before changing readers, verify that all hosted setup/edit/clear paths which
actually choose a registration record or remove that exact server association
with the corresponding configuration change. A downstream issuer attachment
by itself chooses no registration. Review current and rollback writers,
including legacy association capture and detach. No automatic broad inference
or fallback on a lookup miss is allowed. With the existing schema, require a
verified reconciliation and a controlled transition rather than using row
absence as a permanent per-server compatibility switch. If the approved
process cannot establish completeness, keep the existing hosted path and
leave hosted caller migration and retirement blocked.

After that gate, consent, callback/recovery, and direct and gateway dispatch
must revalidate and use the same exact placement under the server's project
and tenant. Resolve only that registration's live subject grant; recheck
revocation, current placement, and existing admission. The existing
`ResolveAuthClientAccessTokens` accepts an exact, already-authorized
registration without a sign-in link; it is not itself a placement check.
Review its attached-agent and legacy-grant behavior before using it for hosted
resolution. Reject ambiguity
before grant filtering (never choose the registration that happens to have a
ready token). A proved no-upstream route or explicit detach must never fall
back to legacy bindings or return that registration's credentials. Do not
remove downstream sign-in to simulate a detach.

A mixed-version fleet can keep today's readers while checking parity, but
must not expose association-only attach or detach while any serving or rollback
revision can still return credentials via the legacy path. This also blocks
activating a per-server registration without a legacy registration-to-sign-in
binding; the downstream sign-in issuer itself can remain attached. Verify
serving revisions, the rollback floor, exact mappings, and grant parity
before switching. This is separate from verifying that legacy RPC callers
have stopped using those endpoints.

Acceptance tests for a future switch: direct and stored/derived gateway
hosted consent, callback/recovery, and tool execution with the exact token;
two hosted servers with the same sign-in and different registrations (include
same-provider registrations), an unrelated sibling, explicit no-upstream and
detach, multiple configurations, foreign tenant, wrong subject, and revoked
grant. Verify that hosted environment behavior is unchanged, and that non-hosted
and proxied backends do not enter this path. No part of that behavior is
implemented by this assessment.
