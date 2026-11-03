// Package sandbox shares GKE Agent Sandbox discovery between runtime purposes.
package sandbox

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ClaimUIDLabel binds a pod to the claim that adopted it from a warm pool.
const ClaimUIDLabel = "agents.x-k8s.io/claim-uid"

var (
	// Claims selects the v1alpha1 claim API used by assistant runtimes.
	Claims = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1alpha1", Resource: "sandboxclaims"}
	// Templates selects the v1alpha1 template API used by assistant runtimes.
	Templates = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1alpha1", Resource: "sandboxtemplates"}
	// Sandboxes selects the v1alpha1 sandbox API used by assistant runtimes.
	Sandboxes = schema.GroupVersionResource{Group: "agents.x-k8s.io", Version: "v1alpha1", Resource: "sandboxes"}
	// ClaimsV1Beta1 selects native pool-based claims for Code Mode.
	ClaimsV1Beta1 = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1beta1", Resource: "sandboxclaims"}

	// TemplatesV1Beta1 selects the templates referenced by native warm pools.
	TemplatesV1Beta1 = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1beta1", Resource: "sandboxtemplates"}

	// WarmPoolsV1Beta1 selects the pools referenced by native claims.
	WarmPoolsV1Beta1 = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1beta1", Resource: "sandboxwarmpools"}

	// SandboxesV1Beta1 selects sandboxes assigned to native claims.
	SandboxesV1Beta1 = schema.GroupVersionResource{Group: "agents.x-k8s.io", Version: "v1beta1", Resource: "sandboxes"}

	// Pods selects Kubernetes pods for claim ownership and image checks.
	Pods = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
)

// AssignedName accepts the upstream and legacy managed-GKE status spellings.
func AssignedName(claim *unstructured.Unstructured) string {
	name, _, _ := unstructured.NestedString(claim.Object, "status", "sandbox", "name")
	if name == "" {
		name, _, _ = unstructured.NestedString(claim.Object, "status", "sandbox", "Name")
	}
	return name
}

// Ready reports the controller's Ready condition, excluding terminating resources.
func Ready(resource *unstructured.Unstructured) bool {
	if resource.GetDeletionTimestamp() != nil {
		return false
	}
	conditions, _, _ := unstructured.NestedSlice(resource.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if condition["type"] == "Ready" {
			return condition["status"] == string(metav1.ConditionTrue)
		}
	}
	return false
}

// RunningPods lists live, addressed pods belonging to a claim. Callers decide
// whether multiple pods or a particular image are acceptable for their purpose.
func RunningPods(ctx context.Context, client dynamic.Interface, namespace, claimUID string) ([]unstructured.Unstructured, error) {
	if claimUID == "" {
		return nil, fmt.Errorf("sandbox claim UID is required")
	}
	pods, err := client.Resource(Pods).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: ClaimUIDLabel + "=" + claimUID})
	if err != nil {
		return nil, fmt.Errorf("list claimed sandbox pods: %w", err)
	}
	result := make([]unstructured.Unstructured, 0, len(pods.Items))
	for _, pod := range pods.Items {
		phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
		ip, _, _ := unstructured.NestedString(pod.Object, "status", "podIP")
		if pod.GetDeletionTimestamp() == nil && pod.GetLabels()[ClaimUIDLabel] == claimUID && phase == "Running" && ip != "" {
			result = append(result, pod)
		}
	}
	return result, nil
}

// RunningPodsForClaim lists the live, addressed pods that PodsForClaim
// attributes to a claim.
func RunningPodsForClaim(ctx context.Context, client dynamic.Interface, namespace string, claim *unstructured.Unstructured, sandboxes schema.GroupVersionResource) ([]unstructured.Unstructured, error) {
	pods, err := PodsForClaim(ctx, client, namespace, claim, sandboxes)
	if err != nil {
		return nil, err
	}
	result := make([]unstructured.Unstructured, 0, len(pods))
	for _, pod := range pods {
		phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
		ip, _, _ := unstructured.NestedString(pod.Object, "status", "podIP")
		if phase == "Running" && ip != "" {
			result = append(result, pod)
		}
	}
	return result, nil
}

// PodsForClaim lists the non-terminating pods belonging to a claim in any
// phase. It supports both managed controller layouts. Older controllers label
// Pods with the claim UID; newer controllers label the Sandbox and expose its
// pod selector instead. A selector alone is not proof of ownership: the
// fallback verifies both controller owner references.
func PodsForClaim(ctx context.Context, client dynamic.Interface, namespace string, claim *unstructured.Unstructured, sandboxes schema.GroupVersionResource) ([]unstructured.Unstructured, error) {
	if claim == nil || claim.GetUID() == "" || claim.GetName() == "" || claim.GetNamespace() != namespace || claim.GetDeletionTimestamp() != nil {
		return nil, fmt.Errorf("live sandbox claim identity is required")
	}
	claimUID := string(claim.GetUID())
	labelled, err := client.Resource(Pods).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: ClaimUIDLabel + "=" + claimUID})
	if err != nil {
		return nil, fmt.Errorf("list claimed sandbox pods: %w", err)
	}
	result := make([]unstructured.Unstructured, 0, len(labelled.Items))
	for _, pod := range labelled.Items {
		if pod.GetDeletionTimestamp() == nil && pod.GetLabels()[ClaimUIDLabel] == claimUID {
			result = append(result, pod)
		}
	}
	if len(result) != 0 {
		return result, nil
	}
	name := AssignedName(claim)
	if name == "" {
		return nil, nil
	}
	resource, err := client.Resource(sandboxes).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get assigned sandbox: %w", err)
	}
	if resource.GetDeletionTimestamp() != nil {
		return nil, nil
	}
	rawSelector, _, _ := unstructured.NestedString(resource.Object, "status", "selector")
	// Without a selector, preserve the prior missing-pod behavior: the
	// caller must wait or retire its missing runner.
	if rawSelector == "" {
		return nil, nil
	}
	selector, err := labels.Parse(rawSelector)
	if err != nil || selector.Empty() {
		return nil, fmt.Errorf("assigned sandbox has no valid pod selector")
	}
	if resource.GetUID() == "" || !controlledBy(resource, Claims.Group, "SandboxClaim", claim) {
		return nil, fmt.Errorf("assigned sandbox claim ownership mismatch")
	}
	if uid := resource.GetLabels()[ClaimUIDLabel]; uid != "" && uid != claimUID {
		return nil, fmt.Errorf("assigned sandbox claim label mismatch")
	}
	candidates, err := client.Resource(Pods).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, fmt.Errorf("list assigned sandbox pods: %w", err)
	}
	for _, pod := range candidates.Items {
		if !controlledBy(&pod, Sandboxes.Group, "Sandbox", resource) {
			continue
		}
		if uid := pod.GetLabels()[ClaimUIDLabel]; uid != "" && uid != claimUID {
			continue
		}
		if pod.GetUID() != "" && pod.GetDeletionTimestamp() == nil {
			result = append(result, pod)
		}
	}
	return result, nil
}

func controlledBy(child *unstructured.Unstructured, group, kind string, parent *unstructured.Unstructured) bool {
	owner := metav1.GetControllerOf(child)
	if owner == nil || owner.Kind != kind || owner.Name != parent.GetName() || owner.UID != parent.GetUID() || child.GetNamespace() != parent.GetNamespace() {
		return false
	}
	version, err := schema.ParseGroupVersion(owner.APIVersion)
	return err == nil && version.Group == group
}
