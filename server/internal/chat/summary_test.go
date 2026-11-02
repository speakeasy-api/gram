package chat_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// plantedSecrets are assembled at runtime so no literal credential lives in
// the repository.
func plantedSecrets() (githubToken, dbPassword string) {
	return "ghp_" + strings.Repeat("aB3dE5fG7h", 3) + "123456", "pg-" + "planted-" + "pw9"
}

func withTeamMemory(ti *chatTestInstance, client openrouter.CompletionClient, enabled bool) {
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagTeamMemory, feature.AnyDistinctID, enabled)
	ti.service.WithMaskedSummaries(chat.NewSummaryGenerator(ti.conn, client, chatredact.New()), flags)
}

func requestText(req openrouter.CompletionRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(openrouter.GetText(m))
		b.WriteString("\n")
	}
	return b.String()
}

func TestService_Summarize_MaskedPathRedactsInputAndOutput(t *testing.T) {
	t.Parallel()

	token, password := plantedSecrets()
	var sent openrouter.CompletionRequest
	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			req, ok := args.Get(1).(openrouter.CompletionRequest)
			require.True(t, ok)
			sent = req
		}).
		// The model echoes a secret; the stored summary must still be masked.
		Return(assistantTextResponse("The team rotated the deploy token "+token+" and moved retries to the queue."), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	withTeamMemory(ti, client, true)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "ext-user", "Retry queue")
	seedMessageContent(t, ctx, ti, chatID, "Use token "+token+" and postgres://app:"+password+"@db:5432/main for the retry worker")

	result, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String()})
	require.NoError(t, err)
	require.False(t, result.Cached)
	require.NotContains(t, result.Summary, token)
	require.Contains(t, result.Summary, "retries")

	require.Equal(t, chat.SummaryModel, sent.Model)
	require.Equal(t, openrouter.KeyTypeInternal, sent.KeyType)
	prompt := requestText(sent)
	require.NotContains(t, prompt, token)
	require.NotContains(t, prompt, password)
	require.Contains(t, prompt, "<transcript>")

	stored, err := repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: chatID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.True(t, stored.SummaryRedacted)
	require.Equal(t, result.Summary, stored.Summary.String)

	messages, err := repo.New(ti.conn).ListChatMessages(ctx, repo.ListChatMessagesParams{ChatID: chatID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.NotEmpty(t, messages)
	require.False(t, stored.SummaryGeneratedAt.Time.Before(messages[len(messages)-1].CreatedAt.Time))

	// Served from the stored masked summary without another model call.
	again, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String()})
	require.NoError(t, err)
	require.True(t, again.Cached)
	client.AssertExpectations(t)
}

func TestService_Summarize_MaskedPathReplacesUnmaskedSummary(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Return(assistantTextResponse("Masked summary."), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	withTeamMemory(ti, client, true)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "ext-user", "Legacy")
	seedMessageContent(t, ctx, ti, chatID, "Refactor the auth middleware")

	_, err := repo.New(ti.conn).UpdateChatSummary(ctx, repo.UpdateChatSummaryParams{
		Summary:         conv.ToPGText("Old unmasked summary."),
		SummaryRedacted: false,
		ID:              chatID,
		ProjectID:       ti.projectID,
	})
	require.NoError(t, err)

	result, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String()})
	require.NoError(t, err)
	require.False(t, result.Cached)
	require.Equal(t, "Masked summary.", result.Summary)
	client.AssertExpectations(t)
}

func TestService_Summarize_FlagOffKeepsLegacyPath(t *testing.T) {
	t.Parallel()

	var sent openrouter.CompletionRequest
	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			req, ok := args.Get(1).(openrouter.CompletionRequest)
			require.True(t, ok)
			sent = req
		}).
		Return(assistantTextResponse("Legacy summary."), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	withTeamMemory(ti, client, false)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "ext-user", "Flag off")
	seedMessageContent(t, ctx, ti, chatID, "Deploy the API to staging")

	result, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String()})
	require.NoError(t, err)
	require.Equal(t, "Legacy summary.", result.Summary)
	require.Empty(t, sent.Model)

	stored, err := repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: chatID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.False(t, stored.SummaryRedacted)
	client.AssertExpectations(t)
}

func TestUpdateChatSummary_StampsCoveredThroughWhenNewerMessageLanded(t *testing.T) {
	t.Parallel()

	ti := newTestChatServiceWithCompletion(t, &mockCompletionClient{})
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "ext-user", "Race")
	seedMessageContent(t, ctx, ti, chatID, "A message that landed after the transcript was read")

	// The summary covered the transcript up to an hour ago; the message above
	// is newer, so the summary must not read as covering it.
	coveredThrough := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	updated, err := repo.New(ti.conn).UpdateChatSummary(ctx, repo.UpdateChatSummaryParams{
		Summary:         conv.ToPGText("Summary of the earlier transcript."),
		SummaryRedacted: true,
		CoveredThrough:  pgtype.Timestamptz{Time: coveredThrough, InfinityModifier: pgtype.Finite, Valid: true},
		ID:              chatID,
		ProjectID:       ti.projectID,
	})
	require.NoError(t, err)
	require.True(t, updated.SummaryGeneratedAt.Time.Equal(coveredThrough))

	// Covering every message stamps the write time instead.
	updated, err = repo.New(ti.conn).UpdateChatSummary(ctx, repo.UpdateChatSummaryParams{
		Summary:         conv.ToPGText("Summary of the whole transcript."),
		SummaryRedacted: true,
		CoveredThrough:  pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		ID:              chatID,
		ProjectID:       ti.projectID,
	})
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), updated.SummaryGeneratedAt.Time, time.Minute)
}
