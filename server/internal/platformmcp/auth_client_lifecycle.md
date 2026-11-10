# Upstream credential lifecycle: Platform MCP boundary

## Decision

Do not expose `remoteSessions.detachServerClientAssociation` as a Platform MCP
credential-removal tool. This compatibility operation is not the inverse of
`offer_mcp_upstream_credential`. Removing a credential offer remains a dashboard
operation; this exclusion does not claim complete agent parity for removal.

The intended outcome would be to stop one exact server offering one exact typed
credential, without deleting the credential, changing its other servers, or
changing the sign-in's bindings. An external administrator could request that
outcome. A managed assistant must not acquire server-credential administration
merely because it can use a credential.

## Why the compatibility detach is not a safe substitute

- `offer_mcp_upstream_credential` accepts an exact project, server, and typed
  `auth_client_id`. `AccessRoleMutationService.OfferUpstreamCredential` uses
  `authclients.AttachToServer`, which creates an administrator-owned link.
- The compatibility detach accepts a legacy `remote_session_client_id`, not a
  typed credential or link ID. For an enforced sole consumer, it retires only
  backfill-owned links derived from that registration, preserves the underlying
  association and sign-in binding, and returns 404 when nothing remains to retire.
  Administrator-owned links, including links to backfilled credentials, survive.
- For a non-sole consumer, the same endpoint removes an association or records an
  unclaimed-client exclusion and synchronizes its derived links. It therefore
  cannot promise the same typed-link-only mutation for every target.
- `get_mcp_connection_settings` returns typed credential IDs, types, enabled state,
  personal/shared status and sharing counts. `MCPUpstreamAuthClient` does not expose
  the legacy registration mapping, per-link provenance, or a link version. Its
  settings version does not cover credential links. An agent must not infer a
  registration from a name, treat the settings version as a credential-link
  concurrency guard, or claim that detaching a registration removed an
  administrator-owned offer.

A removal tool needs an outcome-specific preview of the exact typed target and
its provenance, live authorization for that project/server and actor, explicit
confirmation of the affected offer, concurrency/replay protection, atomic audit,
and a live result confirming the offer is gone. It must distinguish external
administrator access from managed-assistant credential use. Those are contract
requirements, not authorization supplied by the legacy registration endpoint.
No new removal tool or managed-assistant mutation is admitted here.

## Personal sign-ins on more than one server

A person holds one live upstream grant per sign-in registration
(`remote_session_client`). When two servers with different upstream resources
use one registration, a member who signs in for one server can replace the
grant the other server uses. Resource-qualified grants remove that limit, and
are a separate change that is not in force yet.

Until it is, the dashboard setup flows do not attach a stored personal sign-in
to another server. Each offers **New from scratch** or **New from existing**,
and both create a registration with its own id. **New from existing** copies a
stored registration's reusable settings; the stored secret is copied on the
server through `template_client_id` and never reaches the browser. Sessions,
grants, issuer attachments and server associations are not copied.

That restriction is in the dashboard only. These operations still attach a
stored personal sign-in and are unchanged, so callers that already depend on
sharing keep working:

- `offer_mcp_upstream_credential`, for any credential type. Its description
  tells the agent to warn the user about a personal sign-in.
- `authClients.attachToMcpServer`, and `authClients.create` with an existing
  `remote_session_client_id`.
- `remoteSessions.commitServerIdentityConfiguration` with `client_mode`
  `existing`, and `remoteSessionClients.attachUserSessionIssuer`.

`offer_mcp_upstream_credential` is not changed to refuse personal sign-ins: the
agent cannot create the separate registration the dashboard offers instead,
because new credentials and their secrets are created in the dashboard. A
refusal would remove the outcome without giving the agent another way to it.
Sharing shared credentials (service accounts, stored and caller-supplied
headers) is not affected anywhere.

Existing shared configurations are not rewritten. Lifting the dashboard
restriction needs the resource-qualified grant changes and their consumers
deployed and validated, not merely merged.

## Evidence

- `client/dashboard/src/pages/mcp/x/tabs/settings/sections/authentication/AuthClientSheet.tsx`:
  removal confirms the exact typed credential and server, using
  `authClients.detachAuthClientFromMcpServer`, not the legacy registration detach.

- `auth_client_access.go`: `OfferUpstreamCredential`, confirmation, exact target
  admission, audited attachment and post-write read.
- `connection_settings.go`: `MCPUpstreamAuthClient` and `MCPConnectionSettings`.
- `auth_clients_integration_test.go`:
  `TestPlatformMCPOffersUpstreamCredentialOnAnotherServer` exercises the actual
  offer workflow and preserves the source server's offer.
- `../remotesessions/serverclientassociations_test.go`:
  `TestDetachServerClientAssociation_EnforcedSoleConsumer` proves link retirement,
  preserved association, link-only audit, durable resync and repeated 404;
  `TestDetachServerClientAssociation_KeepsAdministratorLinkToBackfilledClient`
  proves an administrator-owned link is not retired through this endpoint.
