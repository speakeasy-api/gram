package risk_test

import (
	"encoding/json"
	"fmt"
	"strings"
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

func TestJudgeWindowRespectsGenerations(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	queries := riskrepo.New(ti.conn)
	chatID, err := queries.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{String: "", Valid: false}, ExternalUserID: pgtype.Text{String: "", Valid: false}})
	require.NoError(t, err)
	oldID, err := queries.CreateJudgeWindowMessageForTest(ctx, riskrepo.CreateJudgeWindowMessageForTestParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: "archived", Generation: 0})
	require.NoError(t, err)
	currentID, err := queries.CreateJudgeWindowMessageForTest(ctx, riskrepo.CreateJudgeWindowMessageForTestParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: "current", Generation: 1})
	require.NoError(t, err)
	loader := judgemessage.NewWindowLoader(ti.conn)
	target := judgemessage.New(message.User, "", "current")
	target.AnchorID, target.ChatID = currentID, chatID
	window, err := loader.Load(ctx, orgID, projectID.String(), target)
	require.NoError(t, err)
	require.Len(t, window.Messages, 1, "older generations cannot become preceding context")
	target.AnchorID, target.Body = oldID, "archived"
	window, err = loader.Load(ctx, orgID, projectID.String(), target)
	require.NoError(t, err)
	require.Len(t, window.Messages, 1, "newer generations cannot become following context")
	target.AnchorID, target.Body = uuid.Nil, "live"
	window, err = loader.Load(ctx, orgID, projectID.String(), target)
	require.NoError(t, err)
	require.Len(t, window.Messages, 2)
	require.Equal(t, "current", window.Messages[0].Body)
	require.Equal(t, "live", window.Messages[1].Body)
}

func TestJudgeWindowBoundsLegacyToolJSON(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	queries := riskrepo.New(ti.conn)
	chatID, err := queries.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{String: "", Valid: false}, ExternalUserID: pgtype.Text{String: "", Valid: false}})
	require.NoError(t, err)
	_, err = queries.CreateChatMessageWithToolCallsForTest(ctx, riskrepo.CreateChatMessageWithToolCallsForTestParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Role: "assistant", Content: "no tools", ToolCalls: []byte(`null`)})
	require.NoError(t, err)
	calls := make([]map[string]any, 10)
	for i := range calls {
		calls[i] = map[string]any{"function": map[string]string{"name": "read_file", "arguments": strings.Repeat("界", 64002)}}
	}
	raw, err := json.Marshal(calls)
	require.NoError(t, err)
	raw, err = json.Marshal(string(raw))
	require.NoError(t, err)
	_, err = queries.CreateChatMessageWithToolCallsForTest(ctx, riskrepo.CreateChatMessageWithToolCallsForTestParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Role: "assistant", Content: "", ToolCalls: raw})
	require.NoError(t, err)
	target := judgemessage.New(message.User, "", "live")
	target.ChatID = chatID
	window, err := judgemessage.NewWindowLoader(ti.conn).Load(ctx, orgID, projectID.String(), target)
	require.NoError(t, err)
	require.Len(t, window.Messages, 3)
	require.Empty(t, window.Messages[0].ToolCalls)
	require.Equal(t, "content", window.Messages[0].BodyKind)
	require.Len(t, window.Messages[1].ToolCalls, 8)
	require.True(t, window.Messages[1].ToolCallsTruncated)
	require.True(t, window.Messages[1].ToolCalls[0].ArgumentsTruncated)
	require.Equal(t, "read_file", window.Messages[1].ToolCalls[0].Tool.Name)
}
