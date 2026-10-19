package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var kitGKEAuthorityGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
var kitGKETriggerGVR = schema.GroupVersionResource{Group: "podsnapshot.gke.io", Version: "v1", Resource: "podsnapshotmanualtriggers"}
var kitGKESnapshotGVR = schema.GroupVersionResource{Group: "podsnapshot.gke.io", Version: "v1", Resource: "podsnapshots"}

// KitGKECheckpointDriver is the deployment-verified part of the GKE contract.
// Public docs specify targetPod, AllSnapshotsAvailable and ps-name, but omit
// machine-readable trigger/source UID/expiry status and restore-vs-cold evidence:
// https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots-trigger
// https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots
// Select must wait for this exact trigger's completion and correlate source UID.
// VerifyAvailable must recheck expiry/backing data, not merely cached status.
// PrepareColdRestore must arrange filesystem restoration WITHOUT restoring
// process memory, while keeping the restored guest externally fenced. GKE
// whole-pod restore alone is not sufficient. All persisted session files and
// tmpfs HOME data must survive; rootfs-only that loses HOME is not sufficient.
// VerifyRestored must prove the exact filesystem snapshot was restored without
// process memory and reject GKE's documented silent cold-start fallback.
// RefreshRestored must cold-start a NEW process after replacing ALL fenced
// inference/MCP/gateway credentials, proving no old process, background job,
// cached token, MCP transport or gateway authentication survives. It must not
// delete session files. File overwrite or an init container alone is NOT success.
// No guessed status paths or unauthenticated exec implementation is provided.
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
	TriggerName  string
	TriggerUID   types.UID
	SnapshotName string
	SnapshotUID  types.UID
}

type kitGKEAuthority struct {
	PriorCheckpoints []kitGKECheckpointRef  `json:"prior_checkpoints,omitempty"`
	Phase            string                 `json:"phase"`
	Metadata         kitGKEBackendMetadata  `json:"metadata"`
	TriggerName      string                 `json:"trigger_name,omitempty"`
	TriggerUID       types.UID              `json:"trigger_uid,omitempty"`
	SnapshotName     string                 `json:"snapshot_name,omitempty"`
	SnapshotUID      types.UID              `json:"snapshot_uid,omitempty"`
	Source           *KitGKESnapshotBinding `json:"source,omitempty"`
}

