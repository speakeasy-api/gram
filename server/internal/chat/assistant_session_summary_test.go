package chat_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func grantAssistantSummaryRead(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
		authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource),
	)
}

func TestGetAssistantSessionSummary_RangeAndAssistantScoped(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantAssistantSummaryRead(t, initSessionCtx(t, ti))
	queries := repo.New(ti.conn)

	assistantID, err := queries.SeedAssistant(ctx, repo.SeedAssistantParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		Name:           "summary assistant",
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)

	chatID := seedChat(t, ctx, ti, "", "", "in-range session")
	require.NoError(t, queries.SeedAssistantThread(ctx, repo.SeedAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     ti.projectID,
		CorrelationID: "in-range",
		ChatID:        chatID,
	}))
	for _, createdAt := range []time.Time{now.Add(-time.Minute), now} {
		_, err := queries.SeedChatMessage(ctx, repo.SeedChatMessageParams{
			ChatID:    chatID,
			ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
			CreatedAt: pgtype.Timestamptz{Time: createdAt, InfinityModifier: pgtype.Finite, Valid: true},
		})
		require.NoError(t, err)
	}

	oldChatID := seedChat(t, ctx, ti, "", "", "old session")
	require.NoError(t, queries.SeedAssistantThread(ctx, repo.SeedAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     ti.projectID,
		CorrelationID: "old",
		ChatID:        oldChatID,
	}))
	_, err = queries.SeedChatMessage(ctx, repo.SeedChatMessageParams{
		ChatID:    oldChatID,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: now.Add(-2 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	setupChatID := seedChat(t, ctx, ti, "", "", "setup session")
	_, err = queries.UpsertSetupAssistantThread(ctx, repo.UpsertSetupAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     ti.projectID,
		CorrelationID: "setup",
		ChatID:        setupChatID,
	})
	require.NoError(t, err)
	_, err = queries.SeedChatMessage(ctx, repo.SeedChatMessageParams{
		ChatID:    setupChatID,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: now, InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	chatIDs, err := queries.ListAssistantSessionSummaryChatIDs(ctx, repo.ListAssistantSessionSummaryChatIDsParams{
		AssistantID:    assistantID,
		ProjectID:      ti.projectID,
		AfterChatID:    uuid.Nil,
		ExternalUserID: "",
		UserID:         "",
		PageLimit:      10,
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{chatID, oldChatID}, chatIDs)

	result, err := ti.service.GetAssistantSessionSummary(ctx, &gen.GetAssistantSessionSummaryPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		AssistantID:      assistantID.String(),
		From:             from.Format(time.RFC3339),
		To:               to.Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Sessions)
	require.Equal(t, int64(2), result.Messages)
	require.Zero(t, result.TotalTokens)
	require.Zero(t, result.TotalCost)
}

func TestGetAssistantSessionSummary_UnknownAssistant(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantAssistantSummaryRead(t, initSessionCtx(t, ti))
	now := time.Now().UTC()

	_, err := ti.service.GetAssistantSessionSummary(ctx, &gen.GetAssistantSessionSummaryPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		AssistantID:      uuid.NewString(),
		From:             now.Add(-time.Hour).Format(time.RFC3339),
		To:               now.Format(time.RFC3339),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetAssistantSessionSummary_RequiresProjectRead(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))
	now := time.Now().UTC()

	_, err := ti.service.GetAssistantSessionSummary(ctx, &gen.GetAssistantSessionSummaryPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		AssistantID:      uuid.NewString(),
		From:             now.Add(-time.Hour).Format(time.RFC3339),
		To:               now.Format(time.RFC3339),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetAssistantSessionSummary_NotLimitedToChatPageSize(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := grantAssistantSummaryRead(t, initSessionCtx(t, ti))
	queries := repo.New(ti.conn)

	assistantID, err := queries.SeedAssistant(ctx, repo.SeedAssistantParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		Name:           "unlimited summary assistant",
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	createdChatIDs := make([]uuid.UUID, 0, 101)

	for range 101 {
		chatID := seedChat(t, ctx, ti, "", "", "summary session")
		createdChatIDs = append(createdChatIDs, chatID)
		require.NoError(t, queries.SeedAssistantThread(ctx, repo.SeedAssistantThreadParams{
			AssistantID:   assistantID,
			ProjectID:     ti.projectID,
			CorrelationID: uuid.NewString(),
			ChatID:        chatID,
		}))
		_, err := queries.SeedChatMessage(ctx, repo.SeedChatMessageParams{
			ChatID:    chatID,
			ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
			CreatedAt: pgtype.Timestamptz{Time: now, InfinityModifier: pgtype.Finite, Valid: true},
		})
		require.NoError(t, err)
	}

	firstPage, err := queries.ListAssistantSessionSummaryChatIDs(ctx, repo.ListAssistantSessionSummaryChatIDsParams{
		AssistantID:    assistantID,
		ProjectID:      ti.projectID,
		AfterChatID:    uuid.Nil,
		ExternalUserID: "",
		UserID:         "",
		PageLimit:      100,
	})
	require.NoError(t, err)
	require.Len(t, firstPage, 100)
	secondPage, err := queries.ListAssistantSessionSummaryChatIDs(ctx, repo.ListAssistantSessionSummaryChatIDsParams{
		AssistantID:    assistantID,
		ProjectID:      ti.projectID,
		AfterChatID:    firstPage[len(firstPage)-1],
		ExternalUserID: "",
		UserID:         "",
		PageLimit:      100,
	})
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	require.ElementsMatch(t, createdChatIDs, append(firstPage, secondPage...))

	result, err := ti.service.GetAssistantSessionSummary(ctx, &gen.GetAssistantSessionSummaryPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		AssistantID:      assistantID.String(),
		From:             now.Add(-time.Hour).Format(time.RFC3339),
		To:               now.Add(time.Hour).Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, int64(101), result.Sessions)
	require.Equal(t, int64(101), result.Messages)
}
