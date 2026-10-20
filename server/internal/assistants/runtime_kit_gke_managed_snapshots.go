package assistants

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
)

var kitGKEPolicyGVR = schema.GroupVersionResource{Group: "podsnapshot.gke.io", Version: "v1", Resource: "podsnapshotpolicies"}

// ErrKitGKEManagedSnapshotIncompatible is a storage/process incompatibility,
// not an absent credential or a request to inject an implementation.
var ErrKitGKEManagedSnapshotIncompatible = errors.New("managed GKE snapshot is incompatible with Kit fresh-process restore")
var ErrKitGKEManagedSnapshotSchema = errors.New("unsupported managed GKE snapshot evidence")

// KitGKEManagedCheckpointDriver reads the managed API without requiring ADC or
// guessing private controller fields. Source references:
// https://docs.cloud.google.com/kubernetes-engine/docs/reference/crds/podsnapshot
// https://docs.cloud.google.com/kubernetes-engine/docs/concepts/pod-snapshots#snapshot-matching
//
// Rootfs-only starts a new process. The guest's startup marker gate rejects
// silent cold fallback before loading fresh projected credentials or listening.
// Provider readiness is availability evidence, not proof of guest restoration.
type KitGKEManagedCheckpointDriver struct{ client dynamic.Interface }

var _ KitGKECheckpointDriver = (*KitGKEManagedCheckpointDriver)(nil)

func NewKitGKEManagedCheckpointDriver(client dynamic.Interface) (*KitGKEManagedCheckpointDriver, error) {
	if client == nil {
		return nil, errors.New("managed GKE snapshots require a Kubernetes client")
	}
	return &KitGKEManagedCheckpointDriver{client: client}, nil
}

func kitGKEObserved(obj *unstructured.Unstructured) error {
	observed, found, err := unstructured.NestedInt64(obj.Object, "status", "observedGeneration")
	if err != nil || !found || obj.GetGeneration() <= 0 || observed != obj.GetGeneration() || obj.GetDeletionTimestamp() != nil {
		return fmt.Errorf("%w: %s controller generation is absent, stale or deleting", ErrKitGKEManagedSnapshotSchema, obj.GetKind())
	}
	return nil
}

func kitGKEManagedScope(policy *unstructured.Unstructured) error {
	if policy.GetAPIVersion() != "podsnapshot.gke.io/v1" || policy.GetKind() != "PodSnapshotPolicy" || policy.GetDeletionTimestamp() != nil {
		return fmt.Errorf("%w: invalid PodSnapshotPolicy", ErrKitGKEManagedSnapshotSchema)
	}
	scope, found, err := unstructured.NestedString(policy.Object, "spec", "snapshotScope")
	if err != nil {
		return fmt.Errorf("%w: invalid snapshotScope", ErrKitGKEManagedSnapshotSchema)
	}
	if !found {
		scope = "whole-pod"
	} // Documented managed API default.
	switch scope {
	case "whole-pod":
		return fmt.Errorf("%w: policy %s uses whole-pod, which restores application memory and cannot prove a fresh process with renewed credentials", ErrKitGKEManagedSnapshotIncompatible, policy.GetName())
	case "rootfs-only":
		trigger, _, err := unstructured.NestedString(policy.Object, "spec", "triggerConfig", "type")
		if err != nil || trigger != "manual" || policy.GetUID() == "" || policy.GetGeneration() <= 0 {
			return fmt.Errorf("%w: rootfs-only requires an authoritative manual policy", ErrKitGKEManagedSnapshotSchema)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown snapshotScope %q", ErrKitGKEManagedSnapshotSchema, scope)
	}
}

func (d *KitGKEManagedCheckpointDriver) checkpointPolicy(ctx context.Context, pod *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if pod == nil || pod.GetUID() == "" || pod.GetNamespace() == "" {
		return nil, ErrKitGKEManagedSnapshotSchema
	}
	// Inspect the actual matching policy, not a hard-coded deployment name.
	policies, err := d.client.Resource(kitGKEPolicyGVR).Namespace(pod.GetNamespace()).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("read managed snapshot policies: %w", err)
	}
	var matched *unstructured.Unstructured
	for i := range policies.Items {
		p := &policies.Items[i]
		raw, found, err := unstructured.NestedMap(p.Object, "spec", "selector")
		if err != nil || !found {
			return nil, fmt.Errorf("%w: missing policy selector", ErrKitGKEManagedSnapshotSchema)
		}
		var selector metav1.LabelSelector
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &selector); err != nil {
			return nil, fmt.Errorf("%w: malformed policy selector", ErrKitGKEManagedSnapshotSchema)
		}
		s, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid policy selector", ErrKitGKEManagedSnapshotSchema)
		}
		if !s.Matches(labels.Set(pod.GetLabels())) {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("%w: multiple policies match the source Pod", ErrKitGKEManagedSnapshotSchema)
		}
		matched = p
	}
	if matched == nil {
		return nil, fmt.Errorf("%w: no policy matches the source Pod", ErrKitGKEManagedSnapshotSchema)
	}
	// The backend holds admission and has reconciled all gateway sessions and jobs.
	if err := kitGKEManagedScope(matched); err != nil {
		return nil, err
	}
	return matched, nil
}

