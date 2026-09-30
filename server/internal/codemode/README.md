# Gateway Python host API

This package supplies the discovery and invocation contract for ephemeral code
executions. `CatalogHost` holds a run's optional tool restriction and the identity
of definitions it resolves. It fetches current catalogs through `Backend`; it
does not retain a reusable discovery cache.

`Search` loads authorized members with bounded concurrency, reports incomplete
members, and ranks exact paths, tokenized tool names and descriptions. Cursors
bind the caller scope, query, full definition identities and failed members.
Following a cursor repeats discovery and rejects a changed catalog. `Describe`
preserves schemas and produces an escaped Python call template.

`Call` validates arguments before invoking the backend. The backend must compare
the expected fingerprint against the actual definition and destination at its
authoritative dispatch point. Discovery is never authorization to execute.

The MCP package's bridge uses the existing hosted, remote and tunneled member
dispatchers and frozen-definition guards. It refreshes grants, preserves the
admitted permission boundary, and requires a live gate refresher for endpoint
binding, session expiry/revocation, policy and upstream credentials. A missing
refresher fails closed. New member identities cannot join an admitted execution.
Changed connection policy ends the run; a new execution can use the new policy.

The caller must impose the execution deadline, discovery/call attempt budgets,
concurrency and cumulative result limits, cancellation, and admission capacity.
Each callback uses that execution's context. Callbacks must not accept a caller,
project, credential or destination supplied by a runner. The host API itself does
not register a public MCP tool or provision a sandbox.

## Execution and cancellation

The gateway exposes one `execute` tool in `code_mode`. Its description includes
Python discovery/call examples and the caller's permitted member prefixes. Gram
builds the host from its own admitted identity; the runner receives only Python,
limits and callback results. Existing hosted, remote and tunnel dispatchers own
credentials and tool telemetry. Discovery loads live catalogs on every request.

`Coordinator` admits a request through a short Redis ownership record, acquires
a provider lease and runs a fresh interpreter. The record contains an opaque
execution ID; its key hashes organization, project, gateway, credential, MCP
session and the typed JSON-RPC request ID. It contains no code or credentials.
Cancellation can arrive on another replica: it atomically marks the owner and
the executing replica checks ownership every 200 ms. Loss of Redis ownership
fails closed. These records coordinate in-flight work, not durable jobs or replay.

Authenticated sessionless requests use their validated credential as the
cancellation scope. An explicit MCP session ID further narrows that scope.
Expiry or refresh rotation of the original bearer ends its running execution at
the next helper boundary. A replacement bearer cannot inherit or cancel that
execution; use the original HTTP request lifetime to interrupt it. This preserves
the admitted credential boundary even when a refreshed session gains permissions.
Anonymous requests have independent execution identities; cancellation
notifications cannot control them. Anonymous callers cancel through the HTTP
request lifetime, and every execution has the same hard deadline.

The Go client reuses an authenticated HTTP-upgraded yamux connection. Four local
admission slots bound discovery and open/closing streams; refusals do not open
another stream. Admission remains held until the peer's FIN. The runner's
`admission_refused` response proves the program did not run. A missing `started`
response does not prove that. Submitted programs are never automatically retried.

Host construction, including an optional exact tool allowlist, runs after
admission under the 30-second execution context. Each helper reruns issuer
admission and bypasses the usual session-policy cache, refreshes grants, and
intersects them with the admission boundary. Cancellation stops new callbacks
and joins dispatchers for up to two seconds before recording outcomes. Stream
cleanup adds at most one second. The provider release path also has a bounded
cleanup context. Incomplete writes remain `unknown` even when Python fails.

## Local runtime

Run `mise run zero:code-mode`, then `mise exec -- pitchfork start code-runtime`.
The setup task saves a local provider and random token in ignored
`mise.local.toml`, preserving existing settings, and builds the native binaries.
`./zero` includes this setup. Normal `mise run start` / `wake` starts the daemon
when `GRAM_CODE_RUNTIME_PROVIDER=local`; `pause` stops it with the application.
Pitchfork owns the native runner process; no runner Docker container is needed.
Its HTTP address defaults to `http://127.0.0.1:18081`, with
`GRAM_CODE_RUNNER_PORT` remapped per worktree. An explicit
`GRAM_CODE_RUNTIME_URL` overrides that address. Logs and restarts use
`mise exec -- pitchfork logs code-runtime` and
`mise exec -- pitchfork restart code-runtime`. Restart `server` after changing
its runtime settings. A native local process exercises the protocol and Python
restrictions; managed gVisor/container isolation still needs staging validation.
Mise installs the pinned Rust toolchain. The interpreter build cache is shared
across worktrees; Cargo checks the pinned source revision on each setup. A failed
runtime setup/start leaves the rest of the local stack available and logs how
to retry.

