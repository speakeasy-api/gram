# Configure production Kit assistants

The Kit GKE installer is an explicit deployment opt-in. It selects runtime v3
for newly created assistants only. Existing assistants and conversations retain
their runtime and canonical transcript. This path does not import legacy history
or migrate existing conversations.

## Verified ACP transport

The production backend uses `https://<PodIP>:<guest-port>/acp/v2`. Each cold
bootstrap and snapshot restore creates a fresh ECDSA TLS key and certificate,
scoped to `kit-<runtime-uuid>.kit.internal`. The server trusts only that
bootstrap's certificate, verifies its DNS identity, and retains the guarded
Pod-CIDR dial policy. Redirects and transport retries are disabled. No insecure
TLS verification or plaintext production fallback is available.

Kit's `gateway` contract has an HTTP listener, not native TLS
flags (`src/gateway.rs`). The renderer therefore uses the repository's
unprivileged nginx pattern: `nginxinc/nginx-unprivileged:1.28-alpine` terminates
TLS on the guest port, then forwards to Kit on `127.0.0.1:17766` inside the same
Pod. Kit uses its supported `--listen`, `--credential-file`, and `--project`
arguments, without wildcard binding. SSE buffering, proxy retries, and access
logs are disabled; request headers and bearer credentials must never be logged.
The metadata-file rotation path requires Kit v0.2.19 or later; no service mesh
is required.

TLS private keys exist only in the runtime's immutable bootstrap Secret and its
read-only `tls` projection, not the Kit root filesystem. The Kit container does
not mount the TLS key. Fresh bootstrap also removes restored `.kit/tls` and the
entire old credential directory. Only the public certificate is persisted in
lifecycle authority. New bootstrap replaces the Secret and trust certificate;
existing UID-fenced cleanup removes the old Secret. Certificates expire after
30 days; gateway renewal uses that certificate lifetime, independently of
short-lived workload token rotation.

Infrastructure contract (review/apply separately):

- Permit pulling the configured released Kit image, the nginx image above,
  and the digest-pinned metadata sidecar image. The sandbox has two ordinary containers (`kit`, `acp-tls`) and a native
  restartable init sidecar (`workload-token`), UID/GID
  1000, no capabilities, and automatic service-account token mounting disabled.
- Keep server/worker ingress limited to the configured guest TCP port and
  source CIDR. The guest port now speaks TLS, not HTTP. Do not expose port
  17766; it is loopback-only and excluded from ingress policy.
- Permit the existing bootstrap Secret projection plus a read-only `tls`
  projection and 32 MiB memory-backed `tls-tmp` volume. No extra RBAC, external
  CA, DNS record, shared private key, or load balancer is needed.
- Budget the proxy's 10m CPU/16 MiB requests and 100m CPU/64 MiB limits in
  addition to Kit resources. The metadata sidecar adds 10m CPU/32 MiB requests
  and 100m CPU/128 MiB limits. Rootfs-only snapshot policy must cover this
  Pod, including its native sidecar; Secret and memory volumes are supplied
  fresh on restore.
- The TLS transport/image participate in the runtime compatibility fingerprint.
  Do not reuse plaintext-era authority or snapshots across this rollout.

Local Docker remains a loopback-published development transport; these changes
apply to production GKE, not the local image smoke fixture. Platform MCP tool
contracts and demo seed data are unchanged: this is internal transport plumbing.

### Transport verification

Run the focused transport and lifecycle regression tests:

```sh
mise run test:server ./internal/assistants -run '^TestKit' -count=1
```

With the pinned local Kit integration image and the nginx image available to
the container engine, run the actual encrypted ACP transport smoke test (no
inference provider or production cluster required):

```sh
GRAM_KIT_TLS_PROXY_SMOKE=1 mise run test:server ./internal/assistants \
  -run '^TestKitGKETLSProxyImageSmoke$' -count=1
```

