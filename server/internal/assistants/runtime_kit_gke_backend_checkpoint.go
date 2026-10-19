package assistants

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var kitGKEAuthorityGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
var kitGKETriggerGVR = schema.GroupVersionResource{Group: "podsnapshot.gke.io", Version: "v1", Resource: "podsnapshotmanualtriggers"}
var kitGKESnapshotGVR = schema.GroupVersionResource{Group: "podsnapshot.gke.io", Version: "v1", Resource: "podsnapshots"}

// KitGKECheckpointDriver enforces the filesystem/fresh-process lifecycle contract.
// Managed rootfs-only snapshots preserve writable filesystem state, not process
// memory. A fresh projected bootstrap checks the previous boot's marker before
// launching Kit. The backend authenticates the fresh gateway before publication.
// Provider Ready conditions are documented at:
// https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots
type KitGKECheckpointDriver interface {
	KitGKESnapshotAvailabilityVerifier
	// VerifySettled reconciles gateway session/list with canonical bindings and
	// verifies ALL live/unbound sessions and jobs; DB-bound status alone is not
	// sufficient. The caller holds admission and has checked queued work.
	VerifySettled(context.Context, assistantRuntimeRecord, *unstructured.Unstructured) error
	PrepareColdRestore(context.Context, assistantRuntimeRecord, *unstructured.Unstructured, *unstructured.Unstructured, KitGKESnapshotBinding) (*unstructured.Unstructured, error)
	Select(context.Context, *unstructured.Unstructured, KitGKESnapshotBinding) (*unstructured.Unstructured, error)
	VerifyRestored(context.Context, *unstructured.Unstructured, *unstructured.Unstructured, KitGKESnapshotBinding) error
	RefreshRestored(context.Context, assistantRuntimeRecord, *unstructured.Unstructured, KitLocalBootstrap) error
}

type kitGKECheckpointRef struct {
	TriggerName  string    `json:"TriggerName"`
	TriggerUID   types.UID `json:"TriggerUID"`
	SnapshotName string    `json:"SnapshotName"`
	SnapshotUID  types.UID `json:"SnapshotUID"`
}

// These tagged persistence types preserve the existing authority JSON format
// without coupling the provisioning input structs to storage serialization.
type kitGKECheckpointIdentity struct {
	Namespace               string `json:"Namespace"`
	AssistantID             string `json:"AssistantID"`
	Lineage                 string `json:"Lineage"`
	CompatibilityGeneration string `json:"CompatibilityGeneration"`
}
type kitGKECheckpointSource struct {
	PolicyName       string                   `json:"policy_name"`
	PolicyUID        types.UID                `json:"policy_uid"`
	PolicyGeneration int64                    `json:"policy_generation"`
	CheckpointMarker string                   `json:"CheckpointMarker"`
	Identity         kitGKECheckpointIdentity `json:"Identity"`
	SourcePodName    string                   `json:"SourcePodName"`
	SourcePodUID     types.UID                `json:"SourcePodUID"`
}

func (s kitGKECheckpointSource) binding() KitGKESnapshotBinding {
	return KitGKESnapshotBinding{PolicyName: s.PolicyName, PolicyUID: s.PolicyUID, PolicyGeneration: s.PolicyGeneration, Identity: KitGKEIdentity(s.Identity), SourcePodName: s.SourcePodName, SourcePodUID: s.SourcePodUID}
}

type kitGKEAuthority struct {
	PriorCheckpoints []kitGKECheckpointRef   `json:"prior_checkpoints,omitempty"`
	Phase            string                  `json:"phase"`
	Metadata         kitGKEBackendMetadata   `json:"metadata"`
	TriggerName      string                  `json:"trigger_name,omitempty"`
	TriggerUID       types.UID               `json:"trigger_uid,omitempty"`
	SnapshotName     string                  `json:"snapshot_name,omitempty"`
	SnapshotUID      types.UID               `json:"snapshot_uid,omitempty"`
	Source           *kitGKECheckpointSource `json:"source,omitempty"`
}

