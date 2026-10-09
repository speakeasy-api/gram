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
The exception permits the configured ILB address, regardless of hostname or port.
The ILB treats inbound requests as untrusted. Customer labels are release-channel conventions,
**not authorization boundaries**. Every customer can route to every catalog
server; ordinary product authorization and upstream authentication still apply.

## Enforcement and path audit

- Opted-in MCP clients add the configured `/32` to Guardian's existing CIDR
  allowance. Other destinations retain the ordinary blocklist. There is no
  catalog-specific hostname, scheme, port, or transport logic.
- Preflight checks every DNS answer. Runtime checks the actual resolved socket
  on every connection, including after preflight and redirects. A DNS change to
  another private address cannot reuse the exception.
- Existing MCP HTTPS validation and redirect/body-replay rules remain in force.
  Guardian uses its ordinary HTTP transport and TLS verification. The allowance
  changes only which IPs are reachable, not authentication or authorization.
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
