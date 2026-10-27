package assistants

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var kitGKEAccessReviewGVR = schema.GroupVersionResource{Group: "authorization.k8s.io", Version: "v1", Resource: "selfsubjectaccessreviews"}

// preflightPermissions checks the current caller, not controller health. These
// non-persistent reviews neither create guests nor mint credentials. Permissions
// can change after the check; normal API operations must still enforce RBAC.
// The stable API requires allowed=true; denied is optional and cannot also be true:
// https://kubernetes.io/docs/reference/kubernetes-api/authorization-resources/self-subject-access-review-v1/
func (k *KitGKERuntimeBackend) preflightPermissions(ctx context.Context) error {
	if k.config.GKE.Dynamic == nil || k.config.GKE.Namespace == "" {
		return fmt.Errorf("%w: Kit GKE admission requires a client and namespace", errRuntimeSwitchUnavailable)
	}
	permissions := []struct {
		resource schema.GroupVersionResource
		verbs    []string
	}{
		{kitGKEIAMPendingGVR, []string{"get", "create", "update", "delete"}},
		{kitServiceAccountGVR, []string{"create", "get", "delete"}},
		{kitSecretGVR, []string{"create", "get", "delete"}},
		{kitTemplateGVR, []string{"create", "get", "delete"}},
		{kitWarmPoolGVR, []string{"create", "get", "delete"}},
		{kitClaimGVR, []string{"create", "get", "delete", "list"}},
		{gkePodGVR, []string{"get", "list", "delete"}},
	}
	if k.config.Provision.SnapshotsEnabled {
		permissions = append(permissions, []struct {
			resource schema.GroupVersionResource
			verbs    []string
		}{
			{kitGKETriggerGVR, []string{"create", "get", "delete"}},
			{kitGKESnapshotGVR, []string{"get", "delete"}},
			{kitGKEPolicyGVR, []string{"get", "list"}},
		}...)
	}
	for _, permission := range permissions {
		for _, verb := range permission.verbs {
			review := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview",
				"spec": map[string]any{"resourceAttributes": map[string]any{
					"namespace": k.config.GKE.Namespace, "verb": verb,
					"group": permission.resource.Group, "version": permission.resource.Version,
					"resource": permission.resource.Resource,
				}},
			}}
			result, err := k.config.GKE.Dynamic.Resource(kitGKEAccessReviewGVR).Create(ctx, review, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("%w: review Kit GKE permission %s %s: %w", errRuntimeSwitchUnavailable, verb, permission.resource.Resource, err)
			}
			if !kitGKEPermissionAllowed(result) {
				return fmt.Errorf("%w: Kit GKE permission %s %s is not conclusively allowed", errRuntimeSwitchUnavailable, verb, permission.resource.Resource)
			}
		}
	}
	return nil
}

func kitGKEPermissionAllowed(review *unstructured.Unstructured) bool {
	if review == nil || review.GetAPIVersion() != "authorization.k8s.io/v1" || review.GetKind() != "SelfSubjectAccessReview" {
		return false
	}
	allowed, found, err := unstructured.NestedBool(review.Object, "status", "allowed")
	if err != nil || !found || !allowed {
		return false
	}
	denied, _, err := unstructured.NestedBool(review.Object, "status", "denied")
	if err != nil || denied {
		return false
	}
	evaluationError, _, err := unstructured.NestedString(review.Object, "status", "evaluationError")
	return err == nil && evaluationError == ""
}
