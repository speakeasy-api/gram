package assistants

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestKitGKECheckpointGC(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	state, handle, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	state.Phase = "stopped"
	state.TriggerName, state.TriggerUID = "latest-trigger", "latest-trigger-uid"
	state.SnapshotName, state.SnapshotUID = "latest-snapshot", "latest-snapshot-uid"
	state.PriorCheckpoints = []kitGKECheckpointRef{{TriggerName: "old-trigger", TriggerUID: "old-trigger-uid", SnapshotName: "old-snapshot", SnapshotUID: "old-snapshot-uid"}}
	handle, err = k.saveAuthority(t.Context(), handle, state)
	require.NoError(t, err)
	failSnapshot := true
	client.PrependReactor("delete", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		deletion, ok := action.(ktesting.DeleteAction)
		require.True(t, ok)
		require.NotNil(t, deletion.GetDeleteOptions().Preconditions)
		require.Equal(t, deletion.GetName()+"-uid", string(*deletion.GetDeleteOptions().Preconditions.UID))
		if deletion.GetName() == "old-snapshot" && failSnapshot {
			return true, nil, fmt.Errorf("temporary cleanup failure")
		}
		return false, nil, nil
	})
	_, _, err = k.gcSupersededCheckpoints(t.Context(), state, handle)
	require.ErrorContains(t, err, "temporary cleanup failure")
	state, handle, err = k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, state.PriorCheckpoints, 1)
	failSnapshot = false
	state, handle, err = k.gcSupersededCheckpoints(t.Context(), state, handle)
	require.NoError(t, err)
	require.Empty(t, state.PriorCheckpoints)
	require.Equal(t, "latest-snapshot", state.SnapshotName)
	_, _, err = k.gcRetiredCheckpoints(t.Context(), state, handle)
	require.ErrorContains(t, err, "permanent retirement")
	state.Phase = "retired"
	handle, err = k.saveAuthority(t.Context(), handle, state)
	require.NoError(t, err)
	state, _, err = k.gcRetiredCheckpoints(t.Context(), state, handle)
	require.NoError(t, err)
	require.Empty(t, state.SnapshotName)
	require.Empty(t, state.TriggerUID)
}

func TestKitGKECheckpointGCGuards(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	state, handle, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	state.PriorCheckpoints = []kitGKECheckpointRef{{SnapshotName: "old", SnapshotUID: "old-uid"}}
	state.Phase = "checkpointing"
	_, _, err = k.gcSupersededCheckpoints(t.Context(), state, handle)
	require.ErrorContains(t, err, "durable replacement")
	state.Phase = "stopped"
	_, _, err = k.gcSupersededCheckpoints(t.Context(), state, handle)
	require.ErrorContains(t, err, "replacement snapshot")
	state.SnapshotName, state.SnapshotUID = "old", "old-uid"
	_, _, err = k.gcSupersededCheckpoints(t.Context(), state, handle)
	require.ErrorContains(t, err, "overlaps")
	require.Error(t, k.deleteCheckpointRef(t.Context(), kitGKECheckpointRef{SnapshotName: "missing-uid"}))
	for _, action := range client.Actions() {
		require.NotEqual(t, "delete", action.GetVerb())
	}
}

func TestKitGKEStopRetriesSupersededCleanupAfterDurableReplacement(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	k.config.Provision.SnapshotsEnabled = true
	k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	state, handle, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	state.TriggerName, state.TriggerUID = "previous-trigger", "previous-trigger-uid"
	state.SnapshotName, state.SnapshotUID = "previous-snapshot", "previous-snapshot-uid"
	_, err = k.saveAuthority(t.Context(), handle, state)
	require.NoError(t, err)
	fail := true
	client.PrependReactor("delete", "podsnapshots", func(action ktesting.Action) (bool, runtime.Object, error) {
		deletion, ok := action.(ktesting.DeleteAction)
		require.True(t, ok)
		if deletion.GetName() != "previous-snapshot" {
			return false, nil, nil
		}
		durable, _, err := k.loadAuthority(t.Context(), r)
		require.NoError(t, err)
		require.Equal(t, "checkpointed", durable.Phase)
		require.Equal(t, "verified-snapshot", durable.SnapshotName)
		require.NotEmpty(t, durable.SnapshotUID)
		if fail {
			return true, nil, fmt.Errorf("temporary snapshot cleanup failure")
		}
		return false, nil, nil
	})
	require.ErrorContains(t, k.Stop(t.Context(), r), "temporary snapshot cleanup failure")
	state, _, err = k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "checkpointed", state.Phase)
	require.Len(t, state.PriorCheckpoints, 1)
	fail = false
	require.NoError(t, k.Stop(t.Context(), r))
	state, _, err = k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Empty(t, state.PriorCheckpoints)
	require.Equal(t, "stopped", state.Phase)
	require.Equal(t, "verified-snapshot", state.SnapshotName)
	// Repeated idle cleanup keeps the latest checkpoint and restore inputs.
	require.NoError(t, k.ReapStoppedMachine(t.Context(), r))
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" && action.GetResource() == kitGKESnapshotGVR {
			deletion, ok := action.(ktesting.DeleteAction)
			require.True(t, ok)
			require.NotEqual(t, "verified-snapshot", deletion.GetName())
		}
	}
	_, err = k.get(t.Context(), r, kitTemplateGVR, kitGKETemplateName(kitGKEName(r)), state.Metadata.TemplateUID)
	require.NoError(t, err)
	_, err = k.get(t.Context(), r, kitSecretGVR, state.Metadata.SecretName, state.Metadata.SecretUID)
	require.NoError(t, err)
}
