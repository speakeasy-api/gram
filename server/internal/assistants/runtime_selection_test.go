package assistants

import (
	"testing"

	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

func TestAssistantDesiredRuntimeVersionProjection(t *testing.T) {
	t.Parallel()
	for _, version := range []int16{2, 3} {
		records := map[string]assistantRecord{
			"managed":  assistantRecordFromManagedRow(assistantrepo.GetManagedAssistantByProjectRow{RuntimeVersion: version}),
			"create":   assistantRecordFromCreateRow(assistantrepo.CreateAssistantRow{RuntimeVersion: version}),
			"list":     assistantRecordFromListRow(assistantrepo.ListAssistantsRow{RuntimeVersion: version}),
			"get":      assistantRecordFromGetRow(assistantrepo.GetAssistantRow{RuntimeVersion: version}),
			"dispatch": assistantRecordFromDispatchRow(assistantrepo.GetAssistantForDispatchRow{RuntimeVersion: version}),
			"update":   assistantRecordFromUpdateRow(assistantrepo.UpdateAssistantRow{RuntimeVersion: version}),
		}
		for name, record := range records {
			if record.RuntimeVersion != version {
				t.Errorf("%s: desired runtime version = %d, want %d", name, record.RuntimeVersion, version)
			}
		}
	}
}

func TestAssistantAdmissionRejectsUnavailableDesiredRuntime(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "runtime_selection_admission")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)
	queries := assistantrepo.New(conn)
	err = queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		AssistantID: assistantID, ProjectID: projectID, RuntimeVersion: 3,
	})
	require.NoError(t, err)
	core := &ServiceCore{db: conn}

	// Even a pre-lock legacy snapshot cannot reserve a legacy runtime after
	// the persisted selection changes. No backend needs to be configured.
	_, err = core.admitPendingThreadsV2(t.Context(), assistantRecord{
		ID: assistantID, ProjectID: projectID, RuntimeVersion: 2, Status: StatusActive,
	})
	require.ErrorContains(t, err, "version 3 admission is not configured")
	_, err = core.EnsureWarmupThread(t.Context(), assistantID)
	require.ErrorContains(t, err, "version 3 admission is not configured")
	count, err := queries.CountAssistantRuntimeRowsForTest(t.Context(), assistantrepo.CountAssistantRuntimeRowsForTestParams{
		AssistantID: assistantID, ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Zero(t, count)
}
