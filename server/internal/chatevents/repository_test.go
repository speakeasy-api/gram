package chatevents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func upsertTestChat(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID, externalChatID, inferenceSessionID string) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	row, err := chatrepo.New(conn).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID: uuid.New(), ProjectID: projectID, OrganizationID: organizationID,
		UserID: conv.ToPGText("chat-user"), ExternalUserID: conv.ToPGText("person@example.test"),
		ExternalChatID: conv.ToPGText(externalChatID), InferenceSessionID: conv.ToPGTextEmpty(inferenceSessionID),
		Title:     conv.ToPGText("Imported conversation"),
		CreatedAt: conv.ToPGTimestamptz(now), UpdatedAt: conv.ToPGTimestamptz(now), PreferStoredTitle: true,
	})
	require.NoError(t, err)
	return row.ID
}

func TestRepositoryLoadsOnlyTheTenantsLiveMessages(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "chatevents")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessions := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("chatevents"), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, conn, sessions)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	organizationID := authCtx.ActiveOrganizationID

	live := upsertTestChat(t, ctx, conn, projectID, organizationID, "ses_live", "ses_inference")
	deleted := upsertTestChat(t, ctx, conn, projectID, organizationID, "ses_deleted", "")
	_, err = chatrepo.New(conn).SoftDeleteChat(ctx, chatrepo.SoftDeleteChatParams{ProjectID: projectID, ID: deleted})
	require.NoError(t, err)

	liveMessage, deletedMessage := uuid.New(), uuid.New()
	var params []chatrepo.CreateChatMessageParams
	for _, m := range []struct {
		id   uuid.UUID
		chat uuid.UUID
	}{{id: liveMessage, chat: live}, {id: deletedMessage, chat: deleted}} {
		var p chatrepo.CreateChatMessageParams
		p.ID, p.ChatID, p.ProjectID, p.Role, p.Content = m.id, m.chat, projectID, "user", "hello"
		p.Source = conv.ToPGText("claude-code")
		p.MessageID = conv.ToPGText("prompt_01")
		p.CreatedAt = conv.ToPGTimestamptz(time.Now().UTC())
		params = append(params, p)
	}
	_, err = chatrepo.New(conn).CreateChatMessage(ctx, params)
	require.NoError(t, err)

	rows, err := NewRepository(conn).LoadMessages(ctx, organizationID, projectID, []uuid.UUID{liveMessage, deletedMessage})
	require.NoError(t, err)
	require.Len(t, rows, 1, "a deleted chat's messages are not loaded")
	require.Equal(t, liveMessage, rows[0].ID)
	require.Equal(t, live, rows[0].ChatID)
	require.Equal(t, "hello", rows[0].Content)
	require.Equal(t, "prompt_01", rows[0].MessageID.String)
	require.Equal(t, "ses_live", rows[0].ExternalChatID.String)
	require.Equal(t, "ses_inference", rows[0].InferenceSessionID.String)
	require.Equal(t, organizationID, rows[0].OrganizationID)
	require.Equal(t, "chat-user", rows[0].UserID, "an empty message actor falls back to the chat's owner")
	require.Equal(t, "person@example.test", rows[0].UserEmail, "an email-shaped external user id stands in for a directory email")

	rows, err = NewRepository(conn).LoadMessages(ctx, "other-organization", projectID, []uuid.UUID{liveMessage})
	require.NoError(t, err)
	require.Empty(t, rows, "the read is pinned to the event's organization")
}
