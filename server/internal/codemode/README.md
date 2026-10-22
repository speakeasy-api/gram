# Gateway Python host API

This package supplies the discovery and invocation contract for ephemeral code
executions. `CatalogHost` holds a run's optional tool restriction. It fetches
current catalogs through `Backend` only for discovery; it does not retain a
reusable discovery cache.

`Search` loads authorized members with bounded concurrency, reports incomplete
members, and ranks exact paths, tokenized tool names and descriptions. Cursors
bind the caller scope, query, full definition identities and failed members.
Following a cursor repeats discovery and rejects a changed catalog. `Describe`
preserves schemas and produces an escaped Python call template.

`Call` checks the path against the run's restrictions and that arguments are a
bounded JSON object, then invokes the backend without reading the member's
catalog. Authorization is refreshed on every call; the member enforces its own
input schema and rejects unknown tools, and that refusal reaches the program as
a completed `tool_call_rejected` result it can correct. Discovery is never
authorization to execute. The bridge drops a member whose routing destination
changed since admission.

The MCP package's bridge uses the existing hosted, remote and tunneled member
dispatchers and the connection's consent tool selection. It refreshes grants, preserves the
admitted permission boundary, and requires a live gate refresher for endpoint
binding, session expiry/revocation, policy and upstream credentials. A missing
refresher fails closed. New member identities cannot join an admitted execution.
Changed connection policy ends the run; a new execution can use the new policy.

The caller must impose the execution deadline, discovery/call attempt budgets,
concurrency and cumulative result limits, cancellation, and admission capacity.
Each callback uses that execution's context. Callbacks must not accept a caller,
project, credential or destination supplied by a runner. The host API itself does
not register a public MCP tool or provision a sandbox.

Anonymous sessions share four runner identities per gateway. Each identity can
hold four executions, so anonymous traffic on one gateway can consume all 16
per-replica organization slots. It can delay authenticated callers in the same
organization; separate organizations retain their own concurrency budgets.

## Execution and cancellation

The gateway exposes one `execute` tool in `code_mode`. Its description includes
Python discovery/call examples and the caller's permitted member prefixes. Speakeasy
builds the host from its own admitted identity; the runner receives only Python,
limits and callback results. Existing hosted, remote and tunnel dispatchers own
credentials and tool telemetry. All tools (frozen) and Specific tools reuse the
immutable catalogue captured at consent for servers, search, and describe,
without upstream discovery or token resolution. All tools (live) refreshes only
the members approved at consent. Invocation always refreshes authorization and
uses the existing live dispatcher, with the same approved catalogue boundary.

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
derived URL to preserve worktree port remapping. The dedicated runner receives
the same token as `GRAM_CODE_RUNNER_TOKEN`; use at least 32 random bytes. The
local provider accepts literal loopback addresses only and refuses non-local
environments.
The default provider is disabled. When no runtime is configured, stored
Code Mode gateways and connections use Progressive discovery. New explicit
Code Mode choices require a configured runtime and the normal settings
permissions. Configuring the runtime enables new Code Mode choices for every
organization on that deployment; there is no organization entitlement or rollout
flag. Never commit the runtime token or local configuration.

## Results and compatibility

Tool results preserve `structuredContent` exactly and retain bounded MCP content.
Output schemas are not checked: a call never reads the member's catalog, so
structured output passes through as the member returned it.
Missing structured data is JSON null. Text is not parsed as JSON. Private `_meta`
on the result, content blocks and embedded resources stays on the host side.

An upstream reply has outcome `completed`, including a tool-level error. A
transport failure after admission has outcome `unknown`; it must not trigger an
automatic replay. A member's own unknown-tool or invalid-params error, or a
hosted lookup refusal before dispatch, produces a completed `tool_call_rejected`
result carrying a bounded copy of its message. An error the gateway's proxy
synthesizes, including a response withheld after the tool ran, stays `unknown`. A result too
large or malformed to expose produces a completed `tool_result_unavailable`
result. These outcomes describe dispatch, not whether a remote service
made a side effect. Only typed guard refusals prove `not_dispatched`; unclassified backend or host
failures retain an unknown outcome.

## Validation and product surfaces

Run `mise run test:server ./internal/codemode ./internal/authz
./internal/mcp/metamcp ./internal/mcp/toolfilter` and
`mise run test:server ./internal/mcp -run 'Meta|ToolSelection|CodeBridge'` from the repo.
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
`GRAM_CODE_RUNTIME_IMAGE` (a `repository@sha256:` reference; previews may use a
tag, see below), and
`GRAM_CODE_RUNTIME_TOKEN`. The runner pod gets the same secret under
`GRAM_CODE_RUNNER_TOKEN`; neither value reaches the Monty child.

