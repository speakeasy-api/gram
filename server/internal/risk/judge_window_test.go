package risk_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/stretchr/testify/require"
)

func TestJudgeWindowBoundsAndTenantIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	queries := riskrepo.New(ti.conn)
	chatID, err := queries.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{String: "", Valid: false}, ExternalUserID: pgtype.Text{String: "", Valid: false}})
	require.NoError(t, err)
	ids := make([]uuid.UUID, 7)
	for i := range ids {
		ids[i], err = queries.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: fmt.Sprintf("message %d", i), UserID: pgtype.Text{String: "", Valid: false}, ExternalUserID: pgtype.Text{String: "", Valid: false}})
		require.NoError(t, err)
	}
	loader := judgemessage.NewWindowLoader(ti.conn)
	target := judgemessage.New(message.User, "", "message 3")
	target.AnchorID, target.ChatID = ids[3], chatID
	window, err := loader.Load(ctx, orgID, projectID.String(), target)
	require.NoError(t, err)
	require.Len(t, window.Messages, 5)
	require.Equal(t, 2, window.TargetIndex)
	for i, msg := range window.Messages {
		require.Equal(t, fmt.Sprintf("message %d", i+1), msg.Body)
	}
	_, err = loader.Load(ctx, "another-organization", projectID.String(), target)
	require.Error(t, err)
	_, err = loader.Load(ctx, orgID, uuid.NewString(), target)
	require.Error(t, err)
	target.AnchorID = uuid.Nil
	target.Body = "live target"
	window, err = loader.Load(ctx, orgID, projectID.String(), target)
	require.NoError(t, err)
	require.Len(t, window.Messages, 5)
	require.Equal(t, 4, window.TargetIndex)
	require.Equal(t, "message 3", window.Messages[0].Body)
	require.Equal(t, "live target", window.Messages[4].Body)
	window, err = loader.Load(ctx, "another-organization", projectID.String(), target)
	require.NoError(t, err)
	require.Len(t, window.Messages, 1, "no cross-tenant neighbors returned for unpersisted target")
}
