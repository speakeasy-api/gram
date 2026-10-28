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
  and the digest-pinned `docker.io/curlimages/curl` image used by the identity
  sidecar. The sandbox has two ordinary containers (`kit`, `acp-tls`) and a
  native restartable init sidecar (`workload-token`), UID/GID 1000, no
  capabilities, and automatic service-account token mounting disabled.
- Keep server/worker ingress limited to the configured guest TCP port and
  source CIDR. The guest port now speaks TLS, not HTTP. Do not expose port
  17766; it is loopback-only and excluded from ingress policy.
- Permit the existing bootstrap Secret projection plus a read-only `tls`
  projection and 32 MiB memory-backed `tls-tmp` volume. No extra RBAC, external
  CA, DNS record, shared private key, or load balancer is needed.
- Budget the proxy's 10m CPU/16 MiB requests and 100m CPU/64 MiB limits in
  addition to Kit resources. The identity sidecar adds 10m CPU/8 MiB requests
  and 100m CPU/32 MiB limits. Rootfs-only snapshot policy must cover this
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
- `GRAM_ASSISTANT_KIT_GKE_PROJECT_NUMBER`, `GRAM_ASSISTANT_KIT_GKE_ORGANIZATION_ID`,
  `GRAM_ASSISTANT_KIT_GKE_CLUSTER_LOCATION` and `GRAM_ASSISTANT_KIT_GKE_CLUSTER_NAME`:
  the numeric project number and organization ID owning the assistant cluster,
  and its exact location and name. The server derives the Agent Identity issuer,
  JWKS and each workload's SPIFFE subject from these values, never from token
  claims. Missing or non-numeric values are a startup validation error.
- Existing assistant GKE cluster endpoint, CA, namespace and runner CIDR settings.
- `GRAM_ASSISTANT_KIT_GKE_INGRESS_CIDR=<SERVER_SOURCE_CIDR>`. This is the
  server source network permitted to reach gateway pods, not an unrestricted
  public ingress rule.
- CPU/memory request and limit settings under `GRAM_ASSISTANT_KIT_GKE_*`.
  Workspace and temporary volume limits are separately configurable.
- `GRAM_ASSISTANT_KIT_MCP_RESOURCE=https://<RESOURCE_HOST>/mcp/<SLUG>`.
- `GRAM_ASSISTANT_KIT_MCP_ISSUER=https://<EXPECTED_ISSUER>` and
  `GRAM_ASSISTANT_KIT_MCP_ISSUER_ID=<PERSISTED_ISSUER_UUID>`.
- `GRAM_ASSISTANT_KIT_WORKLOAD_ISSUER` and `GRAM_ASSISTANT_KIT_WORKLOAD_JWKS_URL`
  are used only by local Kit. GKE mode ignores them, so they can stay set while
  older application images are still deployed and be removed afterwards.

## Workload identity

Kit authenticates with GKE Agent Identity, which requires GKE
`1.37.0-gke.3503000` or later, the Agent Registry API and, on Standard clusters,
Workload Identity Federation for GKE with the GKE metadata server on sandbox
node pools. Each bootstrap
generation gets its own Kubernetes service account (KSA) with no Google IAM
service-account link. The Pod is annotated with
`iam.gke.io/identity: "spiffe://<TRUST_DOMAIN>/*"`, where the trust domain is
`agents.global.org-<ORG_ID>.system.id.goog`. No Pod certificates are injected and
`automountServiceAccountToken` stays false.

Google signs ID tokens for the Pod with:

- issuer `https://sts.googleapis.com/v1/organizations/<ORG_ID>/locations/global/workloadIdentityPools/<TRUST_DOMAIN>`;
- JWKS `<ISSUER>/openid/jwks`;
- subject `spiffe://<TRUST_DOMAIN>/resources/container/projects/<PROJECT_NUMBER>/locations/<LOCATION>/clusters/<CLUSTER>/ns/<NAMESPACE>/sa/<KSA>`.

The subject identifies a KSA, not a Pod. Bootstrap registers the issuer and
assigns the generation's exact subject to the assistant's existing agent.
Project-scoped inference authority must already be available to the owner;
existing agent policies are not silently expanded. A retired subject is never
admitted again, including after the issuer is deleted and recreated.

Kit reads its assertion from a file and exchanges it at the Speakeasy token
endpoint (RFC 7523). Kit has no Google-specific code. The `workload-token`
native sidecar adapts the metadata server to that file contract. It runs a
short POSIX shell script in a pinned `curlimages/curl` image:

1. `GET http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/identity?audience=<AUDIENCE>&format=full`
   with `Metadata-Flavor: Google`, a 10-second timeout and a 16 KiB limit.
