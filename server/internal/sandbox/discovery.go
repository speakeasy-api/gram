// Package sandbox shares GKE Agent Sandbox discovery between runtime purposes.
package sandbox

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ClaimUIDLabel binds a pod to the claim that adopted it from a warm pool.
const ClaimUIDLabel = "agents.x-k8s.io/claim-uid"

var (
	// Claims selects the managed GKE Agent Sandbox claim API.
	Claims = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1alpha1", Resource: "sandboxclaims"}
	// Sandboxes selects the managed GKE sandbox API.
	Sandboxes = schema.GroupVersionResource{Group: "agents.x-k8s.io", Version: "v1alpha1", Resource: "sandboxes"}
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