Each caller identity (user, external user, API key and credential) gets its own pod
within a project, so a pod never runs code for two users, projects or
organizations. The organization gateway has no project: it runs
organization-wide (`Scope.OrganizationWide`, project `uuid.Nil`), with its own
pod per caller kept apart from that caller's project pods and counted in the
organization cap. Anonymous sessions share a bounded set of runtime identities
per gateway, with fresh interpreter state and member sessions confined to each
execution. An authenticated pod is reused only by its caller and is deleted after a minute idle, never returned to
the warm pool. Each
pod admits four concurrent executions. A replica holds at most 64 pods
(`GRAM_CODE_RUNTIME_MAX_RUNNERS`), at most 16 per organization
(`GRAM_CODE_RUNTIME_MAX_ORGANIZATION_RUNNERS`), and evicts its oldest idle pod
when another caller needs capacity. Readiness polls start at 25 ms and back off
to one second, so adopting a warm pod costs little more than the controller's
bind. Claims
expire after five minutes, refuse admission with less than 35 seconds remaining,
and are deleted after a minute idle. A replacement can warm while the old pod
drains. Warm-up has its own two-minute deadline; cancellation of one waiting
request does not cancel the shared cold start. Local housekeeping consumes zero Temporal
actions. Claim deletion uses a UID precondition. Every admission, including one
that reuses a live connection, checks current claim ownership and expiry, the
sandbox and pod's deletion and readiness, pod UID/IP, gVisor isolation and the
configured image, plus the running digest; each new connection repeats that
check before dialing. An unready pod refuses the admission without retiring it.
The dedicated Kubernetes client is explicitly limited to 100 QPS with a
200-request burst; each admission costs a claim read, a sandbox read and a pod
list.
A changed pod fails the execution; programs are never replayed.

