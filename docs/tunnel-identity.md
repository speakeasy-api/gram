# Signed caller identity for MCP tunnels

Gram can send a signed caller assertion to a private MCP server through its
tunnel. The receiving server validates the assertion using Gram's public JWKS
and uses the verified principal in its own access policy. The assertion does
not contain the user's Gram credentials or replace the receiving server's
authorization rules.

## Configuration

Configure the same values on every Gram MCP serving replica (`gram start`,
`gram mcp`, and `gram network-ingress-server` when used):

| Environment variable     | Value                                                                                                                                                     |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GRAM_AUTHZ_PRIVATE_KEY` | One RSA private key, PKCS#8 or PKCS#1 PEM, at least 2048 bits.                                                                                            |
| `GRAM_AUTHZ_PUBLIC_KEYS` | PEM bundle of public RSA keys in SubjectPublicKeyInfo or PKCS#1 format. Must include the signing key's public half; may include future and retiring keys. |
| `GRAM_SERVER_URL`        | Canonical HTTPS origin used as the JWT issuer, with the trailing slash removed.                                                                           |

Both key variables absent disables issuance. Partial configuration, invalid
keys, or an active private key without a matching public key prevents startup.
No database migration is needed. These keys are separate from
`GRAM_JWT_SIGNING_KEY`, which protects Gram sessions.

Terraform and Helm wiring lives in the separate `gram-infra` repository. The
private key belongs in Secret Manager and is injected into the serving tiers;
the tunnel gateway and customer agent do not need it.

## Wire contract

The HTTP header is `SPEAKEASY_AUTHZ: <JWT>`, without a `Bearer` prefix. Gram
removes client-supplied variants of this header and the `Speakeasy-Authz` alias.
The tunnel gateway and agent transport the minted assertion to the customer
server. The existing upstream OAuth `Authorization` header remains independent.

Header names are case-insensitive. Underscores are significant: if the customer
server is behind an HTTP proxy that drops headers containing underscores,
configure that proxy to preserve `SPEAKEASY_AUTHZ`.

Tokens use RS256 and the protected header `typ: speakeasy-authz+jwt`. The `kid`
is the public key's RFC 7638 SHA-256 thumbprint. Their lifetime is at most 60
seconds, capped by the source credential's expiry where available.

| Claim                           | Meaning                                                                                                                                  |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `version`                       | Contract version, currently `1`.                                                                                                         |
| `iss`                           | Canonical configured Gram origin. Never derived from an incoming Host header.                                                            |
| `aud`                           | `tunneled-mcp-server:<TUNNELED_MCP_SERVER_ID>`. Identifies the destination, independently of gateway address, MCP slug or custom domain. |
| `sub`                           | Typed principal identifier, for example `user:<USER_ID>`.                                                                                |
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
their human creator or owner.

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
session. Existing session credentials do not retain support-impersonation
provenance, so this contract does not assert that impersonation never occurred.

Issuance is limited to private tunneled destinations. Public destinations,
anonymous callers, embedded chat sessions, assistant credentials, workload
sessions and background probes do not receive an assertion in this version.
Servers that require the header must account for those unsupported callers.
Synthetic OAuth keepalive probes are skipped for private tunnels while signing
is enabled, preserving their prior connection verdict. Interactive consent
validation uses the authenticated discovery assertion.

When issuance is enabled, a caller whose authenticated organization or bound
project differs from the destination is rejected before forwarding. In
particular, use an API key scoped to the destination project.

## Verification

Fetch keys from the configured issuer's `/.well-known/jwks.json` endpoint over
HTTPS. The route is public and supports GET, HEAD, ETag and conditional GET. It
advertises a five-minute cache lifetime. When issuance is disabled it returns
`{"keys":[]}`. The route may also be reachable on a custom domain, but verifiers
must use the configured canonical issuer and its JWKS URL. On an unknown `kid`,
refresh that configured JWKS once before rejecting the assertion.

The receiving server must:

1. Read exactly one assertion and select an RSA signing key by `kid` from its
   configured Gram JWKS. Do not follow a token-provided key URL or issuer.
2. Verify the signature with an explicit RS256 allowlist and require
   `typ=speakeasy-authz+jwt` and `version=1`.
3. Require the expected issuer and destination audience. Check organization,
   project and MCP server bindings against its own configuration.
4. Require `iat` and `exp`, reject expired/future-dated tokens, and enforce a
   maximum 60-second lifetime with at most five seconds of clock tolerance.
5. Check the principal type and purpose before applying the customer's access
   policy. Require `purpose=mcp_request` for tool calls; accept `mcp_discovery`
   only for the discovery methods listed above and in `allowed_methods`. Reject
   unknown purposes. A valid signature alone does not authorize a tool call.

Treat a missing assertion as unauthenticated. A `jti` does not make these bearer
tokens replay-proof; a captured token can be reused within its validity window
unless the receiver adds replay protection. Do not put assertions in tool
arguments, ordinary application logs, or error responses.

Validate each HTTP request. Accepting an assertion during initialization does
not authorize later requests on the same MCP session. A response admitted while
the token was valid may continue streaming after its expiry.

## Key rotation

Publish the future key alongside the active key on all replicas first. After
the last old JWKS-serving replica has stopped, wait the full five-minute cache
lifetime before switching signing keys. Keep the retiring public key until all
old signer replicas have stopped and their last tokens have expired, allowing
for clock tolerance. Key removal does not immediately revoke tokens for clients
that still have the old key cached.

## Tool-call records

Tunneled calls use the MCP proxy's existing telemetry. Available caller
attribution, server/tool identifiers, timing and status are recorded when
logging is enabled. Arguments and results additionally require the tool I/O
logging setting. These records are best-effort operational telemetry; they
must not be treated as a guaranteed record of every attempted or rejected call.

The checked-in retention policy keeps raw rows for 90 days. Arguments and
results are each truncated at 64 KiB when tool I/O logging is enabled. The Logs
page and paginated telemetry API provide query access. A project can also
configure the `tool_call_logs` data-export source to stream records to an OTLP
HTTP `/v1/logs` destination. Export delivery is at least once; consumers can
deduplicate using `gram.telemetry.log.id`. The destination's sensitive-data
policy determines whether payloads and user attributes are included.

This identity assertion feature does not change telemetry retention, delivery
or export capabilities. Management audit events are a separate subsystem.
