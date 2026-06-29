package activities_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestCorrelateClaudePrompts_MatchesOldMessageWhenEventTimeIsClose(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "claude_prompt_correlation_old")
	require.NoError(t, err)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Test Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Test Project",
		Slug:           "proj-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	sessionID := uuid.NewString()
	chatID, err := chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             sessionIDToUUIDForTest(sessionID),
		ProjectID:      project.ID,
		OrganizationID: orgID,
		UserID:         pgtype.Text{},
		ExternalUserID: pgtype.Text{},
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)

	messageTime := time.Now().UTC().Add(-70 * time.Minute)
	prompt := "please summarize the repository changes and include only the important implementation details"
	messageID, err := testrepo.New(conn).InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true},
		Role:      "user",
		Content:   prompt,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).UpdateChatMessageCreatedAt(ctx, testrepo.UpdateChatMessageCreatedAtParams{
		ID:        messageID,
		CreatedAt: pgtype.Timestamptz{Time: messageTime, Valid: true},
	}))

	insertClaudeUserPromptEventForActivityTest(t, ctx, telemetryrepo.New(chConn), claudeUserPromptActivityFixture{
		projectID:     project.ID.String(),
		chatID:        chatID.String(),
		sessionID:     sessionID,
		promptID:      "prompt-old-but-valid",
		prompt:        prompt,
		eventSequence: 1,
		timestamp:     messageTime.Add(30 * time.Second),
	})

	act := activities.NewCorrelateClaudePrompts(logger, conn, chConn)
	var messages []chatrepo.ChatMessage
	require.Eventually(t, func() bool {
		result, err := act.Do(ctx, activities.CorrelateClaudePromptsArgs{
			ProjectID:              project.ID,
			ChatID:                 chatID,
			SessionID:              sessionID,
			AfterMessageSeq:        0,
			AfterEventSequence:     0,
			AfterEventTimeUnixNano: 0,
		})
		require.NoError(t, err)
		require.False(t, result.HasMore)

		messages, err = chatrepo.New(conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: project.ID,
		})
		require.NoError(t, err)
		require.Len(t, messages, 1)
		return messages[0].MessageID.Valid && messages[0].MessageID.String == "prompt-old-but-valid"
	}, 3*time.Second, 50*time.Millisecond)
}

type claudeUserPromptActivityFixture struct {
	projectID     string
	chatID        string
	sessionID     string
	promptID      string
	prompt        string
	eventSequence int64
	timestamp     time.Time
}

func insertClaudeUserPromptEventForActivityTest(t *testing.T, ctx context.Context, queries *telemetryrepo.Queries, event claudeUserPromptActivityFixture) {
	t.Helper()

	attrs, err := json.Marshal(map[string]any{
		"event.name":     "user_prompt",
		"event.sequence": event.eventSequence,
		"session.id":     event.sessionID,
		"prompt.id":      event.promptID,
		"prompt":         event.prompt,
	})
	require.NoError(t, err)

	err = queries.InsertTelemetryLog(ctx, telemetryrepo.InsertTelemetryLogParams{
		ID:                   uuid.NewString(),
		TimeUnixNano:         event.timestamp.UnixNano(),
		ObservedTimeUnixNano: event.timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "claude_code.user_prompt",
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           string(attrs),
		ResourceAttributes:   "{}",
		GramProjectID:        event.projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "claude-code:otel:logs",
		ServiceName:          "claude-code",
		ServiceVersion:       nil,
		GramChatID:           &event.chatID,
	})
	require.NoError(t, err)
}

func sessionIDToUUIDForTest(sessionID string) uuid.UUID {
	if parsed, err := uuid.Parse(sessionID); err == nil {
		return parsed
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(sessionID))
}
