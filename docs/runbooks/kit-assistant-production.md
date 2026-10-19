# Configure production Kit assistants

The Kit GKE installer is an explicit deployment opt-in. It selects runtime v3
for newly created assistants only. Existing assistants and conversations retain
their runtime and canonical transcript. This path does not import legacy history
or migrate existing conversations.

## ACP transport deployment gate

Do not enable production Kit workloads until the server-to-gateway ACP connection
has verified, authenticated encryption. The current backend connects to the Pod
over HTTP and sends a bearer credential. CIDR restrictions and NetworkPolicy do
not encrypt this traffic. HTTPS inference and MCP endpoints do not protect this
separate connection.

Verified gateway TLS or an authenticated encrypted mesh requires a coordinated
runtime and infrastructure contract; neither is established by this installer.
Keep production opt-in disabled until that contract is implemented and tested.

## Deployment inputs

Use the released Kit image with an explicit version tag (not `latest`), or an
explicit digest. Do not use local relay settings in production.

Configure the server and worker consistently:

- `GRAM_ASSISTANT_RUNTIME_PROVIDER=gke`.
- `GRAM_ASSISTANT_KIT_IMAGE=<REGISTRY>/<IMAGE>:<VERSION>`.
- Existing assistant GKE cluster endpoint, CA, namespace and runner CIDR settings.
- `GRAM_ASSISTANT_KIT_GKE_INGRESS_CIDR=<SERVER_SOURCE_CIDR>`. This is the
  server source network permitted to reach gateway pods, not an unrestricted
  public ingress rule.
- CPU/memory request and limit settings under `GRAM_ASSISTANT_KIT_GKE_*`.
  Workspace and temporary volume limits are separately configurable.
- `GRAM_ASSISTANT_KIT_MCP_RESOURCE=https://<RESOURCE_HOST>/mcp/<SLUG>`.
- `GRAM_ASSISTANT_KIT_MCP_ISSUER=https://<EXPECTED_ISSUER>` and
  `GRAM_ASSISTANT_KIT_MCP_ISSUER_ID=<PERSISTED_ISSUER_UUID>`.
- Optional `GRAM_ASSISTANT_KIT_ROOT_TRIGGER_ID=<ROOT_TRIGGER_UUID>` when the
  assistant has more than one root.
- `GRAM_ASSISTANT_KIT_MCP_AUTH_METHOD=client_secret_basic` (default), or
  `private_key_jwt` for ephemeral ES256 startup credentials.

The MCP resource must be an enabled attachment of the assistant and use the
configured workload issuer. Bootstrap resolves the persisted assistant/root
identity, revokes the prior workload clients, provisions a new confidential
client, and rotates inference and gateway credentials. Credentials belong in
protected startup files, never image layers, CLI arguments or logs. The native
Speakeasy provider connects to `https://ai.speakeasy.com` with normal TLS
verification. Do not substitute human credentials or an inference bearer for
MCP authentication. Built-in Platform MCP endpoints are not part of this
attached-resource machine-auth contract.

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

The CLI installs the managed snapshot driver. Whole-pod snapshots, the default
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
| Core (`""`)          | `configmaps`                | `create`, `get`, `update` | UID/resourceVersion-fenced lifecycle authority                         |
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
