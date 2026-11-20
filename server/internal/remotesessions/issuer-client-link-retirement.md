# Retiring direct issuer-to-client links (AIM-1153)

These RPCs manage or report links between downstream sign-in issuers and upstream OAuth registrations:

- `POST /rpc/remoteSessionClients.attachUserSessionIssuer`
- `POST /rpc/remoteSessionClients.detachUserSessionIssuer`
- `GET /rpc/organizationUserSessionIssuers.listRemoteSessionClientBindings`

These links are not a source of truth for a server's typed upstream offers. Choose a replacement based on the operation, not the old link:

- Downstream sign-in: `POST /rpc/mcpServers.update` attaches or clears the downstream issuer on a server (including a hosted server); `POST /rpc/metaMcp.update` sets the gateway's downstream issuer. These operations do not remove a single upstream provider. Hosted external OAuth has its own `toolsets.addExternalOAuthServer`, `toolsets.updateExternalOAuthServer`, and `toolsets.removeOAuthServer` RPCs.
- Typed upstream offers: read `GET /rpc/authClients.list?mcp_server_id=…` for offers on a proxied server, or `GET /rpc/authClients.getMcpServerAuthentication` for its setup status. Use `POST /rpc/authClients.attachToMcpServer` and `POST /rpc/authClients.detachFromMcpServer` only to edit offers on that server. A member offer can also be visible through other gateways; do not use it to emulate gateway-local access. Do not delete a registration to remove an offer or a downstream association.
- Server OAuth registrations: `GET /rpc/remoteSessions.listServerClientAssociations`, `POST /rpc/remoteSessions.attachServerClientAssociation`, and `POST /rpc/remoteSessions.detachServerClientAssociation` manage per-server registration associations. These are not the hosted sign-in source of truth today; do not switch a hosted caller to them until hosted consent and token routing consume that exact association.
- Agent session attachments: use `GET /rpc/remoteSessions.listBindings`, `POST /rpc/remoteSessions.attachBinding`, and `POST /rpc/remoteSessions.detachBinding` for an agent's exact session attachment. Detaching an upstream offer is not an agent-session detach.

These are capability-specific routes, not drop-in replacements for the remaining gateway and hosted link controls. Keep those controls on the old contract until migration preserves their scope and behavior.

## Rollout order

1. Publish the deprecation markers while retaining the old request and response contracts and the guarded unlink behavior. No sunset clock starts with the markers alone.
2. Verify that the replacement flows preserve gateway scope, hosted sign-in, and exact principal attachments. Check authorization parity: a replacement that requires organization administration is not a drop-in replacement for a project-write workflow. Ship the dashboard migration while both contracts are available. Keep the legacy endpoints on all serving server versions until the new dashboard can use the replacement on every server it can reach, old dashboard clients have drained, and the rollback dashboard no longer calls the legacy endpoints. Keep the legacy endpoints through the supported rollback period.
3. The API owner must verify supported SDK versions and direct RPC support, and attribute remaining traffic. If supported clients still use the old contract, publish migration guidance and an agreed sunset date through their support channels. The proposed minimum notice is 30 calendar days from delivery of that guidance, or longer if support policy or an agreement requires it. Do not retire a route while a supported caller still depends on it or while traffic remains unexplained. Sampled trace counts alone are not evidence that callers have drained.
4. Retire the three routes and regenerate contracts in a separate release after the gates above. Verify that none of the old routes can mutate legacy links and that each of the three supported capabilities above still works. Do not combine route retirement with schema removal. If route retirement must be rolled back, restore a server version with the guarded legacy handlers, not an unguarded junction writer.
