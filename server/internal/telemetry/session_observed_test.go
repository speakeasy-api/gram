package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestImportedSessionObservationReachesUserAndSessionAnalytics(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	projectID := uuid.MustParse(ti.projectID)
	now := time.Now().UTC().Add(-time.Minute)
	chatID, err := chatrepo.New(ti.conn).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID: uuid.New(), ProjectID: projectID, OrganizationID: ti.orgID,
		UserID: conv.ToPGText("imported-user"), ExternalUserID: conv.ToPGText("person@example.test"),
		ExternalChatID: conv.ToPGText("provider-conversation"), Title: conv.ToPGText("Imported conversation"),
		CreatedAt: conv.ToPGTimestamptz(now), UpdatedAt: conv.ToPGTimestamptz(now), PreferStoredTitle: true,
	})
	require.NoError(t, err)
	writer, shutdown := chat.NewChatMessageWriter(ti.logger, ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { require.NoError(t, shutdown(context.WithoutCancel(t.Context()))) })
	var write chat.ExternalMessageWrite
	write.Params.ChatID = chatID
	write.Params.ProjectID = projectID
	write.Params.Role = "user"
	write.Params.Source = conv.ToPGText("claude-chat-web")
	write.Params.Content = "Summarize the release checklist."
	write.Params.ExternalMessageID = conv.ToPGText("first-message")
	write.Params.CreatedAt = conv.ToPGTimestamptz(now)
	write.UserEmail = "person@example.test"
	writes := []chat.ExternalMessageWrite{write}
	for range 2 {
		_, err = writer.WriteExternal(ctx, projectID, writes)
		require.NoError(t, err)
	}
	outbox, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	var observations []*telemetryv1.SessionObserved
	for _, row := range outbox {
		if row.Topic != string(proto.MessageName(&telemetryv1.SessionObserved{})) {
			continue
		}
		observation := &telemetryv1.SessionObserved{}
		require.NoError(t, proto.Unmarshal(row.Message, observation))
		observations = append(observations, observation)
	}
	require.Len(t, observations, 1, "retrying transcript storage must not enqueue twice")
	handler := telemetry.NewSessionObservedHandler(ti.conn, ti.telemLogger)
	for range 2 {
		require.NoError(t, handler.HandleBatch(ctx, observations, nil))
	}
	metrics, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		UserID: new("imported-user"), From: now.Add(-time.Hour).Format(time.RFC3339), To: now.Add(time.Hour).Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, metrics.Metrics.TotalChats)
	require.Zero(t, metrics.Metrics.TotalTokens)
	require.Zero(t, metrics.Metrics.TotalToolCalls)
	aggregates, err := ti.chClient.QueryAttributeMetricsTable(ctx, repo.AttributeMetricsQueryParams{
		ProjectIDs: []string{ti.projectID}, TimeStart: now.Add(-time.Hour).UnixNano(), TimeEnd: now.Add(time.Hour).UnixNano(), SortBy: "total_chats",
	})
	require.NoError(t, err)
	require.Len(t, aggregates, 1)
	require.EqualValues(t, 1, aggregates[0].TotalChats)
	require.Zero(t, aggregates[0].TotalCost)
	var sessions, records uint64
	require.NoError(t, ti.chConn.QueryRow(ctx, `SELECT uniqExact(session_id), uniqExact(record_id) FROM agent_events WHERE organization_id = ? AND project_id = ?`, ti.orgID, ti.projectID).Scan(&sessions, &records))
	require.EqualValues(t, 1, sessions)
	require.EqualValues(t, 1, records)
	for _, window := range []time.Duration{time.Hour, repo.SessionSummaryMinWindow + 24*time.Hour} {
		sessions, err := ti.chClient.ListSessions(ctx, repo.ListSessionsParams{
			ProjectIDs: []string{ti.projectID}, TimeStart: now.Add(-window).UnixNano(), TimeEnd: now.Add(time.Hour).UnixNano(), SortBy: "message_count", Limit: 10,
		})
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		require.Equal(t, chatID.String(), sessions[0].GramChatID)
		require.EqualValues(t, 1, sessions[0].MessageCount, "delivery retries must not add messages")
		require.Zero(t, sessions[0].TotalTokens)
		require.Zero(t, sessions[0].TotalCost)
	}
}
