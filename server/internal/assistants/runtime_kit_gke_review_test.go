package assistants

import (
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestKitGKEManagedPolicyRequiresObservedGeneration(t *testing.T) {
	t.Parallel()
	for _, observed := range []int64{-1, 0, 1, 2} {
		policy := kitManagedPolicy(kitGKEInput().Identity, "rootfs-only")
		if observed == -1 {
			delete(policy.Object, "status")
		} else {
			require.NoError(t, unstructured.SetNestedField(policy.Object, observed, "status", "observedGeneration"))
		}
		err := kitGKEManagedScope(policy)
		if observed == 1 {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrKitGKEManagedSnapshotSchema)
		}
	}
}

func TestKitGKEBackendRequiresHTTPSManagement(t *testing.T) {
	t.Parallel()
	k, _, _, _ := kitGKEBackendFixture(t)
	k.config.GKE.ServerURL.Scheme = "http"
	_, err := NewKitGKERuntimeBackend(k.config)
	require.ErrorContains(t, err, "HTTPS management URL")
}

func TestKitGKEStoppedReapPreservesReusableLineage(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	k.config.Provision.SnapshotsEnabled = true
	k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	claim, err := client.Resource(gkeSandboxClaimGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), kitGKEName(r), metav1.GetOptions{})
	require.NoError(t, err)
	require.NoError(t, k.Stop(t.Context(), r))
	// A leftover claim must be removed without retiring reusable authority.
	err = client.Tracker().Create(gkeSandboxClaimGVR, claim, k.config.GKE.Namespace)
	require.NoError(t, err)
	require.NoError(t, k.ReapStoppedMachine(t.Context(), r))
	_, err = client.Resource(gkeSandboxClaimGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), kitGKEName(r), metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err))
	state, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "stopped", state.Phase)
	_, err = k.get(t.Context(), r, kitTemplateGVR, kitGKEName(r), state.Metadata.TemplateUID)
	require.NoError(t, err)
	_, err = k.get(t.Context(), r, kitSecretGVR, state.Metadata.SecretName, state.Metadata.SecretUID)
	require.NoError(t, err)
	snapshot, err := client.Resource(kitGKESnapshotGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), state.SnapshotName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, state.SnapshotUID, snapshot.GetUID())
}

func TestKitGKESwitchSettledStoppedSource(t *testing.T) {
	t.Parallel()
	for _, rowState := range []string{runtimeStateStopped, runtimeStateFailed} {
		t.Run(rowState, func(t *testing.T) {
			t.Parallel()
			k, client, r, _ := kitGKEBackendFixture(t)
			k.config.Provision.SnapshotsEnabled = true
			k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
			result, err := k.Ensure(t.Context(), r)
			require.NoError(t, err)
			r.BackendMetadataJSON = result.BackendMetadataJSON
			r.State = rowState
			var assistant assistantRecord
			assistant.ID, assistant.ProjectID = r.AssistantID, r.ProjectID
			fence, err := k.PrepareRuntimeSwitch(t.Context(), assistant, []assistantRuntimeRecord{r}, 2)
			require.ErrorIs(t, err, errRuntimeSwitchUnavailable, "row state alone is not settlement")
			fence.Release()
			pod, err := client.Resource(gkePodGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), "pod-"+r.ID.String(), metav1.GetOptions{})
			require.NoError(t, err)
			require.NoError(t, k.Stop(t.Context(), r))
			fence, err = k.PrepareRuntimeSwitch(t.Context(), assistant, []assistantRuntimeRecord{r}, 2)
			require.NoError(t, err)
			require.True(t, fence.Settled)
			require.NotNil(t, fence.Release)
			fence.Release()
			_, err = client.Resource(gkePodGVR).Namespace(k.config.GKE.Namespace).Create(t.Context(), pod, metav1.CreateOptions{})
			require.NoError(t, err)
			fence, err = k.PrepareRuntimeSwitch(t.Context(), assistant, []assistantRuntimeRecord{r}, 2)
			require.ErrorIs(t, err, errRuntimeSwitchBusy, "stopped authority cannot override a live process")
			fence.Release()
		})
	}
}
