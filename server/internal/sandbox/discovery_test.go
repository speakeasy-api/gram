package sandbox_test

import (
	"fmt"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/sandbox"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestAssignedNameAcceptsBothControllerSpellings(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"name", "Name"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			claim := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"sandbox": map[string]any{key: "assigned"}}}}
			require.Equal(t, "assigned", sandbox.AssignedName(claim))
		})
	}
	require.Empty(t, sandbox.AssignedName(&unstructured.Unstructured{}))
}

func TestReadyExcludesTerminatingResources(t *testing.T) {
	t.Parallel()
	resource := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}}
	require.True(t, sandbox.Ready(resource))
	now := metav1.Now()
	resource.SetDeletionTimestamp(&now)
	require.False(t, sandbox.Ready(resource))
	resource.SetDeletionTimestamp(nil)
	require.NoError(t, unstructured.SetNestedSlice(resource.Object, []any{map[string]any{"type": "Ready", "status": "False"}}, "status", "conditions"))
	require.False(t, sandbox.Ready(resource))
}

func TestRunningPodsReturnsOnlyLiveAddressedClaimMembers(t *testing.T) {
	t.Parallel()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{sandbox.Pods: "PodList"})
	for _, tc := range []struct {
		name, phase, ip, claim, namespace string
		terminating                       bool
	}{
		{"live", "Running", "10.0.0.1", "claim", "code", false},
		{"terminating", "Running", "10.0.0.2", "claim", "code", true},
		{"pending", "Pending", "10.0.0.3", "claim", "code", false},
		{"unaddressed", "Running", "", "claim", "code", false},
		{"other-claim", "Running", "10.0.0.4", "other", "code", false},
		{"other-namespace", "Running", "10.0.0.5", "claim", "other", false},
	} {
		pod := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": tc.name, "namespace": tc.namespace, "labels": map[string]any{sandbox.ClaimUIDLabel: tc.claim}}, "status": map[string]any{"phase": tc.phase, "podIP": tc.ip}}}
		if tc.terminating {
			now := metav1.Now()
			pod.SetDeletionTimestamp(&now)
		}
		_, err := client.Resource(sandbox.Pods).Namespace(tc.namespace).Create(t.Context(), pod, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	pods, err := sandbox.RunningPods(t.Context(), client, "code", "claim")
	require.NoError(t, err)
	require.Len(t, pods, 1)
	require.Equal(t, "live", pods[0].GetName())
}

func TestRunningPodsRefusesMissingClaimAndPropagatesLoadErrors(t *testing.T) {
	t.Parallel()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{sandbox.Pods: "PodList"})
	called := false
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		called = true
		return true, nil, fmt.Errorf("cluster unavailable")
	})
	_, err := sandbox.RunningPods(t.Context(), client, "code", "")
	require.ErrorContains(t, err, "claim UID is required")
	require.False(t, called)
	_, err = sandbox.RunningPods(t.Context(), client, "code", "claim")
	require.ErrorContains(t, err, "cluster unavailable")
	require.True(t, called)
}
