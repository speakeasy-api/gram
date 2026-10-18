package assistants

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
)

var kitManualTriggerGVR = schema.GroupVersionResource{Group: "podsnapshot.gke.io", Version: "v1", Resource: "podsnapshotmanualtriggers"}

// KitGKESnapshotBinding is read from authoritative control-plane state. Labels
// are checked in addition to, never in place of, this source-pod UID binding.
type KitGKESnapshotBinding struct {
	Identity      KitGKEIdentity
	SourcePodName string
	SourcePodUID  types.UID
}

func (b KitGKESnapshotBinding) validate() error {
	if err := b.Identity.validate(); err != nil {
		return err
	}
	if b.SourcePodUID == "" || len(validation.IsDNS1123Subdomain(b.SourcePodName)) != 0 {
		return fmt.Errorf("Kit snapshot requires an exact source pod name and UID")
	}
	return nil
}
func kitCheckIdentity(obj *unstructured.Unstructured, identity KitGKEIdentity) error {
	if obj == nil || obj.GetNamespace() != identity.Namespace {
		return fmt.Errorf("Kit object namespace mismatch")
	}
	for key, value := range identity.labels() {
		if obj.GetLabels()[key] != value {
			return fmt.Errorf("Kit object identity mismatch: %s", key)
		}
	}
	return nil
}

// RenderKitGKEManualTrigger does not create the trigger or mark a snapshot
// complete. The caller must hold admission, settle/fence effects, and recheck
// source UID throughout trigger execution: targetPod has no UID precondition.
func RenderKitGKEManualTrigger(enabled bool, name string, binding KitGKESnapshotBinding, pod *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if !enabled {
		return nil, fmt.Errorf("Kit snapshots are disabled")
	}
	if err := binding.validate(); err != nil {
		return nil, err
	}
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		return nil, fmt.Errorf("invalid Kit snapshot trigger name")
	}
	if err := kitCheckIdentity(pod, binding.Identity); err != nil {
		return nil, err
	}
	if pod.GetAPIVersion() != "v1" || pod.GetKind() != "Pod" || pod.GetName() != binding.SourcePodName || pod.GetUID() != binding.SourcePodUID || pod.GetLabels()["gram.ai/snapshots"] != "enabled" {
		return nil, fmt.Errorf("Kit snapshot source pod does not match authoritative binding")
	}
	trigger := kitObject("PodSnapshotManualTrigger", "podsnapshot.gke.io/v1", name, binding.Identity)
	trigger.Object["spec"] = map[string]any{"targetPod": binding.SourcePodName}
	return trigger, nil
}

// KitGKESnapshotAvailabilityVerifier must use the deployed CRD's actual status
// contract to verify trigger success, selected snapshot AllSnapshotsAvailable,
// unexpired backing data, and source pod name/UID correlation. No implementation
// is supplied until that contract is verified in the dev canary. A successful
// trigger create or Pod Running alone MUST NOT satisfy this interface.
type KitGKESnapshotAvailabilityVerifier interface {
	VerifyAvailable(context.Context, *unstructured.Unstructured, KitGKESnapshotBinding) error
}

// KitGKEVerifiedSnapshot is only produced after authoritative verification.
// It is short-lived evidence, not a replacement for persisted fencing/authority.
type KitGKEVerifiedSnapshot struct {
	name    string
	binding KitGKESnapshotBinding
}

func (s KitGKEVerifiedSnapshot) Name() string { return s.name }

func VerifyKitGKESnapshot(ctx context.Context, selected *unstructured.Unstructured, binding KitGKESnapshotBinding, verifier KitGKESnapshotAvailabilityVerifier) (*KitGKEVerifiedSnapshot, error) {
	if err := binding.validate(); err != nil {
		return nil, err
	}
	if err := kitCheckIdentity(selected, binding.Identity); err != nil {
		return nil, err
	}
	if selected.GetAPIVersion() != "podsnapshot.gke.io/v1" || selected.GetKind() != "PodSnapshot" || len(validation.IsDNS1123Subdomain(selected.GetName())) != 0 {
		return nil, fmt.Errorf("invalid selected Kit snapshot")
	}
	if verifier == nil {
		return nil, fmt.Errorf("authoritative Kit snapshot availability verification is required")
	}
	if err := verifier.VerifyAvailable(ctx, selected.DeepCopy(), binding); err != nil {
		return nil, fmt.Errorf("verify Kit snapshot availability: %w", err)
	}
	return &KitGKEVerifiedSnapshot{name: selected.GetName(), binding: binding}, nil
}
