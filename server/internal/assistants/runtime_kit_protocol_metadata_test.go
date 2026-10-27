package assistants

import (
	"fmt"
	"testing"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/stretchr/testify/require"
)

func TestKitRuntimeReservationMetadataInheritance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		prior, desired int16
		inherit        bool
	}{
		{3, 3, false}, {2, 3, false}, {3, 2, false}, {2, 2, true},
	} {
		t.Run(fmt.Sprintf("v%d_to_v%d", tc.prior, tc.desired), func(t *testing.T) {
			t.Parallel()
			_, db, key, _ := kitSessionFixture(t)
			queries := assistantrepo.New(db)
			// Model a previously provisioned runtime with a row-bound resource identity.
			err := queries.SetKitRuntimeMetadataFixture(t.Context(), assistantrepo.SetKitRuntimeMetadataFixtureParams{RuntimeVersion: tc.prior, Metadata: []byte(fmt.Sprintf(`{"runtime_id":%q,"resource":"previous-runtime"}`, key.RuntimeID.String())), ProjectID: key.ProjectID, RuntimeID: key.RuntimeID})
			require.NoError(t, err)
			require.NoError(t, queries.RetireAssistantRuntimesForSwitch(t.Context(), assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID}))
			require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: tc.desired}))
			require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: key.ProjectID, AssistantID: key.AssistantID, AssistantThreadID: key.ThreadID, Backend: runtimeBackendLocal, State: runtimeStateStarting}))
			next, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: key.ProjectID, AssistantID: key.AssistantID})
			require.NoError(t, err)
			require.NotEqual(t, key.RuntimeID, next.ID)
			require.Equal(t, tc.desired, next.RuntimeVersion)
			if tc.inherit {
				require.JSONEq(t, fmt.Sprintf(`{"runtime_id":%q,"resource":"previous-runtime"}`, key.RuntimeID.String()), string(next.BackendMetadataJson))
			} else {
				require.JSONEq(t, `{}`, string(next.BackendMetadataJson))
			}
		})
	}
}
