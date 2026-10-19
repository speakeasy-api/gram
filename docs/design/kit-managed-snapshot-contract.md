# Kit managed snapshot contract

## Authority and scope

This contract uses Google's public GKE documentation, not an inferred CRD or a successful live-cluster test. The CRD reference was fetched again during this investigation; the concept, trigger, prepare, and restore pages were inspected from locally cached copies of those authoritative pages. ADC was unavailable, so this document does not claim a live checkpoint/restore succeeded.

- [CRD reference](https://docs.cloud.google.com/kubernetes-engine/docs/reference/crds/podsnapshot)
- [Concepts: contents and compatibility](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/pod-snapshots)
- [Prepare the environment](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots-prepare)
- [Trigger and verify snapshots](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots-trigger)
- [Restore and manage snapshots](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots)

The infra #4331 worktree was inspected read-only: `infra/helm/gram-assistant-sandbox/templates/{snapshots,rbac,sandboxtemplate}.yaml` and `infra/terraform/base/assistants.tf`. No infrastructure edits are included here.

## Concrete checkpoint driver

Use `podsnapshot.gke.io/v1`. Create a namespaced `PodSnapshotManualTrigger` with Kubernetes metadata and exactly this documented spec:

```yaml
apiVersion: podsnapshot.gke.io/v1
kind: PodSnapshotManualTrigger
metadata:
  name: CHECKPOINT_OPERATION_NAME
  namespace: RUNTIME_NAMESPACE
spec:
  targetPod: RUNTIME_POD_NAME
```

The target must match a policy with `spec.triggerConfig.type: manual`. A complete compatible driver must read that same trigger until `status.snapshotCreated` names the controller-created `PodSnapshot`, then reads that snapshot in the same namespace until its `Ready` condition is true. The restore guide explicitly names `Ready`; the reference documents `status.conditions` but does not enumerate condition types for the trigger. Do not invent `Succeeded` or `Completed` trigger conditions. Trigger creation or a nonempty snapshot name alone is not evidence of a completed checkpoint. Bound waits and report timeout/controller conditions without deleting the live runtime on failure.

The driver must not create or update a `PodSnapshot`: Google explicitly reserves that to GKE. Its only documented spec field is immutable `spec.policyName`. Documented snapshot status fields are `conditions`, `lastAccessTime`, `observedCheckpointRetryCount`, `observedGeneration`, and `storageStatus.gcs.observedGCSPath`. The trigger status documents `conditions`, `observedGeneration`, and `snapshotCreated`.

Persist the actual snapshot name plus namespace, policy identity, and application-owned lineage/compatibility information after readiness. `observedGCSPath` is the documented optional storage-location evidence; it is not a restore input. The API reference does not provide a snapshot digest, content hash, signed completion receipt, source Pod UID, or a manifest of captured files. Do not substitute a random UUID, object name, or locally calculated identifier for a provider content digest. Application-owned fields must be labeled as such, not claimed as CRD fields.

## Implemented adapter boundary

The default `KitGKEManagedCheckpointDriver` now constructs without a deployment-specific injection. It resolves actual policy selectors and follows `status.snapshotCreated` with trigger/source UID and observed-generation checks. Selection alone does not authorize checkpoint completion. With the current volume layout, both documented snapshot scopes are incompatible, so the adapter rejects before credential fencing, authority mutation, trigger creation, or source deletion. It does not yet implement successful checkpoint availability, restore, or credential refresh; those operations fail closed rather than returning simulated success. Tests use the documented resource fields and fake Kubernetes clients, not live GKE.

Platform MCP and demo-seed assessment: this is internal runtime lifecycle enforcement, not a new member operation or dashboard data feature. No Platform MCP tool or seed change is needed.

## Exact restoration controls

Create the replacement Pod in the snapshot namespace with this annotation:

```yaml
metadata:
  annotations:
    podsnapshot.gke.io/ps-name: SNAPSHOT_NAME
```

There is no documented `PodSnapshotRestore` resource, `spec.snapshotName` on a Pod, restore command override, or filesystem-only flag on an individual restore. Snapshot scope is a **policy** setting, not a restore setting. Without the annotation, GKE selects the newest compatible snapshot. The restore guide explicitly warns that an absent or non-Ready named snapshot causes a cold start. The annotation is therefore not a fail-closed restore guarantee. A Pod reaching Kubernetes Ready alone cannot prove that the named snapshot was restored; application startup must validate checkpoint state before accepting resumed work. This is a requirement for the implementation, not a request to defer implementation pending deployment verification.

Compatibility still applies: namespace, grouping label values, and scope-specific rules must match. Whole-pod restore compares a distilled Pod-spec hash, machine series/architecture, and sandbox kernel/GPU driver versions. Rootfs-only skips the distilled-spec hash and permits different resources/environments and machine families, but image/node compatibility still applies.

## Memory, filesystems, and fresh processes

The concepts page's snapshot-contents table includes process memory, execution threads, CPU registers, open file descriptors, container rootfs, `emptyDir`, and tmpfs. It excludes PVCs and other unlisted volume types. Active external connections close on restore, while listening sockets, loopback connections, and Unix sockets are captured. Whole-pod restoration resumes original process state; merely provisioning a new Pod is **not** a new application process. Environment variables are memory state and are not reliably replaced during restore; the concepts page expressly warns about this.

`spec.snapshotScope: rootfs-only` is the documented native route to restoring filesystem state without restoring process memory. The CRD and trigger guides require GKE `1.35.3-gke.1031000` or later for this scope; the inspected prepare guide requires `1.35.3-gke.1234000` or later for cluster preparation. Use the stricter preparation requirement when provisioning. Rootfs-only promises **only the container root filesystem**, not tmpfs/emptyDir persistence. The generic whole-pod contents table must not be used to extend that narrower promise.

A concrete native fresh-process design is therefore:

1. Keep durable Kit session/workspace/checkpoint files on the writable container root filesystem, not on an `emptyDir` or tmpfs mount. Flush the application checkpoint before triggering.
2. Keep credentials and other intentionally ephemeral material on mounts that are not part of that rootfs checkpoint. Do not copy them into durable state.
3. Select a policy configured with `snapshotScope: rootfs-only`; take the manual checkpoint and wait for its Ready state as above.
4. Create the replacement Pod with the exact snapshot-name annotation and matching grouping labels. Normal process startup loads the persisted session state, validates its expected checkpoint identity, provisions fresh credentials, and only then becomes application-ready.

This is a supported combination of documented capture scope and application startup behavior, not a claimed native GKE session-resume API. It requires application/infra changes to file placement and policy scope.

For the existing whole-pod policy, the exact incompatibility is: **GKE cannot natively restore tmpfs/emptyDir contents while discarding captured process memory through any field in the published API.** A separate application supervisor could deliberately terminate old children and spawn new Kit processes after restore, with readiness and credential fencing, but that is application lifecycle engineering. It must not be described as GKE fresh-process restore. In particular, a signal sent after a restored gateway is already serving does not prevent old code or credentials from running first.

## Infra #4331 assessment

The inspected chart has:

- Storage config `gram-kit`, GCS path `gram-kit`, and documented `tokenSource: federatedP4SA`.
- Namespaced policy `gram-kit`, selecting `gram.ai/runtime-version: "3"` and `gram.ai/snapshots: "enabled"`.
- `snapshotScope: whole-pod`, manual triggers, and `postCheckpoint: resume`.
- `lastAccessTimeout: 24h` and two snapshots per group. Group labels are `gram.ai/assistant-id`, `gram.ai/runtime-version`, `gram.ai/runtime-lineage`, and `gram.ai/compatibility-generation`; these labels must be present at capture and restore.
- Role permissions to create/get/list/watch/delete manual triggers and get/list/watch/delete snapshots. No snapshot create/update permission is needed. The concrete driver additionally requires namespaced `get`/`list` on `podsnapshotpolicies`; the inspected Role does not grant those permissions. That infrastructure permission change is still required and was not applied.
- Memory-backed `emptyDir` mounts at `/var/lib/gram-assistant/work` and `/tmp`.
- An optional cluster `pod_snapshot_config.enabled` setting, with a comment explicitly stating that enabling it does not upgrade cluster/node versions.

Those resources support a concrete **whole-pod** checkpoint driver today at the API-contract level. They do not implement the rootfs-only fresh-process design: changing policy scope alone would stop guaranteeing persistence of the existing work mount. The necessary change is explicit: relocate durable state to rootfs and select rootfs-only, or implement a fenced restart supervisor for whole-pod restore. No undocumented field can bridge this mismatch.
