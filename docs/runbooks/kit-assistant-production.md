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

The public GKE documentation describes `spec.targetPod` for
`PodSnapshotManualTrigger` and the `AllSnapshotsAvailable` display status, but
omits the complete trigger/snapshot status schema. A deployment-verified `KitGKECheckpointDriver` is required before enabling
`GRAM_ASSISTANT_KIT_GKE_SNAPSHOTS_ENABLED`. The CLI does not currently supply
this adapter: snapshot-enabled installation fails closed until the deployed
CRD status contract and cold-process/session-file restore mechanism are verified
and wired. Merely deploying the snapshot CRDs is not sufficient.
The Kubernetes principal also needs namespace-scoped ConfigMap create/get/update
permissions for UID/resourceVersion-fenced lifecycle authority.

Unavailable or mismatched evidence must fail closed; it must not authorize
stopping a runtime or discarding its canonical session binding.

References:

- [Trigger a Pod snapshot](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots-trigger)
- [Restore a Pod snapshot](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots)
- [Snapshot matching](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/pod-snapshots#snapshot-matching)

## Product surface assessment

Platform MCP tools retain the existing assistant create/read/chat outcomes and
permissions. Runtime provisioning and checkpoint fencing are internal operator
machinery, not new member-facing tools. The configured workload MCP connection
uses the existing issuer-gated resource contract. No new Platform MCP tool is
appropriate for raw Kubernetes lifecycle or credential operations.

No demo seed change is required: this change adds no dashboard entity, field,
or telemetry dimension. The shared read-only demo must not provision live
production sandbox credentials as seed data.
