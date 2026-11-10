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