The smoke test checks authenticated ACP initialization through nginx, plaintext
rejection, wrong-certificate and wrong-hostname rejection, and invalid-bearer
rejection. It does not prove GKE controller or snapshot infrastructure health.

## Deployment inputs

Use Kit v0.2.19 or a workload-capable build with an explicit version tag
(not `latest`) or digest. Do not use local relay settings in production.

Configure the server and worker consistently:

- `GRAM_ASSISTANT_RUNTIME_PROVIDER=gke`.
- `GRAM_ASSISTANT_KIT_IMAGE=<REGISTRY>/<IMAGE>:<VERSION>` (Kit v0.2.19 or later;
  each workload exchange must reopen the assertion file after atomic rotation).
- `GRAM_ASSISTANT_KIT_GKE_IAM_PROJECT=<IAM_PROJECT_ID>`.
- `GRAM_ASSISTANT_KIT_GKE_METADATA_IMAGE=<REGISTRY>/gram-assistant-runtime@sha256:<DIGEST>`.
  Use the image built from this change, not an older runtime image. The existing
  `assistant-runtime-image` CI job builds and publishes it; local builds use
  `mise run build:assistants-runtime-image`. The script and its tests are part
  of that Dockerfile. No separate, unpublished sidecar image is assumed.
- Existing assistant GKE cluster endpoint, CA, namespace and runner CIDR settings.
- `GRAM_ASSISTANT_KIT_GKE_INGRESS_CIDR=<SERVER_SOURCE_CIDR>`. This is the
  server source network permitted to reach gateway pods, not an unrestricted
  public ingress rule.
- CPU/memory request and limit settings under `GRAM_ASSISTANT_KIT_GKE_*`.
  Workspace and temporary volume limits are separately configurable.
- `GRAM_ASSISTANT_KIT_MCP_RESOURCE=https://<RESOURCE_HOST>/mcp/<SLUG>`.
- `GRAM_ASSISTANT_KIT_MCP_ISSUER=https://<EXPECTED_ISSUER>` and
  `GRAM_ASSISTANT_KIT_MCP_ISSUER_ID=<PERSISTED_ISSUER_UUID>`.
- `GRAM_ASSISTANT_KIT_GKE_WORKLOAD_POOL_PROJECT=<CLUSTER_PROJECT_ID>`
  (`--assistant-kit-gke-workload-pool-project`). Required Google Cloud project
  ID owning the cluster workload pool, independently of the IAM project; no
  project-number, issuer-URL, or IAM-project fallback is supported.
- Remove `GRAM_ASSISTANT_KIT_WORKLOAD_ISSUER` and
  `GRAM_ASSISTANT_KIT_WORKLOAD_JWKS_URL` from GKE deployments. These remain
  supported only for local external assertions and are rejected in GKE mode.
  GKE verifies metadata tokens using `https://accounts.google.com` and
  `https://www.googleapis.com/oauth2/v3/certs`.

Failed projected-token admissions are not v1beta1 warm-pool authority. On
retry (or retirement), the application recognizes only the old, pre-pool
projected subject with `provisioning=true`, no recorded pod, and no checkpoint.
It verifies the persisted runtime/project/assistant binding and each object's
UID, ownership labels, and compatibility annotation, refuses assigned claims
or claim-owned pods, fences credentials, then
deletes only the recorded v1alpha1 claim/template, Secret, and Kubernetes
service account. That projected subject never had a Google IAM account.
The authority is cleared only after cleanup succeeds; readmission provisions a
fresh metadata-identity generation. Cleanup failures retain evidence for retry.
Live pods, snapshots, mismatched resources, and unrecorded orphans are not swept
or automatically reinterpreted as current authority. No manual database reset
or bulk cluster deletion is part of this rollout.

The MCP resource must be an enabled attachment of the assistant. Bootstrap
registers a project-scoped Google issuer and a generation-specific numeric IAM
service-account `uniqueId` subject assigned to the assistant's existing agent.
Project-scoped inference authority must already be available to the owner;
existing agent policies are not silently expanded. The issuer permits cached
assertion reuse for Google's one-hour ID tokens. An email is not the subject.

