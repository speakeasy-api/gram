# Private Network Ingress for Gram-Hosted MCP Servers

**Status:** Proposed — pending Phase 0 spike
**Provider:** Tailscale is the first (and MVP-only) supported provider; the data model, gateway, and API are provider-neutral. A fuller multi-provider RFC lives in the internal RFCs database (Notion).
**Scope:** Ingress-side network controls (who can reach a Gram-hosted MCP server). Egress (Gram reaching customer-private upstreams over an overlay network) is explicitly out of scope; the existing tunnel feature covers that direction.

## Summary & recommendation

**Build, behind a new `network_ingress` product feature (Enterprise tier), contingent on a one-week Phase 0 spike.** Estimated effort after the spike: ~4–6 engineer-weeks to MVP, ~3–4 more to GA hardening.

Today Gram's only MCP network control is the IPv4 allowlist on an org's custom domain (`custom_domains.ip_allowlist`), enforced at the nginx ingress (`whitelist-source-range`) and backstopped by an app-level lockdown that 403s platform-host access for allowlisted orgs. This proposal adds a second, stronger control configured from the same page: serving an org's MCP endpoints **directly on the customer's private overlay network** (first provider: a Tailscale tailnet), optionally disabling public access entirely.

Gram runs one embedded overlay node per opted-in organization inside a new dedicated deployment, **`network-gateway`**, structurally mirroring the existing `tunnel-gateway` (separate binary, own Postgres/Redis access, forward-token trust boundary into gram-server). For the Tailscale provider the node is a `tsnet` instance that joins the customer's tailnet using a customer-supplied credential (OAuth client preferred, raw auth key supported) and reverse-proxies the full MCP HTTP surface (`/mcp/*`, `/.well-known/*`, OAuth, install pages) into gram-server over the cluster network, carrying trusted `X-Gram-NetIngress-*` headers (ingress ID, per-request network identity, forward token).

The one genuinely novel risk is **overlay node state persistence and per-node memory at multi-tenant scale** — that is what the spike de-risks. Everything else maps onto proven in-repo patterns: the tunnel gateway, the custom-domain lockdown, `productfeatures`, the `encryption` client, and the Goa management-API recipe.

## Provider abstraction

The gateway supervises nodes through two small interfaces so Tailscale specifics stay in one package:

```go
type Provider interface {                  // one per overlay technology
    Name() string                          // "tailscale"
    NewNode(ctx, IngressConfig, StateStore) (Node, error)
    ValidateCredential(ctx, Credential) error
}

type Node interface {                      // one running overlay endpoint
    Start(ctx) error                       // join network, begin listening
    Listener() (net.Listener, error)       // TLS or plain, provider-decided
    Identity(remoteAddr) (*PeerIdentity, error)  // per-request caller identity, nil if unsupported
    Status() NodeStatus                    // online/offline/error + provider detail
    Close(ctx) error                       // leave network, release state
}
```

- **Tailscale** (MVP): `tsnet` — userspace WireGuard + netstack in-process, `LocalClient.WhoIs` implements `Identity`, custom `ipn.StateStore` for durable node identity.
- **Later candidates**, in rough order of fit: **Headscale** (self-hosted Tailscale control plane — same tsnet client, different `ControlURL`; nearly free once Tailscale ships), **NetBird**, **OpenZiti**, **Nebula**, plain **WireGuard**. Each lands as a `provider/<name>` package; no schema or API change beyond a new `provider` enum value.
- `PeerIdentity` (login, node/device name, tags, capability strings) is the provider-neutral shape behind the `X-Gram-NetIngress-User-*` headers; providers that cannot attest identity return nil and `identity_required` ingresses reject at the gateway.

## Why Tailscale first, and why this shape

- The IP allowlist is a real control but a coarse one: it requires customers to have stable egress IPs, it is IPv4-only, and the server remains publicly reachable (DNS resolves, TLS handshakes complete, only the HTTP layer is filtered at nginx).
- Serving on the customer's overlay network means the MCP server is **not publicly reachable at all** for `private_network_only` orgs: no public DNS name in use, no public listener. Access is governed by the customer's own network ACLs/grants, and (on Tailscale) every request arrives with a cryptographically-bound device/user identity (`WhoIs`) usable for audit and, later, authorization.
- `tsnet` embeds a full Tailscale node in-process (userspace WireGuard + gVisor netstack): no daemon, no root, multiple independent nodes per binary, and a pluggable `ipn.StateStore` for node identity — which makes a stateless-container deployment feasible.

