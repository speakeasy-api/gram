# Platform MCP: Remote URL Source Registration

**Date:** 2026-08-21
**Status:** Approved design, pre-implementation
**Scope:** `server/internal/platformmcp/` (+ one new bundled skill, server instructions text)

## Problem

The Platform MCP's guided flow can only register MCP servers that exist in the
reviewed catalogue. Customers who have a remote MCP server URL that is not in
the catalogue (a vendor's hosted server, a private deployment) cannot deploy it
as a source through the guided agent flow at all — they must leave for the
dashboard. We want a guided-flow path for user-supplied remote MCP URLs.

## Decisions already made

These were settled with the product owner during design and are not open:

1. **Trust path: direct admit with guardrails.** No new review gate. A
   user-supplied URL registers immediately as a `remote_url` source, guarded by
   a live verification probe, evidence disclosure, and explicit user
   confirmation. (Registry-match-first and always-through-approval were
   considered and rejected for this iteration.)
2. **Org approval enforcement is respected, not bypassed.** If an organization
   has MCP approval enforcement enabled (`mcpapproval` subsystem), the
   registration lands `blocked_pending_approval` and the tools report that
   state with the dashboard path. The Platform MCP must not be a loophole
   around org policy.
3. **Probe as gate.** Registration requires the URL to verify as a real MCP
   server: a completed initialize handshake, or a typed auth rejection
   (401/403 with `WWW-Authenticate`) which proves an MCP-shaped, auth-walled
   server. Dead URLs, timeouts, and non-MCP endpoints are refused. This
   mirrors the dashboard's remote-source verification
   (`unproxiedmcp.probeListTools`, `externalmcp.NewClient` +
   `AuthRejectedError`).
4. **Tool shape: probe/register pair bound by a server-issued receipt**
   (approach 3 of 3). The mutating tool never accepts a raw URL.

## What already exists (load-bearing prior art)

- `RegistrationStore.CompleteRegistrationWithRemoteURL`
  (`registration.go`) — a currently caller-less persistence entry point that
  provisions the full private component set (remote MCP server row with URL,
  user session issuer, MCP server, MCP endpoint) from a bare URL.
- `inventory.go` already maps `source_kind = "remote_url"` →
  `"user_supplied_url"` for display.
- `remoteprobe` (in `mcpapproval`) — credential-free probe: OAuth metadata via
  RFC 9728/8414 well-knowns, unauthenticated `tools/list`.
- Signed-token codec pattern and `cursorKeyMaterial` (`catalog_cursor.go`).
- Guardian egress policy, operation budgets, `OrganizationGate` two-key
  rollout pattern (PostHog flag + durable product feature), unavailable-stub
  tool registration.

## Architecture

### Tool: `probe_remote_mcp` (read-only, `tool_probe_remote.go`)

Input: `remote_url` only. (No project selector: the probe mutates nothing
project-scoped and its budget is per-connection.)

1. Validate URL shape **before any network I/O**: `https` only, no userinfo,
   no fragment. Normalize: lowercase host, strip default port.
2. Probe through the guardian policy, one-shot, bounded timeout, no retries
   (same discipline as `unproxiedmcp.probeListTools`): MCP initialize
   handshake over streamable HTTP, `tools/list`, and OAuth discovery via
   RFC 9728/8414 well-knowns.
3. Verification outcome:
   - handshake completed → verified
   - `AuthRejectedError` (401/403 + `WWW-Authenticate`) → verified, posture
     `auth_required`
   - anything else → refused, no receipt
4. On success, return **evidence** (server name/version from initialize, tool
   count and names, auth posture `oauth_discovered` / `auth_required` /
   `open`, explicit gaps such as "server declined unauthenticated tools/list")
   **plus a signed probe receipt**: HMAC over normalized URL + probe digest +
   connection ID + expiry (~10 minutes), built on the `cursorKeyMaterial`
   codec pattern.

The probe gets its **own operation budget, tighter than read budgets**: an
unbounded probe tool is an SSRF/port-scan primitive with Gram as the egress
point. Guardian bounds _where_; the budget bounds _how often_.

### Tool: `register_remote_mcp_for_project` (mutating, `tool_register_remote.go`)

Input: `project_slug`, `probe_receipt`, `idempotency_key`, optional
`display_name`. **No URL parameter** — the URL travels only inside the
receipt, preserving the subsystem invariant that mutations accept only
server-issued identities (catalogue refs, probe receipts).

Mirrors `RegisterCatalogMCP`'s spine:

1. operation budget → org gate → **receipt validation** (signature, expiry,
   connection binding) → project resolution + eligibility
2. `BeginReceipt` / `ConvergeRegistration` /
   `CompleteRegistrationWithRemoteURL` with `SourceKind: "remote_url"`
3. Registration row: `catalog_provider = "remote-url"` (sentinel),
   `catalog_reference = <normalized URL>`. This flows unchanged into the
   existing audit event and `McpKey` derivation.
4. Post-registration, consult org approval enforcement; if the server is not
   approved under active enforcement, report `blocked_pending_approval` with
   the dashboard approvals path.

### Registration and rollout wiring (`tools.go`)

- Both tools register with unavailable-stub counterparts so the surface does
  not flicker during rollout.
- Audience: `externalOnly` initially (the assistant cannot complete secure
  setup).
- Rollout: existing `FeaturePlatformMCP` capability **plus** a new PostHog
  flag specific to this surface, matching the `OrganizationGate` two-key
  pattern.

## Downstream lifecycle integration

Goal: after registration, the lifecycle is identical to the catalogue flow —
same tools, same states. Source kind is a registration-row concern; downstream
tools see only the four provisioned components.

- **Onboarding status**: works off registration row + components (same shape).
  One addition: `blocked_pending_approval` next step with the dashboard
  approvals path when enforcement blocks the server.
- **Identity provider attachment**: for `remote_url`, derive the provider from
  **live RFC 9728/8414 discovery against the persisted remote server URL**
  (from the `remote_mcp_servers` row — server-owned state, never chat input).
  The probe's discovery snapshot is advisory evidence only. No OAuth metadata
  discovered → report that bounded fact; setup falls through to the dashboard.
- **Setup handoff**: maps to the existing remote MCP server Authentication
  settings dashboard surface. **v1 collects no headers in chat, not even
  non-secret ones** — a raw URL declares no configuration fields (unlike
  reviewed catalogue entries), and `remoteprobe` cannot discover install-time
  credential needs. The dashboard is the header surface.
- **Distribution** (`add_platform_mcp_to_default_plugin`): gains an explicit
  enforcement gate. The design's original premise — "enforcement-blocked
  registrations never reach fresh-ready, so readiness is the chokepoint" —
  was disproven in review: an open server that answers anonymous `tools/list`
  reads as Ready regardless of approval state. `Distribute` therefore
  consults org approval enforcement itself (fail closed) and refuses with a
  typed `blocked_pending_approval` error for enforcement-blocked
  `remote_url` sources; this covers both the MCP tool and the dashboard
  management path, which share `Distribute`.

## Error handling

Typed, bounded results following the `operationBudgetResult` pattern:

| Code                | Meaning                                                                    | Receipt issued |
| ------------------- | -------------------------------------------------------------------------- | -------------- |
| `invalid_url`       | fails shape rules pre-network                                              | no             |
| `egress_denied`     | guardian refusal (private ranges, denied hosts); no resolver detail echoed | no             |
| `unreachable`       | connect/timeout failure                                                    | no             |
| `not_an_mcp_server` | connected but handshake failed                                             | no             |
| `auth_required`     | 401/403 + `WWW-Authenticate`                                               | **yes**        |
| `rate_limited`      | probe budget exhausted                                                     | no             |

Receipt validation errors: `receipt_expired` (remedy: re-probe),
`receipt_invalid` (signature/tamper), `receipt_context_mismatch` (different
connection than the probing one).

Duplicate URL registrations follow catalogue semantics unchanged:
idempotency-key replay returns the original; a genuinely new registration
counts against the active-registration cap.

## Safety instruction and skill updates

The server `Instructions` string (`tools.go`) and the bundled skills currently
state a blanket "never accept an MCP endpoint in chat." That rule gets a
precise carve-out, not a deletion:

> A user-supplied URL is accepted by exactly one tool, the read-only
> `probe_remote_mcp`. Every mutation takes server-issued identities only
> (catalogue refs or probe receipts). Probe evidence must be shown to the user
> and explicitly confirmed before registering. Secrets stay out of chat
> unconditionally.

A new sibling skill `add-mcp-from-remote-url` lands in
`server/internal/plugins/platform_mcp_skills/` (following
`authoring-platform-mcp-skills` conventions); `add-mcp-from-catalog` gets one
routing line pointing to it.

**Audit:** reuse `LogPlatformMcpRegistrationCreate` with the sentinel
provider + normalized URL — no new `audit.Action`, hence no dashboard TS
mirror update.

## Testing

- **Receipt codec** (`probe_receipt_test.go`, modeled on
  `catalog_cursor_test.go`): round-trip, expiry, tamper, connection mismatch,
  cross-project reuse. This is the security boundary; it gets the exhaustive
  table.
- **URL validation** table tests: https-only, userinfo/fragment rejection,
  normalization, guardian-denied via stubbed policy interface.
- **Probe classification** against an in-process fake MCP server (`httptest`,
  streamable HTTP — pattern from `externalmcp`/`unproxiedmcp` tests): clean
  handshake → receipt; auth-walled → receipt with `auth_required`; refused /
  non-MCP → typed refusal, no receipt.
- **Registration service** (mirroring `registration_service_test.go`): gate
  off, budget exhausted, expired/invalid receipt, cap, ineligible target,
  idempotent replay, row assertion (`source_kind`, sentinel provider,
  normalized URL), audit event assertion.
- **Integration**: probe → register → onboarding status → distribution
  refuses until ready; enforcement-on variant asserts
  `blocked_pending_approval` in status while distribution stays unavailable.
- **Lifecycle non-regression**: a `remote_url` registration flows through
  readiness + IdP attachment via the discovery path without the
  catalogue-identity path being consulted.

Runner: `mise run test:server ./internal/platformmcp/`.

## Out of scope (this iteration)

- Header/config collection in chat (dashboard only).
- Assistant audience admission.
- Registry matching of user URLs to catalogue entries (rejected trust-path
  option; could be layered later as an optimization).
- Any new approval/review gate beyond existing org enforcement.