// Select follows the documented trigger result, never a newest-by-label guess.
// A pending trigger is retryable by the lifecycle reconciler; its persisted UID
// and source binding remain authoritative across retries.
func (d *KitGKEManagedCheckpointDriver) Select(ctx context.Context, trigger *unstructured.Unstructured, binding KitGKESnapshotBinding) (*unstructured.Unstructured, error) {
	if err := binding.validate(); err != nil {
		return nil, err
	}
	if trigger == nil || trigger.GetUID() == "" || trigger.GetAPIVersion() != "podsnapshot.gke.io/v1" || trigger.GetKind() != "PodSnapshotManualTrigger" {
		return nil, ErrKitGKEManagedSnapshotSchema
	}
	if err := kitCheckIdentity(trigger, binding.Identity); err != nil {
		return nil, err
	}
	pod, err := d.client.Resource(gkePodGVR).Namespace(binding.Identity.Namespace).Get(ctx, binding.SourcePodName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("select managed snapshot: %w", err)
	}
	if _, err = RenderKitGKEManualTrigger(true, trigger.GetName(), binding, pod); err != nil {
		return nil, err
	}
	current, err := d.client.Resource(kitGKETriggerGVR).Namespace(binding.Identity.Namespace).Get(ctx, trigger.GetName(), metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("select managed snapshot: %w", err)
	}
	if current.GetUID() != trigger.GetUID() {
		return nil, fmt.Errorf("managed snapshot trigger UID changed")
	}
	if err = kitCheckIdentity(current, binding.Identity); err != nil {
		return nil, err
	}
	target, _, err := unstructured.NestedString(current.Object, "spec", "targetPod")
	if err != nil || target != binding.SourcePodName {
		return nil, fmt.Errorf("managed snapshot trigger target mismatch")
	}
	if err = kitGKEObserved(current); err != nil {
		return nil, err
	}
	name, found, err := unstructured.NestedString(current.Object, "status", "snapshotCreated")
	if err != nil || !found || len(validation.IsDNS1123Subdomain(name)) != 0 {
		return nil, fmt.Errorf("%w: trigger has no valid status.snapshotCreated", ErrKitGKEManagedSnapshotSchema)
	}
	selected, err := d.client.Resource(kitGKESnapshotGVR).Namespace(binding.Identity.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("select managed snapshot: %w", err)
	}
	// There is no documented sourcePodUID field on PodSnapshot. Recheck the
	// source around selection rather than inventing such a field. This alone
	// does not authorize deletion; VerifyAvailable must succeed separately.
	pod, err = d.client.Resource(gkePodGVR).Namespace(binding.Identity.Namespace).Get(ctx, binding.SourcePodName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("select managed snapshot: %w", err)
	}
	if _, err = RenderKitGKEManualTrigger(true, trigger.GetName(), binding, pod); err != nil {
		return nil, err
	}
	if selected.GetUID() == "" || selected.GetKind() != "PodSnapshot" || selected.GetAPIVersion() != "podsnapshot.gke.io/v1" {
		return nil, ErrKitGKEManagedSnapshotSchema
	}
	return selected, nil
}

func (d *KitGKEManagedCheckpointDriver) VerifyAvailable(ctx context.Context, selected *unstructured.Unstructured, binding KitGKESnapshotBinding) error {
	if err := binding.validate(); err != nil {
		return err
	}
	if selected == nil || selected.GetUID() == "" || selected.GetNamespace() != binding.Identity.Namespace {
		return ErrKitGKEManagedSnapshotSchema
	}
	current, err := d.client.Resource(kitGKESnapshotGVR).Namespace(binding.Identity.Namespace).Get(ctx, selected.GetName(), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("verify managed snapshot: %w", err)
	}
	if current.GetUID() != selected.GetUID() {
		return fmt.Errorf("managed snapshot UID changed")
	}
	if err = kitGKEObserved(current); err != nil {
		return err
	}
	name, found, err := unstructured.NestedString(current.Object, "spec", "policyName")
	if err != nil || !found || len(validation.IsDNS1123Subdomain(name)) != 0 {
		return fmt.Errorf("%w: missing snapshot policyName", ErrKitGKEManagedSnapshotSchema)
	}
	policy, err := d.client.Resource(kitGKEPolicyGVR).Namespace(binding.Identity.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("verify managed snapshot: %w", err)
	}
	if binding.PolicyName == "" || policy.GetName() != binding.PolicyName || policy.GetUID() != binding.PolicyUID || policy.GetGeneration() != binding.PolicyGeneration {
		return fmt.Errorf("managed snapshot policy authority changed")
	}
	if err := kitGKEManagedScope(policy); err != nil {
		return err
	}
	conditions, found, err := unstructured.NestedSlice(current.Object, "status", "conditions")
	if err != nil || !found || current.GetDeletionTimestamp() != nil {
		return ErrKitGKEManagedSnapshotSchema
	}
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if ok && condition["type"] == "Ready" && condition["status"] == "True" {
			return nil
		}
	}
	return fmt.Errorf("%w: snapshot is not Ready", ErrKitGKEManagedSnapshotSchema)
}

// VerifyRestored checks provider and Pod identity only. The backend must additionally
// authenticate the new gateway before publication: its entrypoint checks the exact
// checkpoint marker before listening, so Running/ps-name alone never opens admission.
func (d *KitGKEManagedCheckpointDriver) VerifyRestored(ctx context.Context, pod, selected *unstructured.Unstructured, binding KitGKESnapshotBinding) error {
	if err := kitCheckIdentity(pod, binding.Identity); err != nil {
		return err
	}
	if pod.GetUID() == "" || pod.GetUID() == binding.SourcePodUID || pod.GetDeletionTimestamp() != nil {
		return fmt.Errorf("managed restore requires a new Pod UID")
	}
	if selected == nil || pod.GetAnnotations()["podsnapshot.gke.io/ps-name"] != selected.GetName() {
		return fmt.Errorf("managed restore snapshot selection mismatch")
	}
	return d.VerifyAvailable(ctx, selected, binding)
}