Build `mise run build:monty` and `mise run build:code-runner`, then run
`mise run test:code-mode`. The task exercises the actual pinned Rust/Monty runtime
through the Go protocol and MCP gateway, with real Redis/database infrastructure.
`--monty-bin /absolute/path/to/monty` reuses an already-built pinned executable.
Run with `-race` to include Go's race detector. No commercial registry is needed.

The setup task configures `GRAM_CODE_RUNTIME_PROVIDER=local` and
`GRAM_CODE_RUNTIME_TOKEN` in the ignored development configuration. Keep the
derived URL to preserve worktree port remapping. The dedicated runner receives the same token
as `GRAM_CODE_RUNNER_TOKEN`; use at least 32 random bytes. The local provider
accepts literal loopback addresses only and refuses non-local environments.
The default provider is disabled and stored code-mode gateways fail closed when
the runtime is unavailable. New choices additionally require the selection gates
described below. Set `GRAM_LOCAL_FEATURE_FLAGS_CSV` in `mise.local.toml` to the
absolute path of `server/flags.csv` (or a dedicated CSV **inside server/**), then
restart the local server. The local flag provider rejects paths outside its
working directory. Never commit the runtime token or local configuration.

## Results and compatibility

Tool results preserve `structuredContent` exactly and retain bounded MCP content.
Missing structured data is JSON null. Text is not parsed as JSON. Private `_meta`
on the result, content blocks and embedded resources stays on the host side.

An upstream reply has outcome `completed`, including a tool-level error. A
transport failure after admission has outcome `unknown`; it must not trigger an
automatic replay. A result too large or malformed to expose produces a completed
`tool_result_unavailable` result. An output-schema mismatch preserves the result
with a warning. These outcomes describe dispatch, not whether a remote service
made a side effect. Pre-dispatch guard refusals are `DispatchError` values with
`BeforeInvoke` set.

Input validation fails closed for missing, malformed, oversized or unsupported
schemas. External `$ref` loading is disabled. Tools using unsupported regular
expressions or schemas need a supported schema before code mode can call them;
Direct and Progressive behavior is unchanged. The initial schema ceiling is
128 KiB, independently of the aggregate catalog and callback limits.

## Validation and product surfaces

Run `mise run test:server ./internal/codemode ./internal/authz
./internal/mcp/metamcp ./internal/mcp/toolfilter` and
`mise run test:server ./internal/mcp -run 'Meta|Frozen|CodeBridge'` from the repo.
The MCP tests use real local database infrastructure and hosted, remote and
mapped-tunnel fixtures; they do not prove production capacity or gVisor isolation.

The settings layer exposes gateway/connection discovery choices and updates the
Platform MCP inspection surface and demo seed. No new Temporal work is introduced.

### Dedicated GKE code provider

`GRAM_CODE_RUNTIME_PROVIDER=gke` uses the existing assistants cluster API and
shared sandbox discovery, with a separate namespace, SandboxTemplate and warm
pool. Configure `GRAM_CODE_RUNTIME_GKE_CLUSTER_ENDPOINT`,
`GRAM_CODE_RUNTIME_GKE_CLUSTER_CA` (base64), `GRAM_CODE_RUNTIME_GKE_NAMESPACE`,
`GRAM_CODE_RUNTIME_GKE_SANDBOX_TEMPLATE`, `GRAM_CODE_RUNTIME_GKE_RUNNER_CIDR`,
`GRAM_CODE_RUNTIME_IMAGE` (immutable `@sha256:` reference), and
`GRAM_CODE_RUNTIME_TOKEN`. The runner pod gets the same secret under
`GRAM_CODE_RUNNER_TOKEN`; neither value reaches the Monty child.

Each Gram replica holds at most 16 project runtimes, each admitting four concurrent
executions, with at most four pods per organization. Idle pods are evicted when
another project needs capacity. There is no cross-project pod reuse after warm-pool adoption. Claims
expire after five minutes, refuse admission with less than 35 seconds remaining,
and are deleted after a minute idle. A replacement can warm while the old pod
drains. Warm-up has its own two-minute deadline; cancellation of one waiting
request does not cancel the shared cold start. Local housekeeping consumes zero Temporal
actions. Claim deletion uses a UID precondition. Every admission checks current claim ownership and expiry. Each new connection
also checks pod UID/IP, gVisor isolation and actual image digest. The dedicated
Kubernetes client is explicitly limited to 100 QPS with a 200-request burst; a
healthy persistent connection needs one claim read per admission.
A changed pod fails the execution; programs are never replayed.

The provider requires the Agent Sandbox controller's
[`spec.lifecycle.shutdownTime`](https://github.com/kubernetes-sigs/agent-sandbox/blob/v0.5.6/extensions/api/v1alpha1/sandboxclaim_types.go)
and `shutdownPolicy: Delete`. It refuses a claim if the API prunes either field.
A staging rollout must additionally verify controller-enforced expiry after Gram
termination, gVisor execution, denied pod egress, and SIGTERM draining. Fake-client
tests validate admission and ownership but cannot validate the managed controller
or cluster network policy. Keep the runtime disabled until those checks pass.

Use an image- and protocol-specific template/pool name during rolling deployments,
retaining the previous pool until its Gram replicas and claims have drained. The
infra chart derives the suffix from the image reference and supports one previous
image. All server entrypoints release providers through MCP Service.Shutdown after
HTTP draining. Transient Kubernetes errors refuse the individual admission without
retiring a healthy shared pod. A runner connection failure still affects up to four
executions on that project transport; dispatched writes retain unknown outcomes.

### Preview deployment follow-up

The preview runtime is not provisioned by this change. Existing previews select
the Fly assistants runtime; Code Mode has its own provider and does not inherit
that configuration. The local provider only accepts the local environment and a
loopback URL, so it is not a shortcut for a remotely hosted preview runner.

Use the existing assistants GKE cluster with a dedicated code namespace for each
opted-in preview. Keep the assistant image, sessions and warm pool separate. The
remaining preview wiring belongs in the image pipeline and gram-infra:

1. Publish a code-runner image for the reviewed revision and resolve it to an
   immutable digest. Use that exact digest in both the template and Gram's config.
2. Provision a per-preview namespace, token, template and bounded capacity, with
   gVisor and denied egress. Start with no prewarmed pods or one small warm pool;
   set a namespace quota across all preview Gram replicas.
3. Give the preview a distinct cloud identity with a namespace-scoped RoleBinding.
   Existing previews share an application cloud identity; different Kubernetes
   service-account names alone would not prevent cross-preview claim access.
   Deliver the runner token/config only to that preview's Gram deployment and
   its code namespace, with permitted ingress from the Gram cluster.
4. Tie provisioning and cleanup to the opted-in PR's lifecycle. PR closure or
   preview removal must remove its claims, namespace, secret and identity grants.
   Retain the previous image pool while old Gram replicas drain during an update.
5. Keep Code Mode unavailable until the namespace/config are ready and the managed
   controller, network isolation and cancellation checks above pass. Then enable
   the entitlement and rollout flag for the preview's test organization.

This is the proposed preview deployment path, not an assertion of live cluster
state. Publishing the application PR does not make preview execution available.

## Selecting Code Mode

Set a gateway's `discovery_mode` to `code_mode` through gateway Settings, or
choose Code Mode for an Inspect/consent connection. The native MCP surface
then contains one `execute` tool. Its description lists the caller's permitted
member prefixes and the Python helper workflow. Existing frozen reviews still
approve underlying tools, not the native execute tool.

New choices require the `gateway_discovery_modes` organization entitlement,
a configured code runtime, and the temporary `gateway-code-mode` PostHog flag.
The flag is evaluated by Gram with the organization ID and organization/project
group keys; missing, indeterminate, and error results deny new selections.
Gateway responses expose `code_mode_enabled` to authorized project readers.
The local CSV enables rollout for the fictional development/demo organizations.
Before merging/deploying, create the flag with organization/project **group key**
targeting and verify evaluation reasons for the initial rollout population.

Turning off rollout prevents new explicit choices. It does not rewrite a stored
gateway default or issued connection policy. Ordinary connections can continue
following the stored default. An unavailable runtime refuses code execution
explicitly; it never falls back to Progressive or Direct. Deploy compatible
readers and infrastructure before enabling issuance. The separate infra chart
is disabled by default and requires an immutable image digest.

Inspect shows the live execute description, accepts Python only on explicit
submission, and renders the submitted source, return value, printed output,
and nested tool outcomes. A new request gets a unique JSON-RPC ID. There is no
automatic retry. Cancel, reconnect, or leaving Inspect sends best-effort MCP cancellation with
the original request ID and credential, then aborts its HTTP request;
already dispatched upstream writes may have completed. Changing modes retains
the reviewed frozen selection and token refresh retains the requested override.

Try this against the locally seeded **Python Code Gateway**:

```python
found = await tools.search("ticket")
found
```

Then use a returned exact path with `await tools.describe(path)` before calling
`await tools.call(path, arguments)`. Hosted, remote and tunneled members share
these helpers. A tunnel's live tools/list flows through its existing dispatcher;
no separate registration or discovery cache is introduced. The shared demo
contains hosted definitions; a separately configured local upstream can provide an executable demonstration
backend. Every run has fresh Python state, so carry paths/arguments in source
rather than expecting prior variables to survive.

### Management MCP parity

The outcome is inspecting the discovery behavior of one exact gateway in an
explicit project, as an authorized external organization admin. The existing
`get_mcp_connection_settings` tool already returns the stored discovery mode;
its description and regression test now cover `code_mode`. Its scopes, exact
target selection, secret omissions, and version semantics are unchanged.
No new low-level execution or settings mutation is added to Platform MCP:
execution belongs to the gateway's authenticated MCP connection, while changing
settings retains the existing dashboard/API authorization and audit workflow.
Managed assistants do not receive the external-admin settings tool. No shipped
Platform MCP skill names a changed tool or procedure. There is no new staff
capability or admin workflow, so Admin MCP requires no change.
