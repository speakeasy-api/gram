package assistants

import (
	"testing"

	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

func TestRuntimeEventAdmissionRejectsRetiredAuthority(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "runtime_event_authority")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	runtimeID := insertActiveV2RuntimeRow(t, conn, projectID, assistantID, threadID, runtimeBackendLocal, runtimeStateActive, `{}`)
	runtime := testRuntimeRecord(runtimeBackendLocal)
	runtime.ID, runtime.ProjectID, runtime.AssistantID = runtimeID, projectID, assistantID
	runtime.RuntimeVersion = 2
	queries := assistantrepo.New(conn)
	require.NoError(t, queries.RetireAssistantRuntimesForSwitch(t.Context(), assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: projectID, AssistantID: assistantID}))
	core := newProvisioningCore(t, conn)
	_, claimed, err := core.claimRuntimeEvent(t.Context(), runtime, threadID)
	require.ErrorIs(t, err, errRuntimeAuthorityRetired)
	require.False(t, claimed)
	event, err := queries.GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{ProjectID: projectID, AssistantThreadID: threadID})
	require.NoError(t, err)
	require.Equal(t, eventStatusPending, event.Status)
	require.Zero(t, event.Attempts)
}

func TestRuntimeEventAdmissionClaimsLiveAuthority(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "runtime_event_live")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	runtimeID := insertActiveV2RuntimeRow(t, conn, projectID, assistantID, threadID, runtimeBackendLocal, runtimeStateActive, `{}`)
	runtime := testRuntimeRecord(runtimeBackendLocal)
	runtime.ID, runtime.ProjectID, runtime.AssistantID = runtimeID, projectID, assistantID
	runtime.RuntimeVersion = 2
	core := newProvisioningCore(t, conn)
	event, claimed, err := core.claimRuntimeEvent(t.Context(), runtime, threadID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, assistantID, event.AssistantID)
	require.Equal(t, eventStatusProcessing, event.Status)
}

func TestRuntimeEventAdmissionClaimsStartingKitAuthority(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "kit_starting_event_root")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	queries := assistantrepo.New(conn)
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: projectID, AssistantID: assistantID, RuntimeVersion: 3}))
	runtimeID := insertActiveV2RuntimeRow(t, conn, projectID, assistantID, threadID, runtimeBackendLocal, runtimeStateStarting, `{}`)
	runtime := testRuntimeRecord(runtimeBackendLocal)
	runtime.ID, runtime.ProjectID, runtime.AssistantID = runtimeID, projectID, assistantID
	runtime.RuntimeVersion = 3
	core := newProvisioningCore(t, conn)
	event, claimed, err := core.claimRuntimeEvent(t.Context(), runtime, threadID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, assistantID, event.AssistantID)
	require.Equal(t, eventStatusProcessing, event.Status)
}
