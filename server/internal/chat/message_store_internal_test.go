package chat

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/identity"
	"github.com/speakeasy-api/gram/server/internal/metering"
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

func TestMessageReadingAttributionClassifiesNativeWorkload(t *testing.T) {
	t.Parallel()

	assistantID, source := messageReadingAttribution(t.Context(), billing.ModelUsageSourcePlayground)

	require.Equal(t, uuid.Nil, assistantID)
	require.Equal(t, metering.WorkloadSourceNative, source)
}

func TestMessageReadingAttributionClassifiesAssistantPrincipal(t *testing.T) {
	t.Parallel()

	assistantID := uuid.New()
	ctx := contextvalues.SetAssistantPrincipal(t.Context(), contextvalues.AssistantPrincipal{
		AssistantID: assistantID,
		ThreadID:    uuid.New(),
	})

	actualAssistantID, source := messageReadingAttribution(ctx, billing.ModelUsageSourcePlayground)

	require.Equal(t, assistantID, actualAssistantID)
	require.Equal(t, metering.WorkloadSourceAssistant, source)
}

func TestMessageReadingAttributionClassifiesAssistantSetup(t *testing.T) {
	t.Parallel()

	assistantID, source := messageReadingAttribution(t.Context(), billing.ModelUsageSourceAssistants)

	require.Equal(t, uuid.Nil, assistantID)
	require.Equal(t, metering.WorkloadSourceAssistant, source)
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

// The writer is the one place every producer stamps actor identity, so the
// four fields it stamps must move together: the two persisted columns, the
// email the meter and publication read, and the billing user.
func TestMessageWrite_StampAttribution(t *testing.T) {
	t.Parallel()

	attribution := identity.Attribution{UserID: "user_1", Email: "dev@example.com", ExternalUserID: "end-user-7", Basis: identity.BasisCredential}

	write := MessageWrite{}
	write.StampAttribution(attribution)
	require.Equal(t, "user_1", write.Params.UserID.String)
	require.True(t, write.Params.UserID.Valid)
	require.Equal(t, "end-user-7", write.Params.ExternalUserID.String)
	require.Equal(t, "dev@example.com", write.UserEmail)
	require.Equal(t, "user_1", write.BillingUserID)

	billedElsewhere := MessageWrite{BillingUserID: "user_billing"}
	billedElsewhere.StampAttribution(attribution)
	require.Equal(t, "user_billing", billedElsewhere.BillingUserID)

	unattributed := MessageWrite{}
	unattributed.StampAttribution(identity.Attribution{Basis: identity.BasisUnattributed})
	require.False(t, unattributed.Params.UserID.Valid)
	require.False(t, unattributed.Params.ExternalUserID.Valid)
	require.Empty(t, unattributed.BillingUserID)

	external := ExternalMessageWrite{}
	external.StampAttribution(attribution)
	require.Equal(t, "user_1", external.Params.UserID.String)
	require.Equal(t, "end-user-7", external.Params.ExternalUserID.String)
	require.Equal(t, "dev@example.com", external.UserEmail)
	require.Equal(t, "user_1", external.BillingUserID)
}
