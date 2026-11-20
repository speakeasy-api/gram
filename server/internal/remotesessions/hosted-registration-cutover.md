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
must not expose association-only attach/detach or linkless activation while
any serving or rollback revision can still return credentials via the legacy
path. Verify serving revisions, the rollback floor, exact mappings, and grant
parity before switching. This is separate from depletion of legacy RPC callers.

Acceptance tests for a future switch: direct and stored/derived gateway
hosted consent, callback/recovery, and tool execution with the exact token;
two hosted servers with the same sign-in and different registrations (include
same-provider registrations), an unrelated sibling, explicit no-upstream and
detach, multiple configurations, foreign tenant, wrong subject, and revoked
grant. Verify that hosted environment behavior is unchanged, and that non-hosted
and proxied backends do not enter this path. No part of that behavior is
implemented by this assessment.
