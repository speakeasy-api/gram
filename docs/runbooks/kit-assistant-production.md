# Configure production Kit assistants

The Kit GKE installer is an explicit deployment opt-in. It selects runtime v3
for newly created assistants only. Existing assistants and conversations retain
their runtime and canonical transcript. This path does not import legacy history
or migrate existing conversations.

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
The CLI installs the managed snapshot driver. Whole-pod snapshots, the default
whole-pod scope, ambiguous policies, stale controller status, and unsupported
status schemas fail closed. Merely deploying the snapshot CRDs is not sufficient.

Kit session files remain on the container root filesystem, not an excluded
workspace volume. Restore starts a fresh gateway process. Before the gateway
listens, bootstrap verifies the checkpoint marker, removes the restored
credential directory, and installs newly issued inference, MCP, and gateway
credentials. A missing or mismatched marker rejects silent cold fallback.

The Kubernetes principal needs the lifecycle object's namespace-scoped
permissions, including ConfigMap create/get/update for UID/resourceVersion-fenced
authority, Secret and sandbox resource management, and snapshot policy/trigger/
snapshot observation. Apply the matching infrastructure contract before opting in.

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
