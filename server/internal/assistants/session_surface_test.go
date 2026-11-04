package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
)

func TestAssistantChatSessionSurface(t *testing.T) {
	t.Parallel()

	require.Equal(t, SessionSurfaceSpeakeasySlack, assistantChatSessionSurface(sourceKindSpeakeasySlack))
	for _, source := range []string{sourceKindSlack, sourceKindDashboard, sourceKindGithub, sourceKindCron, sourceKindWake, sourceKindWarmup, ""} {
		require.Empty(t, assistantChatSessionSurface(source), source)
	}
}

// The Speakeasy Slack app runs on the ordinary assistants runtime, so its
// sessions are told apart by a surface stamped from the persisted trigger
// source. Customer Slack apps and other sources keep the inferred
// "assistants" source.
func TestEnqueueStampsSpeakeasySlackSessionSurface(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "assistant_session_surface")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "assistant-session-surface")
	core := newProvisioningCore(t, db)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Surface target", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil)
	require.NoError(t, err)

	for _, tc := range []struct {
		source string
		event  string
		want   pgtype.Text
	}{
		{source: sourceKindSpeakeasySlack, event: `{"team_id":"T1","channel_id":"C1","thread_id":"1.0","user_id":"U1","text":"hi"}`, want: pgtype.Text{String: SessionSurfaceSpeakeasySlack, Valid: true}},
		{source: sourceKindSlack, event: `{"team_id":"T1","channel_id":"C1","thread_id":"2.0","user_id":"U1","text":"hi"}`, want: pgtype.Text{String: "", Valid: false}},
		{source: sourceKindGithub, event: `{"event_type":"issues","repo":"acme/repo"}`, want: pgtype.Text{String: "", Valid: false}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			result, err := core.EnqueueTriggerTask(t.Context(), bgtriggers.Task{
				DefinitionSlug: tc.source, TargetKind: bgtriggers.TargetKindAssistant, TargetRef: record.ID.String(),
				EventID: "evt-" + tc.source, CorrelationID: "corr-" + tc.source, EventJSON: []byte(tc.event),
			})
			require.NoError(t, err)
			require.NotEqual(t, uuid.Nil, result.ThreadID)

			chat, err := chatrepo.New(db).GetChat(t.Context(), chatrepo.GetChatParams{ID: deterministicChatID(record.ID, "corr-"+tc.source), ProjectID: project})
			require.NoError(t, err)
			require.Equal(t, tc.want, chat.SessionSurface)
		})
	}
}