2. Write the response to a temporary file with mode `0600` and rename it over
   the assertion in a memory-backed `emptyDir`. Refresh every minute; the
   metadata server returns its cached token until that token nears expiry.
3. On failure, retry every 5 seconds and delete an assertion that has not been
   refreshed for 50 minutes, because Google ID tokens live for one hour.

Kit mounts the directory read-only at `/var/run/secrets/speakeasy/workload`,
without `subPath`. The sidecar startup probe holds Kit until an assertion
exists. Bootstrap Secrets, the root filesystem and snapshots never contain
assertions. The sidecar never logs tokens.

The sidecar does not inspect the token. The Speakeasy verifier is the single
authority: it checks the signature against the derived JWKS, the exact issuer,
subject and audience, expiry and lifetime, and it refuses key-bound tokens
(any `cnf` claim) and non-bearer `typ` values.

Security properties to keep in mind:

- NetworkPolicy applies to the whole Pod, so Kit and the sidecar can both
  request metadata tokens, including for other audiences. The sidecar is a file
  adapter, not an isolation boundary. Do not configure other services to trust
  these identities.
- Tokens are bearer credentials. Revocation happens at Speakeasy: retiring a
  generation's subject rejects further exchanges and existing sessions.
- The managed sandbox policy allows only TCP/80 to `169.254.169.254` as
  link-local egress.

Failed projected-token admissions are not v1beta1 warm-pool authority. On
retry (or retirement), the application recognizes only the old, pre-pool
projected subject with `provisioning=true`, no recorded pod, and no checkpoint.
It verifies the persisted runtime/project/assistant binding and each object's
UID, ownership labels, and compatibility annotation, refuses assigned claims
or claim-owned pods, fences credentials, then deletes only the recorded
v1alpha1 claim/template, Secret, and KSA. The authority is cleared only after
cleanup succeeds; readmission provisions a fresh generation. Cleanup failures
retain evidence for retry. Live pods, snapshots, mismatched resources, and
unrecorded orphans are not swept or automatically reinterpreted as current
authority. No manual database reset or bulk cluster deletion is part of this
rollout.

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

### Rollout order

1. Infrastructure first: upgrade the assistant cluster to a GKE version with
   Agent Identity, confirm Workload Identity Federation is enabled, enable the
   Agent Registry API, and add the four
   `GRAM_ASSISTANT_KIT_GKE_*` identity values. Keep the existing issuer/JWKS
   values; the currently deployed application still needs them.
2. Deploy the application. It ignores the issuer/JWKS values in GKE mode.
3. Validate a fresh Kit claim end to end (sidecar startup, exchange, inference,
   MCP, a reply in a real conversation, and refresh past one hour) before
   enabling new Kit sessions in the next environment.
4. Remove the issuer/JWKS values from GKE deployments.

MCP and inference use Kit's generic workload exchange configuration, not native
API keys or confidential OAuth clients. Gateway authentication remains separate
from these outbound credentials. Never place workload assertions or exchanged
tokens in CLI arguments or logs. Never substitute human credentials or inference
bearers for MCP authentication.

References:

- [About Agent Identity for GKE](https://cloud.google.com/kubernetes-engine/security/about-agent-identity)
- [Request Agent Identity](https://cloud.google.com/kubernetes-engine/security/request-agent-identity)
- [Authenticate with Agent Identity](https://cloud.google.com/kubernetes-engine/security/authenticate-agent-identity)

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

| API group            | Resource                    | Required verbs            | Purpose                                                                |
| -------------------- | --------------------------- | ------------------------- | ---------------------------------------------------------------------- |
| Core (`""`)          | `serviceaccounts`           | `get`, `create`, `delete` | Provision and retire generation-specific identities                    |
| Core (`""`)          | `pods`                      | `get`, `list`, `delete`   | Discover claim Pods, verify source/restore identity, and reap runtimes |
| `podsnapshot.gke.io` | `podsnapshotpolicies`       | `get`, `list`             | Select and validate the manual rootfs-only policy                      |
| `podsnapshot.gke.io` | `podsnapshotmanualtriggers` | `create`, `get`, `delete` | Request, observe, and clean up checkpoints                             |
| `podsnapshot.gke.io` | `podsnapshots`              | `get`, `delete`           | Validate selected snapshots and clean up backing checkpoint resources  |

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
no new seed rows are needed.

```sh
mise run test:server ./internal/assistants -run '^TestKitGKE' -count=1
mise run test:server ./cmd/gram -run '^TestKit' -count=1
mise run test:server ./internal/usersessions/assertion/workload -count=1
```

Fake Kubernetes tests do not establish managed-controller
compatibility. Validate pool creation, metadata startup, rotation, exact-subject
rejection, checkpoint/restore and eventual cleanup in an authorized deployment
before production opt-in; this change does not itself perform a live rollout.
