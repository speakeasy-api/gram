package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestFetchOverviewChatMetricsReturnsMetrics(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}
	want := chatRepo.GetChatMetricsSummaryRow{TotalChats: 7, ResolvedChats: 3, FailedChats: 1, AvgSessionDurationMs: 1500, AvgResolutionTimeMs: 2000}

	got := s.fetchOverviewChatMetrics(t.Context(), time.Minute, func(context.Context) (chatRepo.GetChatMetricsSummaryRow, error) {
		return want, nil
	})
	require.Equal(t, want, got)
}

func TestFetchOverviewChatMetricsZeroesOnTimeout(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}

	got := s.fetchOverviewChatMetrics(t.Context(), time.Millisecond, func(ctx context.Context) (chatRepo.GetChatMetricsSummaryRow, error) {
		<-ctx.Done()
		return chatRepo.GetChatMetricsSummaryRow{TotalChats: 7}, ctx.Err()
	})
	require.Equal(t, chatRepo.GetChatMetricsSummaryRow{}, got)
}

func TestFetchOverviewChatMetricsZeroesOnError(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}

	got := s.fetchOverviewChatMetrics(t.Context(), time.Minute, func(context.Context) (chatRepo.GetChatMetricsSummaryRow, error) {
		return chatRepo.GetChatMetricsSummaryRow{TotalChats: 7}, errors.New("statement timeout")
	})
	require.Equal(t, chatRepo.GetChatMetricsSummaryRow{}, got)
}

func TestFetchOverviewChatMetricsZeroesWhenRequestCanceled(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got := s.fetchOverviewChatMetrics(ctx, time.Minute, func(ctx context.Context) (chatRepo.GetChatMetricsSummaryRow, error) {
		return chatRepo.GetChatMetricsSummaryRow{TotalChats: 7}, ctx.Err()
	})
	require.Equal(t, chatRepo.GetChatMetricsSummaryRow{}, got)
}