Each bootstrap uses a separate IAM service account with the `kit-wl-` prefix
and a separate Kubernetes service account (KSA). The IAM account grants only
`roles/iam.workloadIdentityUser` to that exact namespace/KSA principal; the KSA
annotation links to its email. This implementation grants it no generic Google API roles. A
retired numeric subject must never be admitted again: rotate identity on cold
boot/restore and retain failed cleanup evidence for retry.

Set `GRAM_ASSISTANT_KIT_GKE_IAM_PROJECT` to the existing application project
for the environment. It may equal `GRAM_ASSISTANT_KIT_GKE_WORKLOAD_POOL_PROJECT`;
a separate identity project is not required. The runtime provisioner requires
`iam.serviceAccounts.create`, `iam.serviceAccounts.get`,
`iam.serviceAccounts.delete`, `iam.serviceAccounts.getIamPolicy`, and
`iam.serviceAccounts.setIamPolicy` in the selected IAM project. This custom
role has project-wide service-account scope; the `kit-wl-` naming prefix and
application ownership checks do not restrict the principal's IAM permissions
to generated accounts.

Enable Workload Identity Federation for the assistant cluster. The exact member
is `serviceAccount:<CLUSTER_PROJECT_ID>.svc.id.goog[<NAMESPACE>/<KSA>]`, using the
cluster workload-pool project whether or not it equals the IAM project. The
implementation grants no project roles to generated workload accounts. Their
effective Google API access can nevertheless include grants from project,
folder, organization, or resource policies, including broad principal sets;
account-level IAM policy checks do not establish an isolated project boundary.

The managed GKE metadata server uses its documented link-local HTTP endpoint,
not an external plaintext credential service. NetworkPolicy applies to the whole
Pod: both Kit and its token sidecar can request metadata tokens, including for
other audiences. The sidecar is a token-file adapter, not an isolation boundary.
Do not configure other services to trust these workload identities. The gateway
still verifies its exact audience and generation-specific subject.

The metadata refresh sidecar requests the exact exchange audience from the
link-local metadata server with `Metadata-Flavor: Google`, a bounded response,
no redirects, and a timeout. It writes a mode-0600 temporary file and atomically
renames it in a memory-backed `emptyDir`. Kit mounts the whole directory
read-only at `/var/run/secrets/speakeasy/workload`, without `subPath`; bootstrap
Secrets, rootfs and snapshots never contain assertions. A native sidecar
startup probe checks valid token expiry before Kit starts. Refresh retries
before expiry, and removes expired assertions on failure without logging tokens.
`automountServiceAccountToken` stays false, with no explicit service-account
token or trust-bundle projections (managed sandbox core policy rejects those).

The Kit path uses `extensions.agents.x-k8s.io/v1beta1` SandboxTemplate,
SandboxWarmPool, and SandboxClaim objects with explicit `warmPoolRef`. The
per-generation pool is not a shared workload identity pool. Keep the legacy
runtime's API path unchanged. Permit namespace-scoped get/create/delete for
these resources (`list` also for SandboxClaims), plus existing Secret and KSA
permissions. Network policies are managed by the sandbox controller. The
server also needs create on `authorization.k8s.io/selfsubjectaccessreviews`
for fail-closed preflight authorization checks.
Pods tolerate `workload-type=sandbox:NoSchedule` and retain gVisor isolation.