func (k *KitGKERuntimeBackend) createAuthority(ctx context.Context, r assistantRuntimeRecord, m kitGKEBackendMetadata) (kitGKEBackendMetadata, error) {
	obj := kitObject("ConfigMap", "v1", kitGKEName(r)+"-authority", k.identity(r))
	labels := obj.GetLabels()
	labels["gram.ai/project-id"] = r.ProjectID.String()
	obj.SetLabels(labels)
	obj.SetAnnotations(map[string]string{"speakeasy.com/kit-spec": k.fingerprint})
	raw, err := json.Marshal(kitGKEAuthority{Phase: "active", Metadata: m})
	if err != nil {
		return m, err
	}
	obj.Object["data"] = map[string]any{"authority.json": string(raw)}
	obj, err = k.config.GKE.Dynamic.Resource(kitGKEAuthorityGVR).Namespace(k.config.GKE.Namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return m, err
	}
	if obj.GetUID() == "" {
		return m, fmt.Errorf("Kit GKE authority has no UID")
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
		return state, nil, fmt.Errorf("Kit GKE runtime has no persisted authority UID")
	}
	obj, err := k.get(ctx, r, kitGKEAuthorityGVR, kitGKEName(r)+"-authority", m.AuthorityUID)
	if err != nil {
		return state, nil, err
	}
	raw, found, err := unstructured.NestedString(obj.Object, "data", "authority.json")
	if err != nil || !found {
		return state, nil, fmt.Errorf("Kit GKE checkpoint authority is malformed")
	}
	if err = json.Unmarshal([]byte(raw), &state); err != nil {
		return state, nil, err
	}
	state.Metadata.AuthorityUID = m.AuthorityUID
	copy := r
	copy.BackendMetadataJSON, _ = json.Marshal(state.Metadata)
	if _, err = k.metadata(copy); err != nil {
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
		return nil, fmt.Errorf("Kit GKE authority update requires UID and resourceVersion")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	next := obj.DeepCopy()
	next.Object["data"] = map[string]any{"authority.json": string(raw)}
	updated, err := k.config.GKE.Dynamic.Resource(kitGKEAuthorityGVR).Namespace(k.config.GKE.Namespace).Update(ctx, next, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	if updated.GetUID() != obj.GetUID() {
		return nil, fmt.Errorf("Kit GKE checkpoint authority was replaced")
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
		readiness, err := coordinator.PrepareRuntimeSwitch(ctx, assistantRecord{ID: r.AssistantID, ProjectID: r.ProjectID}, []assistantRuntimeRecord{r}, 3)
		if readiness.Release != nil {
			defer readiness.Release()
		}
		if err != nil {
			return err
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
			return e
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
		state.Source = &KitGKESnapshotBinding{Identity: k.identity(r), SourcePodName: state.Metadata.PodName, SourcePodUID: state.Metadata.PodUID}
		obj, err = k.saveAuthority(ctx, obj, state)
		if err != nil {
			return err
		}
	}
	if state.Phase == "checkpointing" {
		if state.Source == nil || state.TriggerName == "" {
			return fmt.Errorf("Kit GKE checkpoint source authority missing")
		}
		if err = k.config.Fence(ctx, r); err != nil {
			return err
		}
		pod, err := k.get(ctx, r, gkePodGVR, state.Source.SourcePodName, state.Source.SourcePodUID)
		if err != nil {
			return err
		}
		trigger, err := RenderKitGKEManualTrigger(true, state.TriggerName, *state.Source, pod)
		if err != nil {
			return err
		}
		resource := k.config.GKE.Dynamic.Resource(kitGKETriggerGVR).Namespace(k.config.GKE.Namespace)
		if state.TriggerUID == "" {
			// A crash after Create before CAS deliberately requires operator repair;
			// an unrecorded trigger may not be adopted on labels alone.
			trigger, err = resource.Create(ctx, trigger, metav1.CreateOptions{})
			if err != nil {
				return err
			}
			state.TriggerUID = trigger.GetUID()
			if state.TriggerUID == "" {
				return fmt.Errorf("Kit GKE checkpoint trigger has no UID")
			}
			obj, err = k.saveAuthority(ctx, obj, state)
			if err != nil {
				return err
			}
		} else {
			trigger, err = resource.Get(ctx, state.TriggerName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if trigger.GetUID() != state.TriggerUID {
				return fmt.Errorf("Kit GKE trigger authority mismatch")
			}
		}
		selected, err := k.config.Snapshots.Select(ctx, trigger, *state.Source)
		if err != nil {
			return err
		}
		verified, err := VerifyKitGKESnapshot(ctx, selected, *state.Source, k.config.Snapshots)
		if err != nil {
			return err
		}
		if _, err = k.get(ctx, r, gkePodGVR, state.Source.SourcePodName, state.Source.SourcePodUID); err != nil {
			return err
		}
		if selected.GetUID() == "" {
			return fmt.Errorf("Kit GKE selected snapshot has no UID")
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
		return fmt.Errorf("Kit GKE checkpoint evidence missing")
	}
	selected, e := k.config.GKE.Dynamic.Resource(kitGKESnapshotGVR).Namespace(k.config.GKE.Namespace).Get(ctx, state.SnapshotName, metav1.GetOptions{})
	if e != nil {
		return e
	}
	if selected.GetUID() != state.SnapshotUID {
		return fmt.Errorf("Kit GKE checkpoint snapshot replaced")
	}
	if _, e = VerifyKitGKESnapshot(ctx, selected, *state.Source, k.config.Snapshots); e != nil {
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

func (k *KitGKERuntimeBackend) restore(ctx context.Context, r assistantRuntimeRecord, state kitGKEAuthority, obj *unstructured.Unstructured) (RuntimeBackendEnsureResult, error) {
	empty := RuntimeBackendEnsureResult{}
	if k.config.Snapshots == nil {
		return empty, fmt.Errorf("%s", kitGKECheckpointUnavailable)
	}
	if state.Phase != "stopped" {
		return empty, fmt.Errorf("Kit GKE phase %s is not safely restorable; finish checkpoint or reconcile fenced attempt", state.Phase)
	}
	if state.Source == nil || state.SnapshotUID == "" || state.SnapshotName == "" {
		return empty, fmt.Errorf("Kit GKE restore authority missing")
	}
	snapshot, err := k.config.GKE.Dynamic.Resource(kitGKESnapshotGVR).Namespace(k.config.GKE.Namespace).Get(ctx, state.SnapshotName, metav1.GetOptions{})
	if err != nil {
		return empty, err
	}
	if snapshot.GetUID() != state.SnapshotUID {
		return empty, fmt.Errorf("Kit GKE restore snapshot UID mismatch")
	}
	verified, err := VerifyKitGKESnapshot(ctx, snapshot, *state.Source, k.config.Snapshots)
	if err != nil {
		return empty, err
	}
	if _, err = k.config.GKE.Dynamic.Resource(gkeSandboxClaimGVR).Namespace(k.config.GKE.Namespace).Get(ctx, kitGKEName(r), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		return empty, fmt.Errorf("Kit GKE restore requires source claim deletion: %v", err)
	}
	template, err := k.get(ctx, r, kitTemplateGVR, kitGKEName(r), state.Metadata.TemplateUID)
	if err != nil {
		return empty, err
	}
	template, err = k.config.Snapshots.PrepareColdRestore(ctx, r, template.DeepCopy(), snapshot, *state.Source)
	if err != nil {
		return empty, err
	}
	if template == nil || template.GetUID() != state.Metadata.TemplateUID || template.GetName() != kitGKEName(r) {
		return empty, fmt.Errorf("cold restore driver changed template authority")
	}
	state.Phase = "restoring"
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	// Pin the selection prepared by the cold-filesystem driver; never accept
	// automatic snapshot selection or resume process memory with cached tokens.
	if err = unstructured.SetNestedField(template.Object, verified.Name(), "spec", "podTemplate", "metadata", "annotations", "podsnapshot.gke.io/ps-name"); err != nil {
		return empty, err
	}
	_, err = k.config.GKE.Dynamic.Resource(kitTemplateGVR).Namespace(k.config.GKE.Namespace).Update(ctx, template, metav1.UpdateOptions{})
	if err != nil {
		return empty, err
	}
	claim := kitObject("SandboxClaim", "extensions.agents.x-k8s.io/v1alpha1", kitGKEName(r), k.identity(r))
	labels := claim.GetLabels()
	labels["gram.ai/project-id"] = r.ProjectID.String()
	claim.SetLabels(labels)
	claim.SetAnnotations(map[string]string{"speakeasy.com/kit-spec": k.fingerprint})
	claim.Object["spec"] = map[string]any{"sandboxTemplateRef": map[string]any{"name": kitGKEName(r)}, "lifecycle": map[string]any{"shutdownPolicy": "Delete"}}
	claim, err = k.config.GKE.Dynamic.Resource(gkeSandboxClaimGVR).Namespace(k.config.GKE.Namespace).Create(ctx, claim, metav1.CreateOptions{})
	if err != nil {
		return empty, err
	}
	if claim.GetUID() == "" {
		return empty, fmt.Errorf("Kit GKE restored claim lacks UID")
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
	if err = k.config.Snapshots.VerifyRestored(ctx, pod, snapshot, *state.Source); err != nil {
		return empty, err
	}
	if _, err = VerifyKitGKESnapshot(ctx, snapshot, *state.Source, k.config.Snapshots); err != nil {
		return empty, err
	}
	b, err := k.config.Bootstrap(ctx, r)
	if err != nil {
		return empty, err
	}
	if err = validateKitBootstrap(b); err != nil {
		return empty, err
	}
	if err = k.config.Snapshots.RefreshRestored(ctx, r, pod, b); err != nil {
		return empty, err
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
	state.Phase = "active"
	_, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	raw, err := json.Marshal(state.Metadata)
	if err != nil {
		return empty, err
	}
	r.BackendMetadataJSON = raw
	if _, err = k.protocol(readyCtx, r); err != nil {
		return empty, err
	}
	return RuntimeBackendEnsureResult{ColdStart: false, BackendMetadataJSON: raw}, nil
}
