# Private Network Ingress for Gram-Hosted MCP Servers

**Status:** Revised 2026-08-24 — architecture pivoted to the Tailscale Kubernetes operator per architectural review; Phase 0 spike (embedded tsnet) validated the underlying primitives and its results carry over where noted. A fuller product RFC lives in the internal RFCs database (Notion).
**Provider:** Tailscale is the first (and MVP-only) supported provider; the data model, router abstraction, and API are provider-neutral.
**Scope:** Ingress-side network controls (who can reach a Gram-hosted MCP server). Egress (Gram reaching customer-private upstreams over an overlay network) is explicitly out of scope; the existing tunnel feature covers that direction.

## Summary & recommendation

**Build, behind the `network_ingress` product feature (Enterprise tier), on top of the Tailscale Kubernetes operator — not a hand-rolled gateway.** An org's private ingress becomes a set of Kubernetes resources provisioned through the same router abstraction that serves custom domains today: the existing `CustomDomainProvisioner` seam gains a second provider, `tailscale-ingress`, alongside today's `nginx-ingress`.

Today Gram's only MCP network control is the IPv4 allowlist on an org's custom domain (`custom_domains.ip_allowlist`), enforced as an nginx Ingress annotation. This proposal adds a second, stronger control configured from the same page: serving an org's MCP endpoints **directly on the customer's Tailscale tailnet**, optionally disabling public access entirely (`private_network_only`).

The Tailscale Kubernetes operator (v1.96+) supplies the entire data plane:

