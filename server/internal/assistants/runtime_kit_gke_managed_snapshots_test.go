package assistants

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func kitManagedPolicy(identity KitGKEIdentity, scope string) *unstructured.Unstructured {
	p := kitObject("PodSnapshotPolicy", "podsnapshot.gke.io/v1", "kit-manual", identity)
	p.SetUID("policy-uid")
	p.SetGeneration(1)
	p.Object["spec"] = map[string]any{"triggerConfig": map[string]any{"type": "manual"}, "selector": map[string]any{"matchLabels": map[string]any{"gram.ai/runtime-version": "3"}}}
	if scope != "" {
		_ = unstructured.SetNestedField(p.Object, scope, "spec", "snapshotScope")
	}
	return p
}

func TestKitGKEManagedPolicyScopesFailBeforeCheckpoint(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		scope, message string
		schema         bool
	}{
		{"", "restores application memory", false},
		{"whole-pod", "restores application memory", false},

		{"future-scope", "unknown snapshotScope", true},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			identity := kitGKEInput().Identity
			policy := kitManagedPolicy(identity, tc.scope)
			client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{kitGKEPolicyGVR: "PodSnapshotPolicyList"}, policy)
			driver, err := NewKitGKEManagedCheckpointDriver(client)
			require.NoError(t, err)
			pod := kitObject("Pod", "v1", "source", identity)
			pod.SetUID("source-uid")
			_, err = driver.checkpointPolicy(t.Context(), pod)
			if tc.schema {
				require.ErrorIs(t, err, ErrKitGKEManagedSnapshotSchema)
			} else {
				require.ErrorIs(t, err, ErrKitGKEManagedSnapshotIncompatible)
			}
			require.ErrorContains(t, err, tc.message)
			for _, a := range client.Actions() {
				require.Equal(t, "list", a.GetVerb())
			}
		})
	}
}

//nolint:paralleltest // Subtests deliberately mutate the same fake API resource sequentially.
func TestKitGKEManagedSelectUsesDocumentedTriggerResult(t *testing.T) {
	identity := kitGKEInput().Identity
	binding := KitGKESnapshotBinding{PolicyName: "kit-manual", PolicyUID: "policy-uid", PolicyGeneration: 1, Identity: identity, SourcePodName: "source", SourcePodUID: "source-uid"}
	pod := kitObject("Pod", "v1", "source", identity)
	pod.SetUID(binding.SourcePodUID)
	labels := pod.GetLabels()
	labels["gram.ai/snapshots"] = "enabled"
	pod.SetLabels(labels)
	trigger, err := RenderKitGKEManualTrigger(true, "manual-trigger", binding, pod)
	require.NoError(t, err)
	trigger.SetUID("trigger-uid")
	trigger.SetGeneration(1)
	trigger.Object["status"] = map[string]any{"observedGeneration": int64(1), "snapshotCreated": "selected-snapshot"}
	snapshot := kitObject("PodSnapshot", "podsnapshot.gke.io/v1", "selected-snapshot", identity)
	snapshot.SetUID("snapshot-uid")
	snapshot.SetGeneration(1)
	snapshot.Object["spec"] = map[string]any{"policyName": "kit-manual"}
	snapshot.Object["status"] = map[string]any{"observedGeneration": int64(1)}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), pod, trigger, snapshot, kitManagedPolicy(identity, "whole-pod"))
	driver, err := NewKitGKEManagedCheckpointDriver(client)
	require.NoError(t, err)
	selected, err := driver.Select(t.Context(), trigger, binding)
	require.NoError(t, err)
	require.Equal(t, snapshot.GetUID(), selected.GetUID())
	// Selection is NOT availability, backing-object verification or restore.
	require.ErrorIs(t, driver.VerifyAvailable(t.Context(), selected, binding), ErrKitGKEManagedSnapshotIncompatible)
	for _, a := range client.Actions() {
		require.Equal(t, "get", a.GetVerb())
	}

	for _, tc := range []struct {
		name   string
		mutate func(*unstructured.Unstructured)
	}{
		{"stale generation", func(u *unstructured.Unstructured) { u.SetGeneration(2) }},
		{"missing result", func(u *unstructured.Unstructured) {
			unstructured.RemoveNestedField(u.Object, "status", "snapshotCreated")
		}},
		{"wrong result type", func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, int64(3), "status", "snapshotCreated")
		}},
		{"wrong target", func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, "other", "spec", "targetPod")
		}},
		{"replaced trigger", func(u *unstructured.Unstructured) { u.SetUID("replacement") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := trigger.DeepCopy()
			tc.mutate(next)
			_, err := client.Resource(kitGKETriggerGVR).Namespace(identity.Namespace).Update(t.Context(), next, metav1.UpdateOptions{})
			require.NoError(t, err)
			_, err = driver.Select(t.Context(), trigger, binding)
			require.Error(t, err)
		})
	}
	_, err = client.Resource(kitGKETriggerGVR).Namespace(identity.Namespace).Update(t.Context(), trigger, metav1.UpdateOptions{})
	require.NoError(t, err)
	pod.SetUID("replaced-source")
	_, err = client.Resource(gkePodGVR).Namespace(identity.Namespace).Update(t.Context(), pod, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = driver.Select(t.Context(), trigger, binding)
	require.ErrorContains(t, err, "authoritative binding")
}

