# Channels security

How Channels decides who may reach it, and where those decisions are made.

## Permissions

Channels carries three organization-scoped permissions, declared in
`server/internal/authz/scopes.go`:

| Scope               | Grants                                                     |
| ------------------- | ---------------------------------------------------------- |
| `channels:connect`  | Open a session and take part in conversations.             |
| `channels:admin`    | Administer instances and membership.                       |
| `channels:read_all` | Read other members' conversations, including private ones. |

None of the three implies another, none is implied by organization membership,
`org:admin`, or any `mcp:*` grant, and each is evaluated as its own check
against freshly loaded grants — including on every SSE tick, so a revoked grant
stops delivery rather than riding a cached decision.

### `channels:read_all` is not an administrator default

The system administrator role grants `channels:connect` and `channels:admin`.
It deliberately does not grant `channels:read_all`: administering Channels
means configuring instances and membership, which does not require reading what
members said to each other in private. An organization that wants a transcript
reviewer grants `channels:read_all` explicitly through a custom role, the same
way `chat:read` is granted for session transcripts.
`TestChannelsAdminRoleDoesNotReadPrivateConversations` pins this down.

## Rollout gate

Channels is gated per deployment by `GRAM_CHANNELS_ENABLED`
(`--channels-enabled`, default **false**). Local development sets it to `true`
in `mise.toml`; production deployments stay off until the surface ships.

The gate is deployment scoped rather than a per-organization product feature
because the surfaces it has to cover include unauthenticated ones. It governs
three things together:

1. **The endpoints.** `attachChannels` registers no routes at all when the gate
   is off, so `/channels/mcp` and `/channels/api` answer the mux's own 404.
2. **OAuth discovery.** `OAuthHTTP.Attach` registers
   `/.well-known/oauth-protected-resource/channels/mcp` only when the gate is
   on, so the server never advertises an audience whose endpoint is not served.
3. **Audience admission.** `requestedResource` refuses the Channels resource,
   which also means `servesResource` refuses it — so no authorization, token
   request, or refresh can bind to the Channels audience.

They move together on purpose: advertising an audience that 404s, or accepting
one for an endpoint that is not served, are both worse than refusing all three.

A per-organization product feature cannot decide 2 or 3, because no
organization is known until a caller has authenticated against the audience
being advertised. Per-organization enablement and the Platform MCP product
feature remain separate concerns: they govern what an admitted caller may do,
not whether the surface exists on this deployment.

### Denials say nothing

A gated-off deployment returns a plain 404 and an unsupported-resource error
that does not name Channels, its permissions, or its rollout state. Probing the
host learns nothing about unreleased product surface.

## Credential lifetime

Channels tokens are minted for their own audience against the existing
control-plane OAuth server, and admission establishes only live organization
membership — never a Channels permission. Permissions are re-evaluated per
operation from live grants, so:

- **Revoking a Channels scope** takes effect on the caller's next operation and
  next SSE tick. It does not require revoking the token: a token whose grants no
  longer satisfy a check is refused at the operation.
- **Refreshing a token** cannot widen its audience (`grantResource` takes the
  audience from the presented credential, never from the request) and cannot
  re-acquire a scope: tokens carry no scope copy, and refresh on a gated-off
  deployment is refused because the audience is no longer served.

The residual is bounded by the operation-level re-evaluation, which is why no
revocation list is needed for scope changes.
