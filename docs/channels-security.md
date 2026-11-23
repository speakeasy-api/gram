# Channels security

How Channels decides who may reach it, and where those decisions are made.

## Permissions

Channels carries three native project-targeted permissions, declared in
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

## Rollout and project controls

The `channels` PostHog flag controls rollout for an organization. The dashboard
hides Channels without that flag, and authenticated Channels admission checks
the same flag using the organization's context. Project enablement and
Channels RBAC remain separate product controls.

Channels owns its OAuth endpoints and metadata independently of Platform MCP.
Public OAuth discovery and registration do not evaluate organization flags:
there is no organization context before authentication. Consent and resource
operations retain their authentication, membership, origin and CSRF checks.
No additional deployment environment variable is required.

## Browser origins

Channels exposes its OAuth metadata, registration, token, and revocation
endpoints with credentialless public CORS. Consent remains a browser-navigation
flow with browser-session, origin and CSRF checks, plus proof of the verified
user and exact organization/project target.

The MCP resource's CORS policy exposes authentication challenges without
allowing browser cookies. It does not bypass the existing `MCPSecurity`
origin policy: browser MCP requests still need a trusted origin. Unknown
external origins receive HTTP 403. Native clients without browser Origin
metadata retain their existing behavior. The browser API keeps its separate
session-authenticated policy.

## Credential lifetime

Channels owns its OAuth client registrations, connections, one-use codes,
access sessions, refresh families, and refresh-reuse evidence. It reuses the
existing sign-in identity and neutral protocol/signing utilities, not Platform
MCP storage or its principal/admission adapter.

Tokens bind to the Channels issuer, exact project resource audience,
organization, project, verified user and registered client. Authorization codes
and refresh credentials retain those bindings and cannot retarget them. Authentication
also checks Channels-owned active connection/session state. A Platform MCP
token is not a Channels credential, and a Channels token grants no access to
Platform MCP. Reauthorization, refresh, and revocation remain independent.

Channels admission checks current organization membership and project access,
and rebuilds native project-targeted grants from the verified actor. Each
operation and subscription reconciliation checks
current Channels permissions and enablement. A permission loss stops the next
operation and delivery reconciliation without waiting for token expiration.

The browser API accepts the existing browser session with origin protection;
it does not accept OAuth bearer tokens. The MCP endpoint does not accept an
ambient browser cookie as its credential.

## Exact project target

MCP is `/channels/<org-slug>/<project-slug>/mcp`; the browser API is
`/channels/<org-slug>/<project-slug>/api`. Its protected metadata is
`/.well-known/oauth-protected-resource/channels/<org-slug>/<project-slug>/mcp`;
401 challenges name that exact resource metadata. The issuer and OAuth endpoints
remain under `/channels/oauth`. There are no old aliases or default-project
fallbacks. All data, settings, presence, exceptions, preferences, delivery and
retention are scoped by organization and project, not just endpoint selection.
An exception or grant for one project supplies no authority in another.

Consent uses the URL-fixed target and verified browser identity, not the active
dashboard organization/project. A user may authorize an accessible project in
another organization without a dashboard switch. The target cannot change at
consent submission, code exchange or refresh.

Only Channels OAuth authenticates MCP. Native API keys and other machine
credentials are not accepted. On first installation, Channels creates its own
schema and OAuth state; it does not import Platform connections or credentials,
accept Platform tokens, or convert a legacy Channels storage format. Shared
sign-in identities do not imply shared credential authority.
