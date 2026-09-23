# Signed caller identity for MCP tunnels

Gram can send a signed caller assertion through a tunnel to your private MCP
server. Your server verifies it against Gram's public JWKS and applies its own
access policy. The assertion contains no Gram credentials.

AICP issues the assertions. Pin these values in your verifier:

- Issuer (`iss`): `https://tunnel.speakeasy.com`
- Public JWKS: `https://tunnel.speakeasy.com/.well-known/jwks.json`

The endpoints are independent of your server's OAuth provider and custom domain.
You do not need to configure a per-server assertion issuer in Gram.

## Wire contract

The HTTP header is `SPEAKEASY_AUTHZ: <JWT>`, without a `Bearer` prefix. Gram
removes client-supplied variants of this header and the `Speakeasy-Authz` alias.
The tunnel gateway and agent forward the assertion to your server. Upstream
OAuth uses its own `Authorization` header.

Header names are case-insensitive. Underscores are significant: if the customer
server is behind an HTTP proxy that drops headers containing underscores,
configure that proxy to preserve `SPEAKEASY_AUTHZ`.

Tokens use RS256 and the protected header `typ: speakeasy-authz+jwt`. The `kid`
is the public key's RFC 7638 SHA-256 thumbprint. Their lifetime is at most 60
seconds, capped by the source credential's expiry where available.

| Claim                           | Meaning                                                                                                                                  |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `version`                       | Contract version, currently `1`.                                                                                                         |
| `iss`                           | `https://tunnel.speakeasy.com` (AICP).                                                                                                   |
| `aud`                           | `tunneled-mcp-server:<TUNNELED_MCP_SERVER_ID>`. Identifies the destination, independently of gateway address, MCP slug or custom domain. |
| `sub`                           | Typed principal identifier, for example `user:<USER_ID>`.                                                                                |
| `email`                         | Human user's email from their Gram profile; absent for agents and API keys.                                                              |
| `principal_type`                | Authenticated principal class; a machine credential is never represented as its owner's human identity.                                  |
| `organization_id`, `project_id` | Destination tenant and project.                                                                                                          |
| `mcp_server_id`                 | MCP wrapper serving this request.                                                                                                        |
| `tunneled_mcp_server_id`        | Underlying tunneled MCP source.                                                                                                          |
| `purpose`                       | Context in which Gram issued the assertion.                                                                                              |
| `allowed_methods`               | Present for consent discovery; limits the assertion to the listed MCP methods.                                                           |
| `iat`, `exp`                    | Issuance and expiry, Unix seconds.                                                                                                       |
| `jti`                           | Unique assertion identifier for correlation.                                                                                             |

Runtime requests use `purpose=mcp_request`. Supported subjects are `user:<USER_ID>`,
`api_key:<API_KEY_ID>` and `agent:<AGENT_ID>`, with matching `principal_type`
values `user`, `api_key` and `agent`. These are Gram identifiers, not email
addresses or upstream account IDs. API keys and agents are never identified as
their human creator or owner. Human assertions also carry `email`; use the
stable `sub` as the identity key because an email can change. The assertion does
not make an `email_verified` claim.

A human authenticated into a live OAuth consent challenge can receive a
discovery assertion before granting tool access. It has `purpose=mcp_discovery`
and `allowed_methods=["server/discover", "initialize", "notifications/initialized", "ping", "tools/list"]`.
The receiver must reject it for `tools/call` and any other method. The server's
discovery policy still determines which tools that user may see. Discovery is
limited to ten minutes from the challenge's creation; restart login if that
window expires. Impersonated or unknown authorizers and agent-selected consent
flows do not receive a discovery assertion. HTTP `DELETE` used to close the
discovery session also carries it; a receiver may permit that session cleanup.

Runtime user assertions identify the effective user of the validated Gram
session. Session credentials do not record whether support impersonation
occurred, so the assertion cannot rule it out.

Issuance is limited to private tunneled destinations. Public destinations,
anonymous callers, embedded chat sessions, assistant credentials, workload
sessions and background probes do not receive an assertion in this version.
Servers that require the header must account for those unsupported callers.
While signing is enabled, Gram skips synthetic OAuth keepalive probes for
private tunnels and keeps their prior connection verdict. Interactive consent
validation uses the authenticated discovery assertion.

Gram rejects a request before forwarding if the caller's authenticated
organization or bound project differs from the destination. Use an API key
scoped to the destination project.

## Verification

Fetch public keys from `https://tunnel.speakeasy.com/.well-known/jwks.json`.
Cache them for up to five minutes. On an unknown `kid`, refresh from that URL
once before rejecting the assertion. The endpoint supports GET, HEAD, ETag and
conditional GET.

Your server must:

1. Read exactly one assertion and select an RSA signing key by `kid` from the
   AICP JWKS above. Do not follow a token-provided key URL or issuer.
2. Verify the signature with an explicit RS256 allowlist and require
   `typ=speakeasy-authz+jwt` and `version=1`.
3. Require `iss=https://tunnel.speakeasy.com` and the destination audience. Check
   organization, project and MCP server bindings against your configuration.
4. Require `iat` and `exp`, reject expired/future-dated tokens, and enforce a
   maximum 60-second lifetime with at most five seconds of clock tolerance.
5. Check the principal type and purpose before applying your access policy.
   Require `purpose=mcp_request` for tool calls; accept `mcp_discovery`
   only for the discovery methods listed above and in `allowed_methods`. Reject
   unknown purposes. A valid signature alone does not authorize a tool call.

Treat a missing assertion as unauthenticated. A captured bearer token can be
reused until it expires, even with a unique `jti`, unless your server adds replay
protection. Keep assertions out of tool arguments, ordinary application logs,
and error responses.

Validate each HTTP request. Accepting an assertion during initialization does
not authorize later requests on the same MCP session. A response admitted while
the token was valid may continue streaming after its expiry.

## Key rotation

Publish the next key alongside the active key on every replica. Once the last
replica serving the old JWKS has stopped, wait the full five-minute cache
lifetime before switching signing keys. Keep the retiring public key until all
old signer replicas have stopped and their last tokens have expired, including
clock tolerance. Clients that cached the old key can still accept its tokens
after you remove it from the JWKS.

## Tool-call records

When logging is enabled, the MCP proxy records tunneled calls with available
caller attribution, server/tool identifiers, timing and status. Recording
arguments and results also requires the tool I/O logging setting. These
best-effort records may omit attempted or rejected calls.

The checked-in retention policy keeps raw rows for 90 days. Arguments and
results are each truncated at 64 KiB when tool I/O logging is enabled. The Logs
page and paginated telemetry API provide query access. A project can also
configure the `tool_call_logs` data-export source to stream records to an OTLP
HTTP `/v1/logs` destination. Export delivery is at least once; consumers can
deduplicate using `gram.telemetry.log.id`. The destination's sensitive-data
policy determines whether payloads and user attributes are included.

Telemetry retention, delivery and export are unchanged. Management audit events
use a separate subsystem.