The provider requires the Agent Sandbox controller's
[`spec.lifecycle.shutdownTime`](https://github.com/kubernetes-sigs/agent-sandbox/blob/v0.5.6/extensions/api/v1alpha1/sandboxclaim_types.go)
and `shutdownPolicy: Delete`. It refuses a claim if the API prunes either field.
A staging rollout must additionally verify controller-enforced expiry after Speakeasy
termination, gVisor execution, denied pod egress, and SIGTERM draining. Fake-client
tests validate admission and ownership but cannot validate the managed controller
or cluster network policy. Keep the runtime disabled until those checks pass.

Use an image- and protocol-specific template/pool name during rolling deployments,
retaining the previous pool until its Speakeasy replicas and claims have drained. The
infra chart derives the suffix from the image reference and supports one previous
image. All server entrypoints release providers through MCP Service.Shutdown after
HTTP draining. Transient Kubernetes errors refuse the individual admission without
retiring a healthy pod. A runner connection failure still affects up to four
executions on that caller's transport; dispatched writes retain unknown outcomes.

### Images and previews

CI publishes `gram-code-runner` on every commit, content-addressed by the
12-character tree hash of `agents/code-runner`: it builds the image when that
hash is new and otherwise re-tags the existing one. Each image is tagged with
the tree hash, its commit and, on pull requests, the preview tag
`pr-<number>-<head sha>`, so every preview has a runner image with the server's
tag.

This repository's CI pushes tags. For released environments, gram-infra resolves
the tree-hash tag to its registry digest and sets the template image and
`GRAM_CODE_RUNTIME_IMAGE` to that same `repository:tag@sha256:` reference; mutable
references are refused there. Templates are named from a hash of the image
reference, so a rollout adds a new template while the previous one keeps serving
old replicas until they drain.

Previews run in their own code namespace (`gram-code-<env>-previews`) with their
own runner token, separate from the environment's. gram-infra sets
`GRAM_IS_PREVIEW=true` on preview servers and provides only
`GRAM_CODE_RUNTIME_PREVIEW_TOKEN`. The server refuses GKE setup when that token
is missing, or matches `GRAM_CODE_RUNTIME_TOKEN` when both are present in the
process. This comparison is a configuration tripwire: it cannot detect a reused
secret if only the preview variable is present, and the preview guard is inactive
unless the server receives `GRAM_IS_PREVIEW`. Separate infra resources and
secrets provide the environment boundary; previews still share the
environment's application service account, so the namespace is not an identity
boundary, and the token is what keeps a preview server off the environment's
runners. The runner receives the preview secret as `GRAM_CODE_RUNNER_TOKEN`.

Only preview mode accepts a tagged runner image (`pr-<number>-<head sha>`): the
preview server runs that same pull request's code, so a digest pin would not
constrain its author. The running container's digest is still checked whenever
the configured image is pinned.

### Isolation and threat model

The server verifies gVisor, disabled service-account token mounting, disabled
host networking, exactly one `code-runner` container, claim ownership, pod UID/IP,
configured image and its running digest. The gram-infra code sandbox template
and cluster admission policy own and must verify the remaining pod fields:

- `spec.hostPID=false`, `spec.hostIPC=false`,
  `spec.serviceAccountName` set to the dedicated code-runner identity, and no
  `initContainers` or `ephemeralContainers`.
- `spec.securityContext.runAsNonRoot=true`, `runAsUser=65532`, and
  `seccompProfile.type=RuntimeDefault`.
- `spec.containers[0].securityContext.readOnlyRootFilesystem=true`,
  `allowPrivilegeEscalation=false`, and `capabilities.drop=[ALL]`, with no added
  capabilities; positive `resources.limits.cpu`, `memory` and `ephemeral-storage`.
- No `hostPath`, Secret or projected credential volumes. Writable mounts may
  only be bounded `emptyDir` scratch storage. No `envFrom`; the environment may
  contain only the runner token and its explicit port/interpreter configuration.
  Only the dedicated runtime token Secret may populate `GRAM_CODE_RUNNER_TOKEN`.

Ingress NetworkPolicy must allow the runner port only from the matching
Speakeasy server deployment; default-deny both ingress and egress for runner
pods. The HTTP upgrade listener binds `0.0.0.0:8081`. Its 32-socket pre-auth
limit is a resource bound, not protection against arbitrary pod-network peers:
an allowed peer can exhaust it. A staging check must demonstrate both denied
peer ingress and denied runner egress before enabling the provider.

Runner authentication uses one static token per environment, sent over the
private pod network without TLS. It is not bound to a claim, caller or execution.
A compromised peer with network access and that token can impersonate the host;
per-claim tokens and authenticated encryption are not implemented here. This
requires the ingress policy, separate preview namespace/secrets and independent
secret rotation for development and production. Workload identity/claim RBAC is
also environment-specific. No token reaches the Monty child.

The coordinator permits at most 16 active executions per organization and four
per caller/project on each replica, including cold-start and cleanup time. It
refuses excess work before Redis or pod acquisition. Pods remain capped at 16
per organization and 64 per replica. Limits are per replica, not a distributed
fair scheduler; bursts can still evict another organization's idle pod and
cause cold starts. There is no queue, retry or promised cross-tenant latency.
Anonymous callers share four runner identities per gateway (fresh interpreters,
execution-local member sessions); they share those per-caller limits. Keep the
runtime default-off until isolation, network policy and capacity checks pass.

## Selecting Code Mode

Set a gateway's `discovery_mode` to `code_mode` through gateway Settings, or
choose Code Mode for an Inspect/consent connection. The native MCP surface
then contains one `execute` tool. Its description lists the caller's permitted
member prefixes and the Python helper workflow. Tools chosen on the consent
screen limit the underlying tools that search, describe and calls can reach, not
the native execute tool.

The organization gateway has no stored default: its connections choose Code
Mode on the consent page, the default stays Progressive, and agent gateways
never use Code Mode.

New choices require a configured code runtime. Gateway responses expose
`code_mode_enabled` to authorized project readers.

A disabled runtime prevents new explicit choices. Existing Code Mode
connections use Progressive discovery with their original consent catalogue;
the stored default and issued policy are unchanged. A configured runtime that
cannot admit a run returns an execution refusal without submitting a program. Deploy compatible readers
and infrastructure before enabling issuance. The separate infra chart is
disabled by default.

Inspect shows the live execute description, accepts Python only on explicit
submission, and renders the submitted source, return value, printed output,
and nested tool outcomes. A new request gets a unique JSON-RPC ID. There is no
automatic retry. Cancel, reconnect, or leaving Inspect sends best-effort MCP cancellation with
the original request ID and credential, then aborts its HTTP request;
already dispatched upstream writes may have completed. Changing modes retains
the consent tool selection and token refresh retains the requested override.

Try this against the locally seeded **Python Code Gateway**:

```python
found = await tools.search("health")
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