- **Multi-tenancy is first-class.** A cluster-scoped `Tailnet` CR references per-tailnet OAuth credentials (a Kubernetes Secret in the operator's namespace); a `ProxyGroup` pins to a `Tailnet` via its immutable `spec.tailnet`; `ProxyGroupPolicy` fences which namespaces may use which ProxyGroup. One operator install serves every customer tailnet.
- **L7 ingress with identity.** An `Ingress` with `ingressClassName: tailscale` (annotated onto the org's ProxyGroup) gets a MagicDNS hostname, an automatically provisioned Let's Encrypt certificate, and proxy pods running `tailscale serve` — which injects `Tailscale-User-Login`-family identity headers into forwarded requests.
- **State and HA are the operator's problem.** Proxy node identity lives in operator-managed Kubernetes Secrets; HA is ProxyGroup replicas. The custom Postgres `ipn.StateStore`, Redis leases, and supervisor from the earlier design are all deleted scope.

What remains ours: the router-provider abstraction, per-org resource provisioning and lifecycle (reusing the custom-domain Temporal reconcile pattern), the management API and dashboard, the serving-path attribution and lockdown enforcement in gram-server, and the tenant-isolation invariant.

## What the Phase 0 spike still tells us

The spike (2026-08-21, embedded tsnet, results in the appendix) predates this revision but validated primitives the operator path still relies on: tailnet join and same-device identity semantics, WhoIs-attested per-request identity (login, display name, device), the single-use-auth-key footgun, and per-node resource costs (~5MB idle RSS — now the operator's budget, not ours). What it validated that we **no longer need**: the Postgres StateStore and unclean-death resume machinery (operator Secrets + ProxyGroup replace it). A new, smaller validation spike targets the operator itself (see Phase 0b below).

## Architecture

### The router provider abstraction

The seam already exists. Custom domains are provisioned through `server/internal/k8s`:

```go
type CustomDomainProvisioner interface {   // server/internal/k8s/provisioner.go:44
    Kind() ProvisionerKind
    Apply(ctx context.Context, config RouteConfig) (SetupResult, error)
    Get(ctx context.Context, resourceName string) error
    Delete(ctx context.Context, resourceName, secretName string) error
}
```

with exactly one real implementation today (`IngressProvisioner`, hardcoded `ingressClassName: nginx`) and a factory that currently ignores its `kind` argument (`client.go:83-88`). The revision:

- Name the providers explicitly: `nginx-ingress` (today's `ProvisionerKindIngress`, string value unchanged for DB compat) and **`tailscale-ingress`** (new kind).
- Make the factory dispatch on kind; the tailscale provisioner writes Tailscale CRs through the existing dynamic client (no `tailscale.com` Go imports — the depguard confinement stands).
- Generalize the three places the single-kind assumption leaks: the infrastructure health check (hardcodes cert-manager Secret + Certificate inspection — moves behind the provisioner interface), orphan-resource listing (hardcodes the ingress kind), and the deletion-time resource-identity checkpoint in `customdomains/impl.go` (kind-guarded).
- `SetupResult.SecretName` already documents "empty when the provisioner does not own a TLS Secret" — written for exactly this case; the tailscale provider terminates TLS itself via the operator.

The IP allowlist stays an nginx-only capability (it is an nginx annotation; on a tailnet, the customer's ACLs are the allowlist).

### Per-org Kubernetes resources

For an enabled `network_ingresses` row, the `tailscale-ingress` provisioner applies, in a dedicated namespace:

1. **Secret** — the customer's OAuth client (`client_id`, `client_secret`), synced from the encrypted columns on the row. Required scopes: `Devices Core`, `Auth Keys`, `Services` (write), tagged `tag:k8s-operator`; the customer's ACL must define `tagOwners` for `tag:k8s-operator` and `tag:k8s`.
2. **Tailnet CR** (cluster-scoped) — `spec.credentials.secretName` → the Secret; ready state `TailnetReady` is the first health signal.
3. **ProxyGroup** (type ingress) — `spec.tailnet` → the Tailnet CR; replicas 2. This is the org's device set on the customer network.
4. **Ingress** — `ingressClassName: tailscale`, `tailscale.com/proxy-group` annotation → the ProxyGroup, hostname from the row (default `gram-mcp`), backend → gram-server's **netingress Service port** (below). The operator provisions MagicDNS + certificate (Let's Encrypt: 50 hostnames/week/tailnet — fine at one hostname per org).

Resource names derive from the ingress id and are **persisted on the row at provision time** (the custom-domain lesson: never re-derive names from a tombstone — a successor's resources would be torn down).

Lifecycle reuses the custom-domain Temporal shape: a signal-coalesced, debounced reconcile workflow per ingress; create/update/delete RPCs nudge it; health checks read `Tailnet`/`ProxyGroup`/`Ingress` status conditions instead of DNS + cert-manager.

### Request flow and trust boundary

```
MCP client on customer tailnet
  └─ https://gram-mcp.<tailnet>.ts.net/mcp/<slug>
     └─ operator ingress proxy pod (tailscale serve, org's ProxyGroup)
        - terminates TLS with the MagicDNS certificate
        - injects Tailscale-User-* identity headers
     └─ gram-server, dedicated netingress listener port
        1. netingress middleware (this listener only):
           - resolve Host → network_ingresses row by dns_name; unknown host → 403
           - normalize Tailscale-User-* → netingress.Context{IngressID, OrgID, Identity}
           - identity_required && no identity headers → 403
        2. customdomains.Middleware: netingress ctx → pass through; synthesize the
           org's custom-domain context if one exists (same namespace resolution)
        3. mcp.ServePublic → ResolveMCPEndpointAndServer
        4. post-resolution org check: endpoint's org == ingress's org, else 404
        5. enforceNetworkLockdown: netingress ctx → allow
        6. normal dispatch; identity attached to logs/audit attrs
```

The trust boundary changes from the earlier design's forward token to **a dedicated listener**: the operator's Ingress backend targets a gram-server Service port that only netingress traffic uses. On every _other_ listener, inbound `Tailscale-*` (and legacy `X-Gram-Netingress-*`) headers are stripped unconditionally, so the public nginx path cannot forge identity; a NetworkPolicy pins the netingress port to the ProxyGroup pods. Host-based attribution on that port mirrors how `customdomains.Middleware` already attributes custom-domain traffic.

### Lockdown matrix (unchanged)

| Request arrives via              | IP allowlist | `private_network_only` | Result                               |
| -------------------------------- | ------------ | ---------------------- | ------------------------------------ |
| Platform host                    | not set      | off                    | allow                                |
| Platform host                    | set          | off                    | 403 — today's behavior               |
| Custom domain                    | any          | off                    | allow (nginx enforced the allowlist) |
| Platform or custom domain        | any          | **on**                 | **403**                              |
| Private network (netingress ctx) | any          | any                    | allow                                |

The install-page / well-known platform-host bypass stays (dashboard-cookie sessions), documented: `private_network_only` governs the MCP data plane.

## Data model (revised)

**`network_ingresses`** — as shipped in the pending migration, minus what the operator obsoletes:

- **Drop `network_node_state` entirely** (operator Secrets hold node state).
- **Drop `auth_key_enc` / the `auth_key` credential mode.** The operator's `Tailnet` CR requires an OAuth client; `credential_kind` collapses to `oauth_client` (column retained for future providers).
- **Add** provisioned-resource identity columns (`resource_name`, mirroring `custom_domains.ingress_name`) written at provision time for tombstone-safe deletion.
- Keep: org scoping + one-per-org partial unique index, `provider` discriminator, `hostname`, `tags`, flags (`enabled`, `private_network_only`, `identity_required`), encrypted OAuth columns, health columns (`status`, `network_name`, `dns_name`, `node_id` → repurposed for ProxyGroup identity, `last_error`, `last_seen_at`, `connected_since`).

The pending migration PR is amended before merge (delete + regenerate, per the unmerged-migration rule).

## Management API and UX (largely as shipped)

The `networkIngress` Goa service survives with a reshaped create/rotate payload: OAuth client id + secret only (no auth-key mode). The dashboard card's setup Sheet documents the heavier customer-side ACL setup (`tag:k8s-operator` OAuth client with the three scopes, `tagOwners` entries) with copy-paste snippets. Everything else — feature gating, RBAC, audit subject, admin surfaces — is unchanged.

## Security analysis (deltas from v1)

1. **Trust boundary**: dedicated listener + NetworkPolicy replaces the shared forward token; header stripping on all other listeners stays. Host attribution is only trusted on the netingress port.
2. **Identity**: `Tailscale-User-*` headers are injected by the operator's serve proxies; advisory in MVP (log/audit). Capability-grant authz remains Phase 2.
3. **Tenant isolation**: the post-resolution org check is unchanged and still the load-bearing invariant, with a dedicated test.
4. **Credentials**: customer OAuth client encrypted at rest in Postgres, synced into the operator namespace as a Secret by the provisioner; rotation updates row + Secret and bounces the Tailnet CR. Blast radius of the Secret is fenced by namespace + RBAC + `ProxyGroupPolicy`.
5. **SSRF/egress posture**: still untouched — the operator's proxies dial inward to gram-server; gram-server never dials the customer network.
6. **Operator supply chain**: the operator is a new cluster-privileged dependency (CRDs + controller). Pin its version; review its RBAC; upgrades follow the same policy as other cluster infra.

## Phased plan (revised)

### Phase 0b — Operator validation spike (~2-3 days, kind cluster + two test tailnets)

- Operator v1.96+ multi-tailnet: `Tailnet` CR + Secret per tailnet, `ProxyGroup` per tailnet, `Ingress` per ProxyGroup; confirm two orgs on two tailnets from one cluster.
- Confirm `Tailscale-User-*` identity headers on the L7 path, and their exact names/values for the middleware contract.
- Confirm Host header seen by the backend (MagicDNS name) and TLS behavior; measure proxy pod footprint.
- Confirm ProxyGroup replica failover keeps the MagicDNS name stable.
- Output: go/no-go memo + the middleware attribution contract.

### Phase 1 — MVP

1. Amend pending migration (drop `network_node_state` + auth-key column; add resource-name columns). _(amends open PR)_
2. Management API reshape: OAuth-only credential payloads. _(amends open PR)_
3. Router abstraction: named provider kinds, kind-dispatching factory, `TailscaleIngressProvisioner` (CRs via dynamic client), generalize the three single-kind leaks, reconcile/delete/health workflows for ingress rows. _(replaces the network-gateway binary PR)_
4. gram-server: netingress listener port + middleware (Host attribution, identity normalization, stripping elsewhere), `enforceNetworkLockdown`, post-resolution org check, wiring.
5. Dashboard card.
6. Deploy: operator install (pinned), namespace + RBAC + `ProxyGroupPolicy` + NetworkPolicy, netingress Service port.

### Phase 2 — Hardening / GA

- Health reconciler on operator status conditions; `checkHealth` RPC; delete-time teardown validation.
- OAuth rotation UX; ACL-breakage surfacing (`tag:k8s-operator` removed, credential revoked).
- Identity → audit-log integration; optional capability-grant authz.
- Metrics/alerts/runbook/customer docs; operator upgrade policy.
- Second provider through the same seam (the earlier embedded-tsnet gateway, preserved in a closed PR, is the non-Kubernetes escape hatch if the operator path hits a wall).

## Open questions

1. Confirm (spike) that a plain `Ingress` + `tailscale.com/proxy-group` annotation serves from a non-default `Tailnet` — the docs pin `spec.tailnet` on ProxyGroup but are silent on Ingress-level selection edge cases.
2. Exact identity-header set and spoofing posture on the serve proxy path (spike): are headers stripped from inbound tailnet requests by serve itself?
3. Per-org namespace vs shared namespace for the provisioned resources (Secret blast radius vs operational sprawl). Leaning shared namespace + tight RBAC for MVP.
4. `Tailnet` CR is cluster-scoped — quota/naming policy for hundreds of orgs; confirm operator scale envelope with Tailscale.
5. Should `private_network_only` also suppress the platform-host install-page bypass? MVP: keep, document.
6. Local-dev story: kind + operator + a dev tailnet; how much of it runs in CI.

## Key files referenced

| Area                                 | Path                                                                                                              |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------------- |
| Provisioner seam to extend           | `server/internal/k8s/provisioner.go:44` (interface), `client.go:83-88` (factory to make kind-aware)               |
| nginx provisioner (template)         | `server/internal/k8s/ingress_provisioner.go`                                                                      |
| Reconcile workflow shape to reuse    | `server/internal/background/custom_domain_registration.go`, `activities/custom_domain_ingress.go:122`             |
| Health machinery to generalize       | `server/internal/background/activities/custom_domain_health.go:33`, `server/internal/k8s/custom_domain_health.go` |
| Lockdown to generalize               | `server/internal/mcp/serveendpoint.go` (`enforceCustomDomainLockdown`)                                            |
| Custom-domain host resolution        | `server/internal/customdomains/middleware.go`                                                                     |
| Management API (shipped, to reshape) | `server/design/networkingress/design.go`, `server/internal/networkingress/`                                       |
| Superseded gateway (escape hatch)    | closed PR #5624 (`netgateway/` tree)                                                                              |

## Appendix — Phase 0 spike results (2026-08-21, embedded tsnet — pre-revision)

Method: throwaway binary (gitignored `scratch/`, separate Go module so `tailscale.com` never touched the root `go.sum`) running N `tsnet.Server` instances in one process against a personal test tailnet on the real Tailscale control plane. Each node used a Postgres-backed `ipn.StateStore` in a dedicated scratch database, with a fresh `os.MkdirTemp` `Dir` on every run (simulating emptyDir loss). RSS measured via `ps` after `runtime.GC`.

| Metric                         | Measured                                                                        |
| ------------------------------ | ------------------------------------------------------------------------------- |
| `tailscale.com` version        | v1.102.3                                                                        |
| Process baseline RSS           | 20.6 MB                                                                         |
| RSS @ 1 / 3 / 10 nodes         | 36.8 / 44.4 / 69.7 MB                                                           |
| Marginal RSS per node          | ≈ 4.9 MB amortized (first node ≈ 16 MB incl. shared init)                       |
| Goroutines per node            | ≈ 91                                                                            |
| Time-to-up per node            | 1.6–2.2 s (fresh join and resume alike)                                         |
| Same-device resume             | 3/3 after `kill -9`, node keys identical, `Dir` wiped, state from Postgres only |
| Auth-key use on resume         | none — tsnet ignores `AuthKey` when the `Store` holds a profile                 |
| Per-request identity (`WhoIs`) | login, display name, and device name attested end-to-end over the tailnet       |

Findings that carry over to the operator architecture: WhoIs identity semantics; the single-use auth-key footgun (now moot — OAuth-only); per-node resource costs as the operator's proxy budget. Findings the operator obsoletes: the Postgres StateStore and unclean-death resume machinery (operator-managed Secrets + ProxyGroup replicas own this now).
