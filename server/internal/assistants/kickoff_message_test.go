package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// A kickoff turn must drive the model (so it generates a greeting) but never
// write a visible user row — only the assistant's reply should surface in the
// conversation log.
func TestKickoffMessageHiddenFromLog(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_kickoff_hidden")
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test")
	require.NoError(t, err)
	ingestor := &fakeDashboardIngestor{core: svc.core, assistantID: managed.ID}
	svc.core.SetDashboardIngestor(ingestor)

	const correlation = "user-test"

	// A real user message lands in the visible log.
	_, err = svc.SendMessage(ctx, &gen.SendMessagePayload{
		AssistantID:   managed.ID.String(),
		Message:       "what are my top errors?",
		CorrelationID: correlation,
	})
	require.NoError(t, err)

	// The kickoff threads into the same conversation and returns its chat.
	res, err := svc.KickoffMessage(ctx, &gen.KickoffMessagePayload{
		AssistantID:   managed.ID.String(),
		CorrelationID: correlation,
	})
	require.NoError(t, err)
	require.True(t, res.Accepted)
	require.Equal(t, deterministicChatID(managed.ID, correlation).String(), res.ChatID)

	// The kickoff instruction still reaches the model (carried hidden in the
	// ingest payload).
	require.Contains(t, string(ingestor.lastPayload), `"hidden":true`)
	require.Contains(t, string(ingestor.lastPayload), "reopened the assistant panel")

	// But the visible log holds only the real user message — never the kickoff
	// instruction.
	full, err := svc.ListMessages(ctx, &gen.ListMessagesPayload{ChatID: res.ChatID})
	require.NoError(t, err)
	require.Len(t, full.Messages, 1)
	require.Equal(t, "user", full.Messages[0].Role)
	require.Equal(t, "what are my top errors?", full.Messages[0].Content)
	for _, m := range full.Messages {
		require.NotContains(t, m.Content, "reopened the assistant panel",
			"the hidden kickoff instruction must not appear in the visible log")
	}
}

func TestKickoffMessageRequiresAssistant(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_kickoff_404")
	svc.core.SetDashboardIngestor(&fakeDashboardIngestor{core: svc.core})
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	_, err := svc.KickoffMessage(ctx, &gen.KickoffMessagePayload{
		AssistantID:   uuid.New().String(),
		CorrelationID: "user-test",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestKickoffMessageRequiresProjectGrant(t *testing.T) {
	t.Parallel()

	svc, ctx, _ := newRBACService(t)
	ctx = authztest.WithExactGrants(t, ctx) // no grants

	_, err := svc.KickoffMessage(ctx, &gen.KickoffMessagePayload{
		AssistantID:   uuid.New().String(),
		CorrelationID: "user-test",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
