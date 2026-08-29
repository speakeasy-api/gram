package chat

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

func TestReadingsForMessagesLogsAndSkipsMeteringFailure(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	writer, shutdown := NewChatMessageWriter(logger, nil, nil)
	t.Cleanup(func() {
		require.NoError(t, shutdown(context.WithoutCancel(t.Context())))
	})

	projectID := uuid.New()
	messageID := uuid.New()
	param := repo.CreateChatMessageParams{}
	param.ID = messageID
	param.ProjectID = projectID
	param.Content = "meter this message"
	param.ToolCalls = []byte(`[{"function":`)

	readings, err := writer.meterMessages(
		t.Context(),
		logger,
		"org-"+uuid.NewString(),
		projectID,
		[]MessageWrite{{Params: param, UserEmail: ""}},
		time.Now().UTC(),
	)

	require.NoError(t, err)
	require.Empty(t, readings)
	require.Contains(t, logs.String(), "generate chat message storage reading")
	require.Contains(t, logs.String(), messageID.String())
}

func TestStoredMessageContentRejectsMalformedToolCalls(t *testing.T) {
	t.Parallel()

	parts, err := extractMeteredContent("message", []byte(`[{"function":`))

	require.Error(t, err)
	require.Nil(t, parts)
}

func TestStoredMessageContentRejectsNonStringArguments(t *testing.T) {
	t.Parallel()
	toolCalls := []byte(`[{"function":{"name":"lookup","arguments":{"city":"Paris"}}}]`)

	parts, err := extractMeteredContent("message", toolCalls)

	require.Error(t, err)
	require.Nil(t, parts)
}
