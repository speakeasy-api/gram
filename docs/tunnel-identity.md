# Signed caller identity for MCP tunnels

Gram can add signed caller identity to requests sent through a tunnel to your
private MCP server. Existing MCP servers can ignore the header and keep their
current authentication. If your server uses these claims for access decisions,
verify the JWT against Gram's public JWKS. The assertion contains no Gram
credentials. Gram enforces its consent and tool-access rules before forwarding.

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

The HTTP header is `X-Speakeasy-Identity: <JWT>`, without a `Bearer` prefix.
Gram removes client-supplied variants of this header, including spellings with
underscores such as `X_Speakeasy_Identity`, in the forwarding proxy before
adding its own assertion.
The tunnel gateway and agent forward the assertion to your server. Upstream
OAuth uses its own `Authorization` header.

Tokens use RS256 and the protected header `typ: speakeasy-authz+jwt`. The `kid`
is the public key's RFC 7638 SHA-256 thumbprint. Their lifetime is at most 60
seconds, capped by the source credential's expiry where available.

| Claim             | Meaning                                                                                                    |
| ----------------- | ---------------------------------------------------------------------------------------------------------- |
| `version`         | Contract version, currently `1`.                                                                           |
| `iss`             | `https://tunnel.speakeasy.com` (AICP).                                                                     |
| `aud`             | The destination's saved resource identifier, or `tunneled-mcp-server:<TUNNELED_MCP_SERVER_ID>` when unset. |
| `sub`             | Typed principal identifier, for example `user:<USER_ID>`. The prefix identifies the principal type.        |
| `email`           | Human user's email from their Gram profile; absent for agents and API keys.                                |
| `allowed_methods` | Present only during consent discovery; methods Gram permits in that context.                               |
| `iat`, `exp`      | Issuance and expiry, Unix seconds.                                                                         |
| `jti`             | Unique assertion identifier for correlation.                                                               |

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

Supported subjects are `user:<USER_ID>`, `api_key:<API_KEY_ID>` and
`agent:<AGENT_ID>`. These are Gram identifiers, not email
addresses or upstream account IDs. API keys and agents are never identified as
their human creator or owner. Human assertions also carry `email`; use the
stable `sub` as the identity key because an email can change. The assertion does
not make an `email_verified` claim.

During OAuth consent, Gram can include the authenticated human's identity with
`allowed_methods=["server/discover", "initialize", "notifications/initialized", "ping", "tools/list"]`.
Runtime assertions omit this claim, so treat a token that carries it as
discovery-only. Gram enforces the method list and blocks tool calls before
forwarding; upstreams can ignore this claim. An upstream's own access policy
still determines the tools it exposes.

Gram issues discovery assertions only within ten minutes of the challenge's
creation, with each assertion valid for at most 60 seconds. Impersonated or
unknown authorizers and agent-selected consent flows receive no discovery
assertion. HTTP `DELETE` used to close the discovery session also carries it.

Runtime user assertions identify the effective user of the validated Gram
session. Session credentials do not record whether support impersonation
occurred, so the assertion cannot rule it out.

Issuance is limited to private tunneled destinations. Public destinations,
anonymous callers, embedded chat sessions, assistant credentials, workload
sessions and background probes do not receive an assertion in this version.
Background connection probes continue using the upstream's existing credentials.
Servers that choose to require the identity header must account for callers
without an assertion. Interactive consent validation includes the authenticated
human's discovery assertion when available.

Gram rejects a request before forwarding if the caller's authenticated
organization or bound project differs from the destination. Use an API key
scoped to the destination project.

## Verification

Fetch public keys from `https://tunnel.speakeasy.com/.well-known/jwks.json`.
Cache them for up to five minutes. On an unknown `kid`, refresh from that URL
once before rejecting the assertion. The endpoint supports GET, HEAD, ETag and
conditional GET.

If your server uses the caller claims:

1. Read exactly one assertion and select an RSA signing key by `kid` from the
   AICP JWKS above. Do not follow a token-provided key URL or issuer.
2. Verify the signature with an explicit RS256 allowlist and require
   `typ=speakeasy-authz+jwt` and `version=1`.
3. Require `iss=https://tunnel.speakeasy.com` and the exact destination audience.
4. Require `iat` and `exp`, reject expired/future-dated tokens, and enforce a
   maximum 60-second lifetime with at most five seconds of clock tolerance.

Gram enforces its consent and tool-access rules before forwarding. Use the
verified caller claims for your server's own access policy.

The default `tunneled-mcp-server:<ID>` audience is unique to your tunneled
server. A saved resource identifier is not: another organization can save the
same identifier, and Gram then issues assertions with that audience to its own
callers. When you use a custom audience, do not accept every valid assertion;
allowlist the `sub` values your policy admits.

If your access policy requires these claims, reject a missing or invalid
assertion. A captured assertion can be reused until it expires, even with a
unique `jti`, unless your server adds replay protection. Keep assertions out of
tool arguments, ordinary application logs, and error responses.

Verify the assertion on each request where you use its claims. An assertion
received during initialization does not cover later requests on the MCP session.
A response admitted while the token was valid may continue streaming after its expiry.

## Key rotation

The JWKS publishes a sliding window of keys: at least the current signing key
and the next one, plus the previous key while replicas may still use it. A new
key is generated about every two months and becomes the signing key in the
following period, so each key signs for about two months.

The application loads its signing key and the gateway loads its public keys
from environment variables at startup, and replicas pick up new versions
independently. The infrastructure checks the live secrets before each change: a
key signs only after it has been in the published bundle for at least 24 hours,
and a key leaves the bundle only after the signing key has been unchanged for at
least 24 hours. Late or skipped infrastructure applies delay rotation rather than
interrupt it.

This keeps every mix of old and new replicas verifying provided each replica
loads a new secret version within 24 hours of its write. Secret sync refreshes
hourly and a changed secret restarts the deployments, so this normally takes
about an hour. A stuck rollout or a failing secret sync can exceed it; treat
either as an incident before the next rotation. Verifiers that cache the JWKS
for five minutes and refresh once on an unknown `kid` then see no gap.

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
