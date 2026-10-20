package assistants

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestKitGKEAdmissionPermissions(t *testing.T) {
	t.Parallel()
	for _, snapshots := range []bool{false, true} {
		t.Run(fmt.Sprintf("snapshots=%t", snapshots), func(t *testing.T) {
			t.Parallel()
			client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gkeSandboxClaimGVR: "SandboxClaimList"})
			k := &KitGKERuntimeBackend{config: KitGKERuntimeConfig{GKE: GKERuntimeConfig{Dynamic: client, Namespace: "assistants"}, Provision: KitGKEProvisionInput{SnapshotsEnabled: snapshots}}}
			seen := map[string][]string{}
			client.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
				require.Empty(t, action.GetNamespace(), "reviews are cluster scoped")
				create, ok := action.(ktesting.CreateAction)
				require.True(t, ok)
				object, ok := create.GetObject().(*unstructured.Unstructured)
				require.True(t, ok)
				review := object.DeepCopy()
				attributes, found, err := unstructured.NestedStringMap(review.Object, "spec", "resourceAttributes")
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, "assistants", attributes["namespace"])
				require.NotEmpty(t, attributes["version"])
				key := attributes["group"] + "/" + attributes["resource"]
				seen[key] = append(seen[key], attributes["verb"])
				review.Object["status"] = map[string]any{"allowed": true}
				return true, review, nil
			})
			require.NoError(t, k.RuntimeReadyForAdmission(t.Context()))
			expected := map[string][]string{
				"/secrets": {"create", "get", "delete"},
				kitTemplateGVR.Group + "/sandboxtemplates":  {"create", "get", "delete"},
				gkeSandboxClaimGVR.Group + "/sandboxclaims": {"create", "get", "delete", "list"},
				"/pods": {"get", "list", "delete"},
			}
			if snapshots {
				expected["podsnapshot.gke.io/podsnapshotmanualtriggers"] = []string{"create", "get", "delete"}
				expected["podsnapshot.gke.io/podsnapshots"] = []string{"get", "delete"}
				expected["podsnapshot.gke.io/podsnapshotpolicies"] = []string{"get", "list"}
			}
			require.Equal(t, expected, seen)
			for _, action := range client.Actions() {
				require.True(t, action.Matches("create", "selfsubjectaccessreviews") || action.Matches("list", "sandboxclaims"), "preflight must not provision resources")
			}
		})
	}
}

func TestKitGKEAdmissionPermissionsFailClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		status   map[string]any
		apiError bool
	}{
		{name: "denied", status: map[string]any{"allowed": false, "denied": true}},
		{name: "no opinion", status: map[string]any{"allowed": false}},
		{name: "missing status"},
		{name: "missing allowed", status: map[string]any{"denied": false}},
		{name: "malformed allowed", status: map[string]any{"allowed": "true"}},
		{name: "contradictory", status: map[string]any{"allowed": true, "denied": true}},
		{name: "malformed denied", status: map[string]any{"allowed": true, "denied": "false"}},
		{name: "evaluation error", status: map[string]any{"allowed": true, "evaluationError": "incomplete evaluation"}},
		{name: "malformed evaluation error", status: map[string]any{"allowed": true, "evaluationError": true}},
		{name: "API failure", apiError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			k := &KitGKERuntimeBackend{config: KitGKERuntimeConfig{GKE: GKERuntimeConfig{Dynamic: client, Namespace: "assistants"}}}
			client.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
				if tc.apiError {
					return true, nil, fmt.Errorf("authorization API unavailable")
				}
				create, ok := action.(ktesting.CreateAction)
				require.True(t, ok)
				object, ok := create.GetObject().(*unstructured.Unstructured)
				require.True(t, ok)
				review := object.DeepCopy()
				if tc.status != nil {
					review.Object["status"] = tc.status
				}
				return true, review, nil
			})
			require.ErrorIs(t, k.RuntimeReadyForAdmission(t.Context()), errRuntimeSwitchUnavailable)
			require.Len(t, client.Actions(), 1)
		})
	}
}

func TestKitGKEPermissionAllowedInvalidResponse(t *testing.T) {
	t.Parallel()
	require.False(t, kitGKEPermissionAllowed(nil))
	require.False(t, kitGKEPermissionAllowed(&unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"allowed": true}}}))
	k := &KitGKERuntimeBackend{}
	require.ErrorIs(t, k.RuntimeReadyForAdmission(t.Context()), errRuntimeSwitchUnavailable)
}
