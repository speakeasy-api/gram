package organizations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// A Cowork transcript only reaches Speakeasy once the plugin is registered in
// Claude.ai and the organization instruction moved a conversation into Cowork,
// so its first message completes both Set up Claude Desktop cards.
func TestService_ListSetupTasksCompletesClaudeDesktopOnceACoworkTranscriptArrives(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, ac.ProjectID)
	// Claude.ai syncs the published marketplace, so a real organization that
	// receives Cowork transcripts has already published it.
	_, err := pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID: *ac.ProjectID, InstallationID: 9001, RepoOwner: "example", RepoName: "setup-board",
		MarketplaceToken: conv.ToPGText("synthetic-marketplace-token"), PublishedMcpFingerprints: nil, PublishedHooksVersion: pgtype.Text{}, PublishedHooksConfig: nil,
	})
	require.NoError(t, err)

	chats := chatrepo.New(ti.conn)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	storeMessage := func(origin string) {
		t.Helper()
		chatID, err := chats.UpsertChat(ctx, chatrepo.UpsertChatParams{
			ID: uuid.New(), ProjectID: *ac.ProjectID, OrganizationID: ac.ActiveOrganizationID,
			UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{}, Title: pgtype.Text{},
		})
		require.NoError(t, err)
		_, err = writer.Write(ctx, *ac.ProjectID, []chat.MessageWrite{{Params: chatrepo.CreateChatMessageParams{
			ID: uuid.New(), ChatID: chatID, Role: "user", ProjectID: *ac.ProjectID, Content: "hello",
			ContentRaw: nil, ContentAssetUrl: pgtype.Text{}, StorageError: pgtype.Text{}, Model: pgtype.Text{},
			MessageID: pgtype.Text{}, ToolCallID: pgtype.Text{}, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
			FinishReason: pgtype.Text{}, ToolCalls: nil, PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0,
			Origin: conv.ToPGText(origin), UserAgent: pgtype.Text{}, IpAddress: pgtype.Text{}, Source: pgtype.Text{},
			ContentHash: nil, Generation: 0, Replayed: false, CreatedAt: pgtype.Timestamptz{},
		}, UserEmail: ""}})
		require.NoError(t, err)
	}
	cards := func() (*gen.SetupTask, *gen.SetupTask) {
		t.Helper()
		result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
		require.NoError(t, err)
		registration := setupTask(result.Tasks, "claude-plugin-registration")
		instructions := setupTask(result.Tasks, "claude-org-instructions")
		require.NotNil(t, registration)
		require.NotNil(t, instructions)
		return registration, instructions
	}

	_, instructions := cards()
	require.Equal(t, "todo", instructions.Status)
	require.False(t, instructions.CompletedByFact)
	require.Equal(t, []string{"claude-plugin-registration"}, instructions.BlockedBy)

	// Messages from other sources prove nothing about Cowork.
	storeMessage("anthropic-inference")
	registration, instructions := cards()
	require.False(t, registration.CompletedByFact)
	require.False(t, instructions.CompletedByFact)

	storeMessage("claude-transcript")
	registration, instructions = cards()
	require.Equal(t, "done", registration.Status)
	require.True(t, registration.CompletedByFact)
	require.Equal(t, "done", instructions.Status)
	require.True(t, instructions.CompletedByFact)
	require.Empty(t, instructions.BlockedBy, "the transcript also settles the registration card it waits on")
}