func TestKitGKEManagedStopPreservesLiveRuntime(t *testing.T) {
	t.Parallel()
	k, client, r, fences := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	// The policy probe uses a separate read-only fake; lifecycle authority
	// and Pod operations remain on the fixture client for mutation assertions.
	clientWithPolicies := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{kitGKEPolicyGVR: "PodSnapshotPolicyList"}, kitManagedPolicy(k.identity(r), "whole-pod"))
	driver, err := NewKitGKEManagedCheckpointDriver(clientWithPolicies)
	require.NoError(t, err)
	k.config.Provision.SnapshotsEnabled = true
	k.config.Snapshots = driver
	client.ClearActions()
	require.ErrorIs(t, k.Stop(t.Context(), r), ErrKitGKEManagedSnapshotIncompatible)
	require.Zero(t, *fences)
	for _, a := range client.Actions() {
		require.Equal(t, "get", a.GetVerb(), "incompatible snapshot must not mutate authority, create trigger or delete Pod")
	}
	state, _, err := k.loadAuthority(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, "active", state.Phase)
}

func TestKitGKEManagedRestoreNeverTreatsRunningAsEvidence(t *testing.T) {
	t.Parallel()
	driver, err := NewKitGKEManagedCheckpointDriver(dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()))
	require.NoError(t, err)
	require.Error(t, driver.VerifyRestored(t.Context(), nil, nil, KitGKESnapshotBinding{}))
	_, err = NewKitGKEManagedCheckpointDriver(nil)
	require.Error(t, err)
}

func TestKitGKEManagedRootfsReady(t *testing.T) {
	t.Parallel()
	identity := kitGKEInput().Identity
	binding := KitGKESnapshotBinding{PolicyName: "kit-manual", PolicyUID: "policy-uid", PolicyGeneration: 1, Identity: identity, SourcePodName: "source", SourcePodUID: "source-uid"}
	policy := kitManagedPolicy(identity, "rootfs-only")
	snapshot := kitObject("PodSnapshot", "podsnapshot.gke.io/v1", "snapshot", identity)
	snapshot.SetLabels(nil) // No undocumented controller label propagation.
	snapshot.SetUID("snapshot-uid")
	snapshot.SetGeneration(1)
	snapshot.Object["spec"] = map[string]any{"policyName": policy.GetName()}
	snapshot.Object["status"] = map[string]any{"observedGeneration": int64(1), "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{kitGKEPolicyGVR: "PodSnapshotPolicyList"}, policy, snapshot)
	driver, err := NewKitGKEManagedCheckpointDriver(client)
	require.NoError(t, err)
	pod := kitObject("Pod", "v1", "source", identity)
	pod.SetUID(binding.SourcePodUID)
	_, err = driver.checkpointPolicy(t.Context(), pod)
	require.NoError(t, err)
	_, err = VerifyKitGKESnapshot(t.Context(), snapshot, binding, driver)
	require.NoError(t, err)
	require.Error(t, driver.VerifyRestored(t.Context(), pod, snapshot, binding))
	pod.SetUID("restored-pod")
	require.ErrorContains(t, driver.VerifyRestored(t.Context(), pod, snapshot, binding), "selection mismatch")
	pod.SetAnnotations(map[string]string{"podsnapshot.gke.io/ps-name": snapshot.GetName()})
	require.NoError(t, driver.VerifyRestored(t.Context(), pod, snapshot, binding))
	snapshot.Object["status"] = map[string]any{"observedGeneration": int64(1), "conditions": []any{map[string]any{"type": "Ready", "status": "False"}}}
	_, err = client.Resource(kitGKESnapshotGVR).Namespace(identity.Namespace).Update(t.Context(), snapshot, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, driver.VerifyAvailable(t.Context(), snapshot, binding), "not Ready")
	snapshot.SetUID("replacement")
	require.ErrorContains(t, driver.VerifyAvailable(t.Context(), snapshot, binding), "UID changed")
}