func (k *KitGKERuntimeBackend) createAuthority(ctx context.Context, r assistantRuntimeRecord, m kitGKEBackendMetadata) (kitGKEBackendMetadata, error) {
	obj := kitObject("ConfigMap", "v1", kitGKEName(r)+"-authority", k.identity(r))
	labels := obj.GetLabels()
	labels["gram.ai/project-id"] = r.ProjectID.String()
	obj.SetLabels(labels)
	obj.SetAnnotations(map[string]string{"speakeasy.com/kit-spec": k.fingerprint})
	raw, err := json.Marshal(kitGKEAuthority{PriorCheckpoints: nil, Phase: "active", Metadata: m, TriggerName: "", TriggerUID: "", SnapshotName: "", SnapshotUID: "", Source: nil})
	if err != nil {
		return m, fmt.Errorf("kit GKE createAuthority: %w", err)
	}
	obj.Object["data"] = map[string]any{"authority.json": string(raw)}
	obj, err = k.config.GKE.Dynamic.Resource(kitGKEAuthorityGVR).Namespace(k.config.GKE.Namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return m, fmt.Errorf("kit GKE createAuthority: %w", err)
	}
	if obj.GetUID() == "" {
		return m, fmt.Errorf("kit GKE authority has no UID")
	}
	m.AuthorityUID = obj.GetUID()
	return m, nil
}
func (k *KitGKERuntimeBackend) loadAuthority(ctx context.Context, r assistantRuntimeRecord) (kitGKEAuthority, *unstructured.Unstructured, error) {
	var state kitGKEAuthority
	m, err := k.metadata(r)
	if err != nil {
		return state, nil, err
	}
	if m.AuthorityUID == "" {
		return state, nil, fmt.Errorf("kit GKE runtime has no persisted authority UID")
	}
	obj, err := k.get(ctx, r, kitGKEAuthorityGVR, kitGKEName(r)+"-authority", m.AuthorityUID)
	if err != nil {
		return state, nil, err
	}
	raw, found, err := unstructured.NestedString(obj.Object, "data", "authority.json")
	if err != nil || !found {
		return state, nil, fmt.Errorf("kit GKE checkpoint authority is malformed")
	}
	if err = json.Unmarshal([]byte(raw), &state); err != nil {
		return state, nil, fmt.Errorf("kit GKE loadAuthority: %w", err)
	}
	state.Metadata.AuthorityUID = m.AuthorityUID
	record := r
	record.BackendMetadataJSON, _ = json.Marshal(state.Metadata)
	if _, err = k.metadata(record); err != nil {
		return state, nil, err
	}
	switch state.Phase {
	case "active", "checkpointing", "checkpointed", "stopped", "restoring", "retired":
	default:
		return state, nil, fmt.Errorf("unknown Kit GKE checkpoint authority phase")
	}
	return state, obj, nil
}
func (k *KitGKERuntimeBackend) saveAuthority(ctx context.Context, obj *unstructured.Unstructured, state kitGKEAuthority) (*unstructured.Unstructured, error) {
	if obj.GetUID() == "" || obj.GetResourceVersion() == "" {
		return nil, fmt.Errorf("kit GKE authority update requires UID and resourceVersion")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("kit GKE saveAuthority: %w", err)
	}
	next := obj.DeepCopy()
	next.Object["data"] = map[string]any{"authority.json": string(raw)}
	updated, err := k.config.GKE.Dynamic.Resource(kitGKEAuthorityGVR).Namespace(k.config.GKE.Namespace).Update(ctx, next, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("kit GKE saveAuthority: %w", err)
	}
	if updated.GetUID() != obj.GetUID() {
		return nil, fmt.Errorf("kit GKE checkpoint authority was replaced")
	}
	return updated, nil
}

