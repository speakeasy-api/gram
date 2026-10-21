//go:build demoseed_safety

package demoseed

import (
	"encoding/json"
	"testing"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/stretchr/testify/require"
)

func TestGoogleChatTriggerReseed(t *testing.T) {
	t.Parallel()
	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		t.Run(spec.OrgID, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "googlechatseed")
			require.NoError(t, err)
			for range 2 {
				seedLocalPostgres(ctx, t, db, spec)
				s := &seeder{spec: spec}
				count, err := repo.New(db).PostflightIDPausedGoogleChatTrigger(ctx, repo.PostflightIDPausedGoogleChatTriggerParams{
					OrgID: spec.OrgID, ProjectID: s.projectID(), AssistantID: s.det("assistant-bound"),
				})
				require.NoError(t, err)
				require.EqualValues(t, 1, count, "reseed must preserve one paused, credential-free Google Chat trigger")
				instance, err := triggerrepo.New(db).GetTriggerInstanceByIDPublic(ctx, s.det("assistant-googlechat-bound"))
				require.NoError(t, err)
				var config map[string]any
				require.NoError(t, json.Unmarshal(instance.ConfigJson, &config))
				definition, ok := bgtriggers.GetDefinition("googlechat")
				require.True(t, ok)
				_, err = definition.DecodeConfig(config)
				require.NoError(t, err)
				require.Contains(t, config, "workspace_events")
				require.Equal(t, "paused", instance.Status)
				require.False(t, instance.EnvironmentID.Valid)
			}
		})
	}
}
