# Internal MCP catalog access

Catalog connectivity is disabled unless both the process configuration and the
MCP client opt in. Ordinary Guardian clients and `ValidateHost` retain their
existing network policy.

## Dev deployment wiring (follow-up)

Set this environment variable on the dev API (`gram start`), MCP serving tier
(`gram mcp`), and worker deployments, using the same server build:

```text
GRAM_REMOTE_MCP_CATALOG_ILB_CIDR=<CATALOG_ILB_IPV4>/32
```

Use the reserved dev ILB address from the internal deployment configuration.
The equivalent flag is `--remote-mcp-catalog-ilb-cidr`. It is also accepted by
admin, streams, and private-ingress runtime commands that share these policy
constructors. Set it on any such deployment that executes catalog MCP requests.
Invalid values fail startup; only one private IPv4 `/32` is accepted. Leave the
variable unset in production. No committed environment default enables it.

The infrastructure follow-up must pass this non-secret value into the relevant
dev deployment environment blocks in gram-infra. It must also finish DNS/TLS
wiring for `*.catalog.dev.speakeasy.com` to the reserved ILB and preserve network
reachability from the dev pod range. No deployment or infrastructure changes are
part of this code change. Local tests use mock DNS and a loopback TLS server;
they do not prove deployed DNS, certificates, firewall rules, or ILB routing.

The catalog uses `https://<customer>.catalog.dev.speakeasy.com/<server>/mcp`.
The ILB handles path-prefix stripping; the application sends the original path.
The exception trusts the configured ILB's HTTPS listener, regardless of the
hostname used to reach it. Customer labels are release-channel conventions,
**not authorization boundaries**. Every customer can route to every catalog
server; ordinary product authorization and upstream authentication still apply.

## Enforcement and path audit

- The configured private IPv4 address is permitted only over HTTPS on port 443.
  Other destinations retain the ordinary Guardian blocklist; public destinations
  remain allowed. There is no hostname allowlist.
- Preflight checks every DNS answer. Runtime checks the actual resolved socket
  on every connection, including after preflight and redirects. A DNS change to
  another private address cannot reuse the exception.
- The standard TLS dialer receives the socket exception; the plaintext dialer
  does not. A redirect to HTTP, even on port 443, cannot use it. Existing MCP
  URL validation and redirect/body-replay rules remain in force.
- Opted-in clients connect directly (environment HTTP proxies are disabled) so
  DNS and socket checks cannot be delegated to a proxy. TLS trust and hostname
  verification remain enabled, using the original request hostname. IP-literal
  URLs require a certificate valid for that IP. The load balancer must expose
  only services intended to be reachable by all catalog consumers.
- Remote MCP create/update/provision/verify use `proxy.ValidateRemoteMCPURL`;
  probes and the hosted proxy opt in at client construction. Unproxied registration
  uses the same validation option.
- `externalmcp.NewClient` covers tool discovery (`process.go`), execution,
  recovery, gateway calls, unproxied probes, and approval rechecks in workers.
  Remote OAuth metadata discovery and protected-resource probes also opt in.
- Platform MCP direct inspection/registration validation, registered-server
  readiness and reviewed-provider readiness use the same rule. Existing
  `register_remote_mcp` and readiness tools already represent these authenticated
  administrator/member outcomes; no tool schema, audience or authorization
  change is needed. This is internal connectivity configuration, not a new tool.
- Registry downloads, other vendor clients, OAuth token/issuer operations and
  raw dialers retain their existing policy. This rule grants no generic private
  OAuth issuer access. No demo records are needed for a transport-only change.