Pools request zero spare replicas so the controller cannot replenish another
pod with the same generation's credentials after adoption. This relies on the
upstream v1beta1 cold-start path, not an assumption that every managed release
is identical. At upstream commit
[`c4ed9ba962270bf2c34d868e8c92048e85038899`](https://github.com/kubernetes-sigs/agent-sandbox/commit/c4ed9ba962270bf2c34d868e8c92048e85038899):
[`SandboxWarmPoolSpec` permits zero](https://github.com/kubernetes-sigs/agent-sandbox/blob/c4ed9ba962270bf2c34d868e8c92048e85038899/extensions/api/v1beta1/sandboxwarmpool_types.go#L40-L46),
[an empty pool falls through to cold start](https://github.com/kubernetes-sigs/agent-sandbox/blob/c4ed9ba962270bf2c34d868e8c92048e85038899/extensions/controllers/sandboxclaim_controller.go#L1232-L1249),
and [`reconcileActive` creates from the pool template](https://github.com/kubernetes-sigs/agent-sandbox/blob/c4ed9ba962270bf2c34d868e8c92048e85038899/extensions/controllers/sandboxclaim_controller.go#L668-L698).
Confirm the managed GKE controller implements this contract before rollout.
Do not change replicas to one as a workaround: replenishment can duplicate
credential-bearing pods even in a per-generation pool.

IAM recovery uses namespace-scoped ConfigMaps as a non-secret pending-operation
journal, written before account creation. Grant `get`, `create`, `update`, and
`delete` on ConfigMaps. Keep these records until UID-fenced cleanup completes;
do not delete them merely because an IAM request timed out. A definitive create
rejection can be cleared, but an uncertain result stays fenced until reconciled.

### Artifact-first rollout order

1. Open the application PR without promoting the server. The existing
   `.github/workflows/pr.yaml` `assistant-runtime-image` job runs for server
   changes on non-Dependabot PRs and uses `docker/build-push-action` with
   `push: true`. It publishes `pr-<NUMBER>`, content-hash and SHA tags.
2. Wait for that image build (including metadata-script tests) to succeed;
   resolve its registry digest. No image digest is invented in this change.
3. Configure the five-permission provisioner role in the existing application
   project and the ConfigMap/WarmPool RBAC.
   Do not remove the existing issuer/JWKS configuration while old application
   images are still selected.
4. In **one infrastructure revision**, pin the published metadata-sidecar digest
   in `GRAM_ASSISTANT_KIT_GKE_METADATA_IMAGE`, select the new server and worker
   application image digests, set `GRAM_ASSISTANT_KIT_GKE_IAM_PROJECT` and
   `GRAM_ASSISTANT_KIT_GKE_WORKLOAD_POOL_PROJECT`,
   and remove the local issuer/JWKS configuration. Apply these paired images and
   settings together; do not deploy the configuration cleanup with old application
   images or the new application with old issuer/JWKS settings. Include all
   environments and preview defaults that enable Kit. Missing/invalid metadata
   image, IAM project, or workload-pool project is a deliberate startup validation error.
   Main's release-dispatch job waits for the assistant runtime image build, but
   that alone does not populate infrastructure image pins or configuration.
5. Validate authorized managed-sandbox startup/rotation/cleanup before enabling
   new Kit sessions. No fallback to a shared IAM identity or mutable image tag
   is permitted.

Quota is a rollout gate: every simultaneously live generation consumes an IAM
service account; failed deletions also consume quota until reconciled. Size
project service-account quota for concurrency and recycle overlap before
opting in. Do not work around quota exhaustion with a shared identity.

MCP and inference use Kit's generic workload exchange configuration, not native
API keys or confidential OAuth clients. Gateway authentication remains separate
from these outbound credentials. No live reset or deployment is implied by
configuring the replacement. Never place workload assertions or exchanged tokens
in CLI arguments or logs. Never substitute human credentials or inference
bearers for MCP authentication.

## Snapshot deployment gate

Snapshot infrastructure is deployed separately. Do not infer checkpoint success
from creating a manual trigger or observing a running Pod. A usable checkpoint
requires authoritative trigger completion, source Pod UID correlation, selected
snapshot availability, and unexpired backing data. Cold restore must select that
exact checkpoint and preserve its runtime lineage and compatibility identity.

Set `GRAM_ASSISTANT_KIT_GKE_SNAPSHOTS_ENABLED=true` only with a matching,
manual `PodSnapshotPolicy` whose `snapshotScope` is explicitly `rootfs-only`.
This scope requires GKE **1.35.3-gke.1031000 or later**; the general Pod snapshot
minimum version is not sufficient. Verify the target cluster supports this scope
and that source and restore node versions and container images satisfy Google's
[rootfs-only snapshot matching requirements](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/pod-snapshots#snapshot-matching).
A control-plane upgrade alone does not establish node compatibility.

Install the managed GKE snapshot driver/controller separately before enabling snapshots.
The runtime only validates its API; the CLI does not install the controller. Whole-pod snapshots, the default
whole-pod scope, ambiguous policies, stale controller status, and unsupported
status schemas fail closed. Merely deploying the snapshot CRDs is not sufficient.

Kit session files remain on the container root filesystem, not an excluded
workspace volume. Restore starts a fresh gateway process. Before the gateway
listens, bootstrap verifies the checkpoint marker, removes the restored
credential directory, and installs newly issued inference, MCP, and gateway
credentials. A missing or mismatched marker rejects silent cold fallback.

Grant the runtime Kubernetes principal these namespace-scoped permissions in
addition to Secret, NetworkPolicy, SandboxTemplate, and SandboxClaim resource
management:

| API group            | Resource                    | Required verbs                      | Purpose                                                                |
| -------------------- | --------------------------- | ----------------------------------- | ---------------------------------------------------------------------- |
| Core (`""`)          | `serviceaccounts`           | `get`, `create`, `delete`           | Provision and retire generation-specific linked identities             |
| Core (`""`)          | `configmaps`                | `get`, `create`, `update`, `delete` | Persist and retire IAM recovery intents                                |
| Core (`""`)          | `pods`                      | `get`, `list`, `delete`             | Discover claim Pods, verify source/restore identity, and reap runtimes |
| `podsnapshot.gke.io` | `podsnapshotpolicies`       | `get`, `list`                       | Select and validate the manual rootfs-only policy                      |
| `podsnapshot.gke.io` | `podsnapshotmanualtriggers` | `create`, `get`, `delete`           | Request, observe, and clean up checkpoints                             |
| `podsnapshot.gke.io` | `podsnapshots`              | `get`, `delete`                     | Validate selected snapshots and clean up backing checkpoint resources  |

The managed snapshot controller creates `PodSnapshot` objects; the runtime
principal does not need `create` on `podsnapshots`. Observation-only snapshot
permissions are insufficient because checkpoint requests and cleanup perform
writes. Apply the matching infrastructure contract before opting in.

Checkpoint and restore failures retain the canonical session and fail closed.
Do not delete lifecycle authority or bypass its UID checks to resume a runtime.
Ambiguous partial resource creation requires reconciliation; a running Pod alone
is not permission to admit work.

Before rollout, verify the complete checkpoint, deletion, fresh-process restore,
and authenticated session-resume sequence in the target GKE deployment. Unit and
fake-client coverage does not establish live cluster or controller compatibility.

References:

- [Trigger a Pod snapshot](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots-trigger)
- [Restore a Pod snapshot](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots)
- [Snapshot matching](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/pod-snapshots#snapshot-matching)

## Scope and local verification

Platform MCP parity: no new tool or schema is needed. These are internal
provisioning, credential refresh, and lifecycle-fencing changes; existing
assistant operations and caller authorization remain unchanged. Demo seed:
no dashboard data model, new visible resource, or telemetry dimension changes;
no new seed rows are needed, and live IAM accounts must not be seeded.

```sh
mise exec -- bun test agents/runtime-image/kit-workload-token.test.mjs
mise run test:server ./internal/assistants -run '^TestKitGKE' -count=1
mise run test:server ./cmd/gram -run '^TestKit' -count=1
mise run test:server ./internal/usersessions/assertion/workload -count=1
```

Fake Kubernetes/IAM and local refresh tests do not establish managed-controller
compatibility. Validate pool creation, metadata startup, rotation, exact-subject
rejection, checkpoint/restore and eventual cleanup in an authorized deployment
before production opt-in; this change does not itself perform a live rollout.