func (k *KitGKERuntimeBackend) Stop(ctx context.Context, r assistantRuntimeRecord) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.config.Provision.SnapshotsEnabled || k.config.Snapshots == nil {
		return fmt.Errorf("%s", kitGKECheckpointUnavailable)
	}
	state, obj, err := k.loadAuthority(ctx, r)
	if err != nil {
		return err
	}
	if state.Phase == "stopped" {
		return nil
	}
	if state.Phase == "restoring" || state.Phase == "retired" {
		return fmt.Errorf("cannot checkpoint Kit GKE phase %s", state.Phase)
	}
	if state.Phase == "active" {
		r.BackendMetadataJSON, _ = json.Marshal(state.Metadata)
		p, err := k.protocol(ctx, r)
		if err != nil {
			return err
		}
		coordinator, ok := p.(runtimeSwitchCoordinator)
		if !ok {
			return errRuntimeSwitchUnavailable
		}
		var assistant assistantRecord
		assistant.ID = r.AssistantID
		assistant.ProjectID = r.ProjectID
		readiness, err := coordinator.PrepareRuntimeSwitch(ctx, assistant, []assistantRuntimeRecord{r}, 3)
		if readiness.Release != nil {
			defer readiness.Release()
		}
		if err != nil {
			return fmt.Errorf("kit GKE Stop: %w", err)
		}
		if readiness.Release == nil {
			return errRuntimeSwitchUnavailable
		}
		if !readiness.Settled {
			return errRuntimeSwitchBusy
		}
		pod, e := k.get(ctx, r, gkePodGVR, state.Metadata.PodName, state.Metadata.PodUID)
		if e != nil {
			return e
		}
		if e = k.config.Snapshots.VerifySettled(ctx, r, pod); e != nil {
			return fmt.Errorf("kit GKE Stop: %w", e)
		}
		settlement, ok := p.(interface {
			VerifyCheckpointSettled(context.Context, assistantRuntimeRecord) error
		})
		if !ok {
			return errRuntimeSwitchUnavailable
		}
		if err := settlement.VerifyCheckpointSettled(ctx, r); err != nil {
			return fmt.Errorf("verify Kit checkpoint settlement: %w", err)
		}
		// Persist the admission fence before revoking credentials or triggering a
		// snapshot. Retried operations cannot re-admit a checkpointing process.
		if state.SnapshotName != "" {
			state.PriorCheckpoints = append(state.PriorCheckpoints, kitGKECheckpointRef{state.TriggerName, state.TriggerUID, state.SnapshotName, state.SnapshotUID})
		}
		state.TriggerUID = ""
		state.SnapshotName = ""
		state.SnapshotUID = ""
		state.Phase = "checkpointing"
		state.TriggerName = "kit-checkpoint-" + uuid.NewString()
		state.Source = &kitGKECheckpointSource{PolicyName: "", PolicyUID: "", PolicyGeneration: 0, CheckpointMarker: state.Metadata.CheckpointMarker, Identity: kitGKECheckpointIdentity(k.identity(r)), SourcePodName: state.Metadata.PodName, SourcePodUID: state.Metadata.PodUID}
		if driver, ok := k.config.Snapshots.(*KitGKEManagedCheckpointDriver); ok {
			policy, err := driver.checkpointPolicy(ctx, pod)
			if err != nil {
				return err
			}
			state.Source.PolicyName, state.Source.PolicyUID, state.Source.PolicyGeneration = policy.GetName(), policy.GetUID(), policy.GetGeneration()
		}
		obj, err = k.saveAuthority(ctx, obj, state)
		if err != nil {
			return err
		}
	}
	if state.Phase == "checkpointing" {
		if state.Source == nil || state.TriggerName == "" {
			return fmt.Errorf("kit GKE checkpoint source authority missing")
		}
		if err = k.config.Fence(ctx, r); err != nil {
			return err
		}
		pod, err := k.get(ctx, r, gkePodGVR, state.Source.SourcePodName, state.Source.SourcePodUID)
		if err != nil {
			return err
		}
		trigger, err := RenderKitGKEManualTrigger(true, state.TriggerName, state.Source.binding(), pod)
		if err != nil {
			return err
		}
		resource := k.config.GKE.Dynamic.Resource(kitGKETriggerGVR).Namespace(k.config.GKE.Namespace)
		if state.TriggerUID == "" {
			// A crash after Create before CAS deliberately requires operator repair;
			// an unrecorded trigger may not be adopted on labels alone.
			trigger, err = resource.Create(ctx, trigger, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("kit GKE Stop: %w", err)
			}
			state.TriggerUID = trigger.GetUID()
			if state.TriggerUID == "" {
				return fmt.Errorf("kit GKE checkpoint trigger has no UID")
			}
			obj, err = k.saveAuthority(ctx, obj, state)
			if err != nil {
				return err
			}
		} else {
			trigger, err = resource.Get(ctx, state.TriggerName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("kit GKE Stop: %w", err)
			}
			if trigger.GetUID() != state.TriggerUID {
				return fmt.Errorf("kit GKE trigger authority mismatch")
			}
		}
		selected, err := k.config.Snapshots.Select(ctx, trigger, state.Source.binding())
		if err != nil {
			return fmt.Errorf("kit GKE Stop: %w", err)
		}
		verified, err := VerifyKitGKESnapshot(ctx, selected, state.Source.binding(), k.config.Snapshots)
		if err != nil {
			return err
		}
		if _, err = k.get(ctx, r, gkePodGVR, state.Source.SourcePodName, state.Source.SourcePodUID); err != nil {
			return err
		}
		if selected.GetUID() == "" {
			return fmt.Errorf("kit GKE selected snapshot has no UID")
		}
		state.SnapshotName = verified.Name()
		state.SnapshotUID = selected.GetUID()
		state.Phase = "checkpointed"
		obj, err = k.saveAuthority(ctx, obj, state)
		if err != nil {
			return err
		}
	}
	// Revalidate selected backing data even on retries after a prior checkpoint.
	if state.Source == nil || state.SnapshotName == "" || state.SnapshotUID == "" {
		return fmt.Errorf("kit GKE checkpoint evidence missing")
	}
	selected, e := k.config.GKE.Dynamic.Resource(kitGKESnapshotGVR).Namespace(k.config.GKE.Namespace).Get(ctx, state.SnapshotName, metav1.GetOptions{})
	if e != nil {
		return fmt.Errorf("kit GKE Stop: %w", e)
	}
	if selected.GetUID() != state.SnapshotUID {
		return fmt.Errorf("kit GKE checkpoint snapshot replaced")
	}
	if _, e = VerifyKitGKESnapshot(ctx, selected, state.Source.binding(), k.config.Snapshots); e != nil {
		return e
	}
	// Durable verified authority precedes deletion. Foreground deletion is waited
	// through; no success is returned while the source process still exists.
	if err = k.deleteOwned(ctx, gkeSandboxClaimGVR, kitGKEName(r), state.Metadata.ClaimUID); err != nil {
		return err
	}
	if err = k.deleteOwned(ctx, gkePodGVR, state.Metadata.PodName, state.Metadata.PodUID); err != nil {
		return err
	}
	state.Phase = "stopped"
	_, err = k.saveAuthority(ctx, obj, state)
	return err
}