## Architecture

### Where overlay nodes run

**Decision: a new dedicated deployment `netgateway/` (binary `network-gateway`), mirroring `tunnel/`.** Embedding in gram-server was rejected:

- **Device multiplication.** Every gram-server replica embedding an org's node would appear as a separate device on the customer's network (replicas × orgs devices, each consuming a machine slot and confusing the customer's admin console). A dedicated gateway with org→replica assignment keeps it to one device per org.
- **Memory blast radius.** Each tsnet node is a userspace WireGuard + netstack instance; realistic RSS is tens of MB per node (the spike measures this — no official benchmark exists; full `tailscaled` is ~72MB RSS). Hundreds of orgs would bloat the latency-sensitive MCP serving path. The gateway scales and caps independently (`NETWORK_GATEWAY_MAX_NODES`, mirroring the tunnel gateway's `MaxSessions`).
- **Dependency weight.** `tailscale.com` is a very large dependency tree (wireguard-go, gVisor). The repo is a single Go module, so it lands in the shared `go.mod` either way — but only the gateway binary links it. Accept root-module placement for MVP (exactly how `tunnel/` works); escape hatch: split into a nested module later if CI/dep-scan pain materializes. Add a depguard/glint rule confining `tailscale.com/...` imports to `netgateway/provider/tailscale/`.
- **Deployment cadence.** Provider library bumps, node restarts, and provider control-plane incidents should not roll gram-server.

Layout (mirrors `tunnel/`):

```
netgateway/
  cmd/network-gateway/main.go       # env config; mise build:network-gateway; pitchfork daemon
  gateway/supervisor.go             # org lease acquisition, node lifecycle (start/stop/reconfigure)
  gateway/node.go                   # Provider/Node interfaces + shared lifecycle wrapper
  gateway/statestore.go             # Postgres-backed state store (ipn.StateStore for tailscale)
  gateway/proxy.go                  # httputil.ReverseProxy → gram-server internal URL
  gateway/credentials.go            # decrypt provider credentials; tailscale: mint auth keys via API
  provider/tailscale/               # tsnet.Server wrapper, WhoIs → PeerIdentity, cert handling
  wire/headers.go                   # X-Gram-NetIngress-* header constants (shared with server)
```

### Sharding / HA model

Redis-leased org ownership, in the spirit of the tunnel route liveness pattern (`tunnel_routes:*`, 30s TTL, 15s republish):

- Each gateway replica watches the set of enabled `network_ingresses` rows (poll ~15s plus a Redis pub/sub nudge published by gram-server on create/update).
- For each unowned ingress, a replica attempts `SET net_ingress_owner:<ingress_id> <replica_id> NX PX 45000`; the winner starts the node and heartbeats the lease every 15s. On replica death the lease expires and another replica claims it, loading node state from the Postgres state store — the node resumes as the **same network device** (same node key): no re-auth, no duplicate device.
- Rendezvous hashing (reuse `tunnel/wire.RendezvousPick`) over live replica IDs biases claims for even spread; the lease remains the source of truth.
- **MVP simplification: one replica** (leases still written so the code path is exercised); multi-replica failover lands in Phase 2.

Coordination is DB-as-source-of-truth plus the Redis nudge. gram-server **never dials the gateway** — traffic flows gateway→gram-server only — so there is **no guardian/SSRF change anywhere** (contrast `tunnel_manager.go:90`, where gram-server dials the tunnel gateway and must relax the blocklist).

### Request flow

```
MCP client on customer overlay network
  └─ https://gram-mcp.<tailnet>.ts.net/mcp/<slug>     (tailscale: ts.net TLS via tsnet, or WireGuard-only HTTP)
     └─ network-gateway (org's overlay node)
        1. node.Identity(remoteAddr) → user login, node name, tags, capability grants
           (tailscale: LocalClient.WhoIs)
        2. ingress.identity_required && no identity → 403 at the gateway
        3. Strip any inbound X-Gram-NetIngress-*; set:
             X-Gram-NetIngress-Forward-Token   (shared secret)
             X-Gram-NetIngress-Ingress-ID
             X-Gram-NetIngress-User-Login / -Node / -Caps   (from Identity)
           Preserve Host; set X-Forwarded-Proto/For.
        4. ReverseProxy → http://gram-server.<ns>.svc   (in-cluster; NOT via public nginx)
     └─ gram-server middleware chain (server/cmd/gram/start.go, ~line 1242)
        5. NEW netingress.Middleware (ordered immediately BEFORE customdomains.Middleware):
           - requests lacking a valid forward token (constant-time compare) have all
             X-Gram-NetIngress-* headers stripped — this also sanitizes the public nginx path
           - valid token: load network_ingresses row (cached), 403 if disabled/deleted,
             attach netingress.Context{IngressID, OrganizationID, PeerIdentity...}
        6. customdomains.Middleware: netingress ctx present → pass through (a .ts.net Host is
           unknown to custom_domains and would otherwise 403 at middleware.go:46-50).
           If the org has an active custom domain, synthesize customdomains.Context from it
           so mcp_endpoints resolve in the org's custom-domain namespace.
        7. mcp.ServePublic → ResolveMCPEndpointAndServer
        8. NEW invariant — post-resolution org check: resolved endpoint's project.org MUST
           equal netingress.Context.OrganizationID, else 404.
           (Load-bearing tenant isolation, esp. for orgs without a custom domain.)
        9. enforceNetworkLockdown (generalized enforceCustomDomainLockdown,
           serveendpoint.go:64): netingress ctx present → allow.
       10. Normal dispatch: issuer gate, visibility/authz.MCPCheck(ScopeMCPConnect),
           backend switch (remote / tunneled / toolset). Peer identity attached to
           logs/audit attrs.
```

### Lockdown matrix

`enforceCustomDomainLockdown` generalizes to `enforceNetworkLockdown`:

| Request arrives via              | IP allowlist set | `private_network_only` | Result                               |
| -------------------------------- | ---------------- | ---------------------- | ------------------------------------ |
| platform host                    | no               | off                    | allow                                |
| platform host                    | yes              | off                    | 403 (today's behavior, unchanged)    |
| custom domain                    | any              | off                    | allow (nginx enforced the allowlist) |
| platform or custom domain        | any              | **on**                 | **403**                              |
| private network (netingress ctx) | any              | any                    | allow                                |

The install-page / well-known platform-host bypass is kept unchanged (see the deliberate comment at `serveendpoint.go:58-63`: private-MCP install pages need the dashboard session cookie on the platform host). Those routes are _also_ fully served over the private network, since the gateway proxies every path. `BaseURLForRequest` derives from `r.Host`, which the gateway preserves, so OAuth issuer/metadata URLs correctly come out as `https://gram-mcp.<tailnet>.ts.net/...` — this is what keeps the OAuth flows working untouched (verified by Phase 1 tests).

## Data model

### `network_ingresses` (new table; one per org, like `custom_domains`)

```sql
CREATE TABLE network_ingresses (
  id uuid PRIMARY KEY DEFAULT generate_uuidv7(),
  organization_id TEXT NOT NULL,
  provider TEXT NOT NULL DEFAULT 'tailscale' CHECK (provider IN ('tailscale')),
  hostname TEXT NOT NULL DEFAULT 'gram-mcp',        -- overlay device hostname
  -- credentials (provider-specific; exactly one mode; AES-256-GCM via server/internal/encryption)
  credential_kind TEXT NOT NULL CHECK (credential_kind IN ('auth_key','oauth_client')),
  auth_key_enc TEXT,                                 -- one-shot; nulled after first successful join
  oauth_client_id TEXT,
  oauth_client_secret_enc TEXT,
  tags TEXT[] NOT NULL DEFAULT '{tag:gram}',         -- ACL tags the node advertises
  -- behavior flags
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  private_network_only BOOLEAN NOT NULL DEFAULT FALSE, -- "disable public access"
  identity_required BOOLEAN NOT NULL DEFAULT TRUE,
  -- learned/health state (written by the gateway; pattern: custom_domains health columns)
  status TEXT NOT NULL DEFAULT 'pending',            -- pending|connecting|online|offline|error|disabled
  network_name TEXT,                                 -- tailscale: e.g. tail1234.ts.net
  dns_name TEXT,                                     -- tailscale: gram-mcp.tail1234.ts.net (MagicDNS)
  node_id TEXT,
  last_error TEXT,
  last_seen_at timestamptz,
  connected_since timestamptz,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted BOOLEAN GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED
);
CREATE UNIQUE INDEX ON network_ingresses (organization_id) WHERE deleted IS FALSE;
```

Decisions:

- **New table, not `custom_domains` columns.** Independent lifecycle and secrets, and the private-network path does not use the domain's DNS; an org can have a network ingress with no custom domain at all.
- **`provider` is an enum-by-CHECK.** Only `tailscale` in MVP; new providers extend the CHECK and add a `provider/<name>` gateway package. Credential columns are provider-specific; if a second provider's credential shape diverges hard, migrate to a `credential jsonb` at that point, not before.
- **OAuth client is the recommended Tailscale credential.** Auth keys are single-use and expire (≤90 days) — any state loss or expiry in auth-key mode strands the node and generates support tickets. An OAuth client (scope `auth_keys`, tied to `tag:gram`) lets the gateway mint fresh tagged auth keys on demand via the Tailscale API. MVP accepts both; the UI nudges toward OAuth.
- **Quota:** one per org via the unique index. If per-org node counts ever grow, reuse the `billing_metadata` pattern (`tunneled_mcp_server_limit` precedent).
- **Secrets** encrypted with the existing `encryption.Client` (AES-256-GCM, `server/internal/encryption`). The gateway gets `GRAM_ENCRYPTION_KEY` and its own DB access (precedent: `tunnel/gateway/pgkeystore.go`). Secrets are never returned by RPCs; rotation is write-only.

### `network_node_state` — Postgres-backed state store (spike target)

`tsnet.Server` accepts a custom `Store ipn.StateStore` (a small `ReadState`/`WriteState` KV interface) holding node identity (machine key, node key, profile) — this is what makes lease failover resume the same device with no PVCs. The table is provider-neutral KV; the tailscale provider is its first client.

```sql
CREATE TABLE network_node_state (
  ingress_id uuid NOT NULL,
  key TEXT NOT NULL,
  value bytea NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (ingress_id, key)
);
```

Caveat to validate in the spike: tsnet also uses a local `Dir` for cert cache and logs. ts.net TLS certs are re-obtainable, so an ephemeral emptyDir should suffice — confirm current tsnet versions tolerate `Dir` loss with `Store` intact, and measure cold-resume time.

## API surface

**New Goa service `networkIngress`** (not an extension of `domains` — different secrets and lifecycle; keeps the `domains` SDK surface stable). Per the `gram-management-api` recipe: `server/design/networkingress/design.go` + blank import in `server/design/gram.go`; impl at `server/internal/networkingress/{impl.go,queries.sql}` + repo + `mv` views + changeset.

Security: `Session` + `ByKey`, org-level (no `ProjectSlug`, same as `domains`); mutations require `authz.ScopeOrgAdmin`; every method is gated on the `network_ingress` product feature.

| Method              | Notes                                                                   |
| ------------------- | ----------------------------------------------------------------------- |
| `getIngress`        | Row + health; secrets redacted to "credential configured" booleans      |
| `createIngress`     | provider, hostname, credential (auth key XOR oauth client), tags, flags |
| `updateIngress`     | hostname / `private_network_only` / `identity_required` / `enabled`     |
| `rotateCredentials` | Replace credential; mark node for re-auth. Write-only                   |
| `deleteIngress`     | Soft-delete; gateway logs the node out and purges node state            |
| `checkHealth`       | On-demand freshness poke (mirrors `domain.checkHealth`)                 |

## UI changes

Extend `client/dashboard/src/pages/org/OrgDomains.tsx` with a second card under the existing domain card, matching its idioms (SettingsPage, Sheet for credential entry, Dialogs for destructive actions, health banners). New components under `client/dashboard/src/pages/org/netingress/`.

- **Not configured:** "Serve MCP over your private network" + Configure button (provider picker shows Tailscale only in MVP); hidden/upsell when the product feature is off (mirrors `canCreateCustomDomain` gating and the `FeatureRequestModal` pattern).
- **Configured:** status badge (Online/Offline/Error + `last_seen_at`), network name, copyable MCP base URL `https://<dns_name>/mcp/<slug>`, hostname edit, toggles for **Private-network-only access** (strong confirm dialog — it 403s the custom domain too) and **Require network identity**, credential-rotation Sheet (secret never displayed), delete dialog.
- Setup Sheet includes the customer-side steps (tailscale): create `tag:gram` in ACLs, create an OAuth client scoped to it, optionally enable MagicDNS/HTTPS.

## Security analysis

1. **Forward-header trust boundary.** Same shape as `TUNNEL_GATEWAY_FORWARD_TOKEN`: shared secret env on both deployments, constant-time compare. Stronger than the tunnel path: `netingress.Middleware` _unconditionally strips_ `X-Gram-NetIngress-*` from any request without a valid token, so headers injected via the public nginx path are inert. Defense in depth: also drop these headers at the nginx edge.
2. **Identity spoofing.** Identity headers are only produced by the gateway post-token-validation; overlay clients cannot set them (the gateway strips inbound copies). Peer identity is advisory in MVP (log/audit attrs); it becomes authz-bearing only in Phase 2 via provider capability grants (tailscale: e.g. `getgram.ai/cap/mcp` in customer ACLs mapped to allowed server slugs), validated against the ingress's org.
3. **Tenant isolation.** The post-resolution org check (step 8 above) is the critical invariant: a private-network request can only ever resolve endpoints in its own org, including on the legacy platform-namespace fallback path. Requires a dedicated test.
4. **SSRF/guardian.** No relaxation anywhere: gateway→gram-server is inbound to gram-server, and gram-server never dials the customer network.
5. **Private-network-only lockdown** is enforced app-level in `enforceNetworkLockdown` (403 on both platform and custom-domain contexts). The install-page/well-known platform-host bypass remains — document that `private_network_only` governs the MCP data plane, not the dashboard-cookie install page.
6. **Credential compromise/rotation.** Secrets AES-GCM at rest; rotation RPC; docs must state that customers should also revoke the credential and delete the device in their provider's admin console (Gram cannot revoke customer-side). On delete: gateway logs the node out (tailscale: tsnet logout) and purges `network_node_state`.
7. **Customer ACL changes.** If the customer's network policy cuts the node off, traffic simply stops; health goes offline with `last_error`; no Gram-side failure mode. Tag deletion breaks OAuth-minted keys — surfaced as `error` status.
8. **OAuth/install flows over the private network.** Full-surface proxying + Host preservation keeps `/.well-known/*`, the issuer gate, and consent/install pages working; the end user's browser is on the customer network by definition of the feature. Third-party IdP redirects leave the private network from the browser — fine.

## Feasibility — the "if"

Costs and risks, honestly:

- **Operational:** Gram becomes an operator of N always-on devices in N customer-controlled networks. Failure modes (expired keys, ACL changes, provider control-plane outages) originate customer-side but page Gram. Mitigate with rich `status`/`last_error` surfacing and docs. This is the largest ongoing cost — hence Enterprise-only gating.
- **Memory/scale:** estimated 30–100MB RSS per tsnet node (spike measures). At ~50 enterprise orgs that is a few GB in a dedicated deployment — acceptable; at thousands it needs the Phase 2 sharding maturity the lease model already provides.
- **Control-plane dependency:** joins and re-auth depend on the provider's coordination servers; established WireGuard sessions largely survive short outages.
- **Library churn:** `tailscale.com` releases frequently; pin and schedule quarterly bumps; nested-module escape hatch if root `go.mod` churn hurts.

Fallback positions (worth documenting regardless — they are the day-1 answer for un-entitled orgs):

1. **Customer-side Tailscale app connector / subnet router + existing IP allowlist:** the customer routes their custom domain through their overlay egress and allowlists that egress IP. Zero Gram code — a docs page. Weaker: the server stays publicly reachable and protection is IP-based.
2. **tsidp + existing issuer gate:** point `mcp_servers.user_session_issuer_id` at Tailscale's OIDC IdP (tsidp) running on the customer's tailnet — network _identity_ without network _transport_. Complementary, not a substitute.

**Verdict: build behind the `network_ingress` product feature**, MVP scoped to one node per org, the Tailscale provider only, and a single gateway replica, contingent on Phase 0 confirming the Postgres StateStore behavior and memory numbers. If the spike falsifies the StateStore assumption, the fallback is per-node state on a StatefulSet PVC — uglier, not fatal.

## Phased plan

### Phase 0 — Spike (~1 week, throwaway, no product code)

- Scratch binary running 3+ tsnet nodes in one process against a test control plane; measure RSS/goroutines per node.
- Implement the Postgres-backed `ipn.StateStore`; kill/restart the process; confirm same-device resume with an ephemeral `Dir`; measure resume latency.
- Validate `LocalClient.WhoIs` fields including capability grants; validate ts.net HTTPS cert issuance and cert-cache-loss tolerance (Let's Encrypt rate limits).
- Validate OAuth-client → tagged auth-key minting via the Tailscale API.
- Output: go/no-go memo with measured sizing.

### Phase 1 — MVP (~3–4 weeks)

1. Migration: `network_ingresses` + `network_node_state` (postgresql skill / atlas flow).
2. Goa service `networkIngress` + impl/repo/mv/tests (gram-management-api recipe); `network_ingress` product feature (4-step productfeatures recipe); `org:admin` RBAC.
3. `netgateway/` binary (single replica; leases written): supervisor, Provider/Node interfaces + tailscale provider, PG state store, identity headers, reverse proxy, forward token; `mise build:network-gateway`; pitchfork entry; Dockerfile mirroring `tunnel/Dockerfile`; depguard rule confining `tailscale.com`.
4. gram-server: `server/internal/netingress/{context,middleware}.go`; `customdomains.Middleware` pass-through + domain-ctx synthesis; `enforceCustomDomainLockdown` → `enforceNetworkLockdown`; post-resolution org check; wiring + flags in `start.go`.
5. Dashboard card on `OrgDomains.tsx` (status, credential Sheet, toggles).
6. Tests: middleware token/strip tests (pattern: `customdomains/middleware_test.go`); lockdown matrix tests; org-isolation test; optional e2e job against a real test tailnet.

### Phase 2 — Hardening / GA (~3–4 weeks)

- Multi-replica gateway with lease failover + rendezvous claim bias; chaos test (replica kill → device-resume SLO).
- OAuth-client auto re-auth (mint fresh keys on expiry); rotation UX polish.
- Identity → audit-log integration (gram-audit-logging pattern); optional capability-grant authz.
- Health reconciler parity with the custom-domain health workflow (background Temporal check, dashboard banners, `checkHealth` RPC).
- Metrics/alerts (node up, handshake age, proxy error rate), runbook, customer docs, `tailscale.com` upgrade policy.
- Nested-module split decision based on observed dependency pain.
- Second provider (likely Headscale — same client stack) to prove the `Provider` seam before it ossifies.

## Open questions

1. Does the current tsnet release route cert storage through `ipn.StateStore`, and is Dir-loss cert reissue safe against Let's Encrypt rate limits? (Spike answers.)
2. Should `private_network_only` also suppress the platform-host install-page bypass, given dashboard cookies live off-network? MVP: keep the bypass, document it.
3. TLS posture on the overlay: ts.net HTTPS (requires the customer to enable MagicDNS + HTTPS) vs plain HTTP over WireGuard. MVP: prefer HTTPS, warn on fallback — needs PM sign-off.
4. Multiple nodes/networks per org (staging vs prod)? MVP: one per org (unique index); relaxing later mirrors how custom domains might go multi.
5. Local-dev story: `customdomains.Middleware` short-circuits on `env == "local"`; netingress middleware should still be exercisable locally against a dev network — needs a dev-flag design. (The Phase 0 spike's local Headscale setup is a candidate answer.)

## Key files referenced

| Area                                 | Path                                                                                   |
| ------------------------------------ | -------------------------------------------------------------------------------------- |
| Lockdown to generalize               | `server/internal/mcp/serveendpoint.go` (`enforceCustomDomainLockdown`, line ~64)       |
| Middleware ordering / wiring         | `server/cmd/gram/start.go` (~line 1242)                                                |
| Custom-domain host resolution        | `server/internal/customdomains/middleware.go`                                          |
| Deployment + trust-boundary template | `tunnel/cmd/tunnel-gateway/main.go`, `tunnel/gateway/`                                 |
| Goa service template                 | `server/design/tunneledmcp/design.go` + `.agents/skills/gram-management-api/SKILL.md`  |
| Secret encryption                    | `server/internal/encryption/encryption.go`                                             |
| Product feature recipe               | `server/internal/productfeatures/features.go` + `.agents/skills/feature-flag/SKILL.md` |
| Custom domain page (UI host)         | `client/dashboard/src/pages/org/OrgDomains.tsx`                                        |
