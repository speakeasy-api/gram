# Signed caller identity for MCP tunnels

Gram can send a signed caller assertion through a tunnel to your private MCP
server. Your server verifies it against Gram's public JWKS and applies its own
access policy. The assertion contains no Gram credentials.

AICP issues the assertions. Pin these values in your verifier:

- Issuer (`iss`): `https://tunnel.speakeasy.com`
- Public JWKS: `https://tunnel.speakeasy.com/.well-known/jwks.json`

The tunnel gateway serves the JWKS endpoint from its public-key bundle. The
application serving tiers hold the private key and sign assertions when
`GRAM_AUTHZ_PRIVATE_KEY`, `GRAM_AUTHZ_PUBLIC_KEYS` and `GRAM_AUTHZ_ISSUER_URL`
are all present. Missing settings disable signing; invalid keys or an invalid
issuer in a complete configuration cause startup to fail.

The issuer and JWKS URL are independent of your server's OAuth provider and
custom domain. You do not need to configure a per-server assertion issuer in Gram.

## Wire contract

The HTTP header is `SPEAKEASY_AUTHZ: <JWT>`, without a `Bearer` prefix. Gram
removes client-supplied variants of this header and the `Speakeasy-Authz` alias
in the forwarding proxy, before adding its own assertion.
The tunnel gateway and agent forward the assertion to your server. Upstream
OAuth uses its own `Authorization` header.

Header names are case-insensitive. Underscores are significant: if the customer
server is behind an HTTP proxy that drops headers containing underscores,
configure that proxy to preserve `SPEAKEASY_AUTHZ`.

Tokens use RS256 and the protected header `typ: speakeasy-authz+jwt`. The `kid`
is the public key's RFC 7638 SHA-256 thumbprint. Their lifetime is at most 60
seconds, capped by the source credential's expiry where available.

| Claim                           | Meaning                                                                                                    |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `version`                       | Contract version, currently `1`.                                                                           |
| `iss`                           | `https://tunnel.speakeasy.com` (AICP).                                                                     |
| `aud`                           | The destination's saved resource identifier, or `tunneled-mcp-server:<TUNNELED_MCP_SERVER_ID>` when unset. |
| `sub`                           | Typed principal identifier, for example `user:<USER_ID>`.                                                  |
| `email`                         | Human user's email from their Gram profile; absent for agents and API keys.                                |
| `principal_type`                | Authenticated principal class; a machine credential is never represented as its owner's human identity.    |
| `organization_id`, `project_id` | Destination tenant and project.                                                                            |
| `mcp_server_id`                 | MCP wrapper serving this request.                                                                          |
| `tunneled_mcp_server_id`        | Underlying tunneled MCP source.                                                                            |
| `purpose`                       | Context in which Gram issued the assertion.                                                                |
| `allowed_methods`               | Methods Gram permits during consent discovery.                                                             |
| `iat`, `exp`                    | Issuance and expiry, Unix seconds.                                                                         |
| `jti`                           | Unique assertion identifier for correlation.                                                               |

Set the resource identifier in the tunneled source settings to use your server's
own audience, such as `https://mcp.internal.example.com/mcp`. Gram copies the
saved identifier exactly, including trailing slashes and escaped characters,
and never connects to that address. Client-supplied resource parameters cannot
change the assertion's audience. Gateway requests use the selected member's
identifier. If the setting is blank, the audience is the tunneled server's Gram
identifier above.

Changing or clearing the setting changes the audience of subsequent assertions;
coordinate that change with your verifier. Older saved identifiers may have had
trailing slashes removed. Save the exact identifier again if your verifier needs
a trailing slash. Restoring a path slash before a query also changes credential
routing, for example `/mcp?tenant=example` to `/mcp/?tenant=example`. Reconnect
upstream OAuth credentials for the new resource; credentials qualified to the
old resource are not forwarded. Existing saved settings are unchanged by deployment.

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
Gram enforces this method list and blocks tool calls during consent. The server's
discovery policy still determines which tools that user may see. Discovery is
limited to ten minutes from the challenge's creation; restart login if that
window expires. Impersonated or unknown authorizers and agent-selected consent
flows do not receive a discovery assertion. HTTP `DELETE` used to close the
discovery session also carries it.

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
3. Require `iss=https://tunnel.speakeasy.com` and the exact destination audience.
4. Require `iat` and `exp`, reject expired/future-dated tokens, and enforce a
   maximum 60-second lifetime with at most five seconds of clock tolerance.

Gram enforces its consent and tool-access rules before forwarding. Use the
verified caller claims for your server's own access policy.

For policies that restrict access to a specific Gram organization, project or
server, also check the corresponding ID claims.

Treat a missing assertion as unauthenticated. A captured bearer token can be
reused until it expires, even with a unique `jti`, unless your server adds replay
protection. Keep assertions out of tool arguments, ordinary application logs,
and error responses.

Validate each HTTP request. Accepting an assertion during initialization does
not authorize later requests on the same MCP session. A response admitted while
the token was valid may continue streaming after its expiry.

## Key rotation

The infrastructure uses a 180-day rotation timer. Keys rotate on the next
Terraform apply after that timer expires. The application loads its signing key
and the gateway loads its public keys from environment variables at startup.

During the rollout, signers and gateways can load different key versions. A
verifier can briefly reject a valid assertion even after refreshing on an unknown
`kid`. Keep enforcing signature verification during this window. The five-minute JWKS
cache lifetime and 60-second assertion lifetime are unchanged.

Uninterrupted rotation would require publishing the next public key on every
replica, waiting the five-minute cache lifetime, then switching signers while
retaining the old public key until its last assertions expire. The infrastructure
does not automate that staged rollout.

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