func (k *KitGKERuntimeBackend) restore(ctx context.Context, r assistantRuntimeRecord, state kitGKEAuthority, obj *unstructured.Unstructured) (_ RuntimeBackendEnsureResult, returnErr error) {
	var empty RuntimeBackendEnsureResult
	if k.config.Snapshots == nil {
		return empty, fmt.Errorf("%s", kitGKECheckpointUnavailable)
	}
	if state.Phase != "stopped" {
		return empty, fmt.Errorf("kit GKE phase %s is not safely restorable; finish checkpoint or reconcile fenced attempt", state.Phase)
	}
	if state.Source == nil || state.SnapshotUID == "" || state.SnapshotName == "" {
		return empty, fmt.Errorf("kit GKE restore authority missing")
	}
	snapshot, err := k.config.GKE.Dynamic.Resource(kitGKESnapshotGVR).Namespace(k.config.GKE.Namespace).Get(ctx, state.SnapshotName, metav1.GetOptions{})
	if err != nil {
		return empty, fmt.Errorf("kit GKE restore: %w", err)
	}
	if snapshot.GetUID() != state.SnapshotUID {
		return empty, fmt.Errorf("kit GKE restore snapshot UID mismatch")
	}
	verified, err := VerifyKitGKESnapshot(ctx, snapshot, state.Source.binding(), k.config.Snapshots)
	if err != nil {
		return empty, err
	}
	if _, err = k.config.GKE.Dynamic.Resource(gkeSandboxClaimGVR).Namespace(k.config.GKE.Namespace).Get(ctx, kitGKEName(r), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		if err != nil {
			return empty, fmt.Errorf("check kit GKE source claim deletion: %w", err)
		}
		return empty, fmt.Errorf("kit GKE restore requires source claim deletion")
	}
	template, err := k.get(ctx, r, kitTemplateGVR, kitGKEName(r), state.Metadata.TemplateUID)
	if err != nil {
		return empty, err
	}
	template, err = k.config.Snapshots.PrepareColdRestore(ctx, r, template.DeepCopy(), snapshot, state.Source.binding())
	if err != nil {
		return empty, fmt.Errorf("kit GKE restore: %w", err)
	}
	if template == nil || template.GetUID() != state.Metadata.TemplateUID || template.GetName() != kitGKEName(r) {
		return empty, fmt.Errorf("cold restore driver changed template authority")
	}
	if state.Source.CheckpointMarker == "" {
		return empty, fmt.Errorf("checkpoint has no rootfs marker; legacy restore is unsupported")
	}
	// Stopped already fences admission and the source claim is gone. Keep that
	// retryable phase through preparation: no Kubernetes resource has changed.
	// Bootstrap may mint authority even when its result is an error. Revoke that
	// authority on every failed restore, including failures after the fresh Pod
	// starts and caller cancellation. Keep the durable phase fenced for recovery.
	defer func() {
		if returnErr != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if fenceErr := k.config.Fence(cleanupCtx, r); fenceErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("revoke failed restore bootstrap: %w", fenceErr))
			}
		}
	}()
	b, err := k.config.Bootstrap(ctx, r)
	if err != nil {
		return empty, err
	}
	if err = validateKitBootstrap(b); err != nil {
		return empty, err
	}
	previousBootstrap, err := k.get(ctx, r, kitSecretGVR, state.Metadata.SecretName, state.Metadata.SecretUID)
	if err != nil {
		return empty, err
	}
	oldGateway, found, err := unstructured.NestedString(previousBootstrap.Object, "data", "gateway-token")
	if err != nil || !found || oldGateway == base64.StdEncoding.EncodeToString(b.GatewayToken) {
		return empty, fmt.Errorf("restore requires a fresh gateway authentication token")
	}
	in := k.config.Provision
	in.Identity, in.Name, in.Model = k.identity(r), kitGKEName(r), b.Model
	in.AgentsMD, in.InferenceJSON, in.MCPJSON, in.GatewayToken, in.MCPFiles = b.AgentsMD, b.InferenceJSON, b.MCPJSON, b.GatewayToken, b.MCPFiles
	in.CheckpointMarker = []byte(uuid.NewString())
	in.ExpectedCheckpointMarker = []byte(state.Source.CheckpointMarker)
	in.Restore, in.RestoreSource = verified, &verified.binding
	in.TLSCertificate, in.TLSPrivateKey, err = newKitGKETLSCertificate(in.Name + ".kit.internal")
	if err != nil {
		return empty, err
	}
	objects, err := RenderKitGKEProvision(in)
	if err != nil {
		return empty, err
	}
	labels := k.identity(r).labels()
	labels["gram.ai/project-id"] = r.ProjectID.String()
	if in.SnapshotsEnabled {
		labels["gram.ai/snapshots"] = "enabled"
	}
	annotations := map[string]string{"speakeasy.com/kit-spec": k.fingerprint}
	objects.Secret.SetLabels(labels)
	objects.Secret.SetAnnotations(annotations)
	// Cross the irreversible-resource boundary only after preparation succeeds.
	// A failed preparation leaves the exact source snapshot/UIDs unchanged and
	// the next Ensure can safely mint a different fresh bootstrap and retry.
	state.Phase = "restoring"
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	secret, err := k.config.GKE.Dynamic.Resource(kitSecretGVR).Namespace(k.config.GKE.Namespace).Create(ctx, objects.Secret, metav1.CreateOptions{})
	if err != nil {
		return empty, fmt.Errorf("create fresh restore bootstrap: %w", err)
	}
	if secret.GetUID() == "" {
		return empty, fmt.Errorf("fresh restore bootstrap lacks UID")
	}
	oldSecretName, oldSecretUID := state.Metadata.SecretName, state.Metadata.SecretUID
	state.Metadata.SecretName, state.Metadata.SecretUID = secret.GetName(), secret.GetUID()
	state.Metadata.CheckpointMarker = string(in.CheckpointMarker)
	state.Metadata.TLSCertificate = in.TLSCertificate
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	if err = k.deleteOwned(ctx, kitSecretGVR, oldSecretName, oldSecretUID); err != nil {
		return empty, err
	}
	// SandboxTemplate's spec is immutable. Replace it only after its exact UID
	// has been deleted; the restoring fence prevents concurrent admission.
	if err = k.deleteOwned(ctx, kitTemplateGVR, kitGKEName(r), state.Metadata.TemplateUID); err != nil {
		return empty, err
	}
	template = objects.Template
	template.SetLabels(labels)
	template.SetAnnotations(annotations)
	if err = unstructured.SetNestedStringMap(template.Object, labels, "spec", "podTemplate", "metadata", "labels"); err != nil {
		return empty, fmt.Errorf("set restored pod labels: %w", err)
	}
	if err = unstructured.SetNestedField(template.Object, k.fingerprint, "spec", "podTemplate", "metadata", "annotations", "speakeasy.com/kit-spec"); err != nil {
		return empty, fmt.Errorf("set restored pod fingerprint: %w", err)
	}
	template, err = k.config.GKE.Dynamic.Resource(kitTemplateGVR).Namespace(k.config.GKE.Namespace).Create(ctx, template, metav1.CreateOptions{})
	if err != nil {
		return empty, fmt.Errorf("create fresh restore template: %w", err)
	}
	if template.GetUID() == "" {
		return empty, fmt.Errorf("fresh restore template lacks UID")
	}
	state.Metadata.TemplateUID = template.GetUID()
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	claim := kitObject("SandboxClaim", "extensions.agents.x-k8s.io/v1alpha1", kitGKEName(r), k.identity(r))
	claimLabels := claim.GetLabels()
	claimLabels["gram.ai/project-id"] = r.ProjectID.String()
	claim.SetLabels(claimLabels)
	claim.SetAnnotations(map[string]string{"speakeasy.com/kit-spec": k.fingerprint})
	claim.Object["spec"] = map[string]any{"sandboxTemplateRef": map[string]any{"name": kitGKEName(r)}, "lifecycle": map[string]any{"shutdownPolicy": "Delete"}}
	claim, err = k.config.GKE.Dynamic.Resource(gkeSandboxClaimGVR).Namespace(k.config.GKE.Namespace).Create(ctx, claim, metav1.CreateOptions{})
	if err != nil {
		return empty, fmt.Errorf("kit GKE restore: %w", err)
	}
	if claim.GetUID() == "" {
		return empty, fmt.Errorf("kit GKE restored claim lacks UID")
	}
	state.Metadata.ClaimUID = claim.GetUID()
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	pending := state.Metadata
	pending.PodUID = ""
	pending.ContainerID = ""
	pending.PodName = ""
	readyCtx, cancel := context.WithTimeout(ctx, gkeRuntimeReadyTimeout)
	defer cancel()
	pod, err := k.waitPod(readyCtx, r, pending)
	if err != nil {
		return empty, err
	}
	state.Metadata.PodName = pod.GetName()
	state.Metadata.PodUID = pod.GetUID()
	_, state.Metadata.ContainerID, err = kitGKEProcessIdentity(pod)
	if err != nil {
		return empty, err
	}
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	if err = k.config.Snapshots.VerifyRestored(ctx, pod, snapshot, state.Source.binding()); err != nil {
		return empty, fmt.Errorf("kit GKE restore: %w", err)
	}
	if _, err = VerifyKitGKESnapshot(ctx, snapshot, state.Source.binding(), k.config.Snapshots); err != nil {
		return empty, err
	}
	if err = k.config.Snapshots.RefreshRestored(ctx, r, pod, b); err != nil {
		return empty, fmt.Errorf("kit GKE restore: %w", err)
	}
	pod, err = k.get(ctx, r, gkePodGVR, state.Metadata.PodName, state.Metadata.PodUID)
	if err != nil {
		return empty, err
	}
	digest, _, e := kitGKEProcessIdentity(pod)
	if e != nil {
		return empty, e
	}
	if digest != state.Metadata.ImageDigest {
		return empty, fmt.Errorf("cold restored image changed")
	}
	state.Metadata.AuthorityNotAfter = time.Now().Add(assistanttokens.MaxInferenceLifetime - time.Hour)
	_, state.Metadata.ContainerID, err = kitGKEProcessIdentity(pod)
	if err != nil {
		return empty, err
	}
	state.Metadata.PodName = pod.GetName()
	state.Metadata.PodUID = pod.GetUID()
	raw, err := json.Marshal(state.Metadata)
	if err != nil {
		return empty, fmt.Errorf("kit GKE restore metadata: %w", err)
	}
	r.BackendMetadataJSON = raw
	ip, _, _ := unstructured.NestedString(pod.Object, "status", "podIP")
	if net.ParseIP(ip) == nil {
		return empty, fmt.Errorf("restored Pod has no valid IP")
	}
	protocol, err := k.config.Protocol(readyCtx, r, &url.URL{Scheme: "https", Host: net.JoinHostPort(ip, strconv.Itoa(int(k.config.Provision.GuestPort))), Path: "/acp/v2"})
	if err != nil {
		return empty, err
	}
	if protocol == nil {
		return empty, fmt.Errorf("fresh restore protocol unavailable")
	}
	// Running precedes the entrypoint's marker check and gateway listener.
	// Retry initialization under the restore fence, never publish on factory success.
	for {
		status, statusErr := protocol.Status(readyCtx, r)
		if statusErr == nil && status.Configured {
			break
		}
		select {
		case <-readyCtx.Done():
			if statusErr != nil {
				return empty, fmt.Errorf("authenticate fresh restored gateway: %w", statusErr)
			}
			return empty, fmt.Errorf("fresh restored gateway is not configured: %w", readyCtx.Err())
		case <-time.After(gkeRuntimePollInterval):
		}
	}
	// Only authenticated startup proves the marker gate ran. Until this point
	// all turn admission remains fenced, including silent cold-start fallback.
	state.Phase = "active"
	if _, err = k.saveAuthority(ctx, obj, state); err != nil {
		return empty, err
	}
	return RuntimeBackendEnsureResult{ColdStart: false, BackendMetadataJSON: raw}, nil
}
