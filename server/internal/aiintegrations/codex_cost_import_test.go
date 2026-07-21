package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
)

func TestBuildCodexCostLogParamsVerifiesSHAAndMapsTelemetry(t *testing.T) {
	t.Parallel()

	body := []byte(`{"event_id":"event_1","type":"COSTS","timestamp":"2026-07-15T22:59:59Z","payload":{"day":"2026-07-15","hour":22,"organization_id":"org-openai","identity":{"user_id":"user_1","email":"Dev@Example.com","name":"Dev User","groups":[]},"product":"codex","client":"github","surface":"github_code_review","model":"gpt-5.5","service_tier":"default","reasoning":"high","measures":{"usage":{"text_input_tokens":75348,"text_cached_input_tokens":879616,"text_output_tokens":4858},"billing":[{"sku":"GPT-5.5 - Output","quantity":{"value":4858,"unit":"tokens"},"cost":{"value":3.6435,"unit":"CREDITS"}},{"sku":"GPT-5.5 - Input","quantity":{"value":75348,"unit":"tokens"},"cost":{"value":9.4185,"unit":"CREDITS"}},{"sku":"GPT-5.5 - Cached Input","quantity":{"value":879616,"unit":"tokens"},"cost":{"value":10.9952,"unit":"CREDITS"}}]}}}` + "\n")
	sum := sha256.Sum256(body)

	cfg := codexCostConfig()
	file := codexapi.LogFile{
		ID:         "eclf_123",
		EventType:  codexComplianceCostsEventType,
		EndTime:    time.Date(2026, 7, 16, 0, 27, 13, 340496000, time.UTC),
		FileName:   "COSTS_2026-07-16T00:27:13.340496+00:00.jsonl",
		FileSize:   int64(len(body)),
		FileSHA256: hex.EncodeToString(sum[:]),
	}

	logParams, err := buildCodexCostLogParams(cfg, file, body)
	require.NoError(t, err)
	require.Len(t, logParams, 1)

	logParam := logParams[0]
	require.Equal(t, time.Date(2026, 7, 15, 22, 59, 59, 0, time.UTC), logParam.Timestamp)
	require.Equal(t, "codex", logParam.ToolInfo.Name)
	require.Equal(t, codexUsageMetricsURN, logParam.ToolInfo.URN)
	require.Equal(t, "dev@example.com", logParam.UserInfo.Email())

	attrs := logParam.Attributes
	require.Equal(t, "api", attrs[attr.EventSourceKey])
	require.Equal(t, "codex", attrs[attr.HookSourceKey])
	require.Equal(t, "openai", attrs[attr.ProviderKey])
	require.Equal(t, cfg.ID.String(), attrs[attr.AIIntegrationConfigIDKey])
	require.Equal(t, "event_1", attrs[attr.CodexComplianceEventIDKey])
	require.Equal(t, "eclf_123", attrs[attr.CodexComplianceLogIDKey])
	require.Equal(t, "CREDITS", attrs[attr.CodexComplianceCostUnitKey])
	require.Equal(t, "github", attrs[attr.CodexComplianceClientKey])
	require.Equal(t, "github_code_review", attrs[attr.CodexComplianceSurfaceKey])
	require.Equal(t, "default", attrs[attr.CodexComplianceServiceTierKey])
	require.Equal(t, "high", attrs[attr.CodexComplianceReasoningKey])
	require.Equal(t, "GPT-5.5 - Output,GPT-5.5 - Input,GPT-5.5 - Cached Input", attrs[attr.CodexComplianceBillingSKUsKey])
	require.Equal(t, "org-openai", attrs[attr.ExternalOrgIDKey])
	require.Equal(t, "gpt-5.5", attrs[attr.GenAIResponseModelKey])
	require.Equal(t, int64(75348), attrs[attr.GenAIUsageInputTokensKey])
	require.Equal(t, int64(879616), attrs[attr.GenAIUsageCacheReadInputTokensKey])
	require.Equal(t, int64(4858), attrs[attr.GenAIUsageOutputTokensKey])
	require.Equal(t, int64(959822), attrs[attr.GenAIUsageTotalTokensKey])
	require.InDelta(t, 24.0572, attrs[attr.GenAIUsageCostKey], 0.000001)
}

func TestBuildCodexCostLogParamsRejectsSHAMismatch(t *testing.T) {
	t.Parallel()

	cfg := codexCostConfig()
	file := codexapi.LogFile{
		ID:         "eclf_123",
		EventType:  codexComplianceCostsEventType,
		EndTime:    time.Date(2026, 7, 16, 0, 27, 13, 340496000, time.UTC),
		FileName:   "COSTS_2026-07-16T00:27:13.340496+00:00.jsonl",
		FileSize:   3,
		FileSHA256: "not-the-right-hash",
	}
	_, err := buildCodexCostLogParams(cfg, file, []byte("{}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "sha256 mismatch")
}

func TestCodexCostSourceUpperBoundReturnsStartWhenNoLogs(t *testing.T) {
	t.Parallel()

	cfg := codexCostConfig()
	cfg.PollWatermarkAt = time.Time{}
	cfg.PollCheckpoint = timewindowpoller.CompletedCheckpoint(time.Time{})
	endTime := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	start := endTime.Add(-InitialUsagePollLookback)
	client := &stubCodexComplianceClient{
		listPages: []*codexapi.LogsPage{
			{Data: nil, HasMore: false, LastEndTime: time.Time{}},
		},
		listParams: nil,
		downloads:  nil,
	}
	source := &codexCostSource{
		client:      client,
		cfg:         cfg,
		principalID: "org-openai",
		pageLimit:   codexCompliancePageLimit,
		processPage: nil,
		progress:    &CodexCostSyncProgress{},
	}

	upperBound, err := source.UpperBound(t.Context(), endTime)

	require.NoError(t, err)
	require.Equal(t, start, upperBound)
	require.Len(t, client.listParams, 1)
	require.Equal(t, start, client.listParams[0].After)
}

func TestCodexCostPollerDoesNotAdvanceWatermarkWhenNoLogs(t *testing.T) {
	t.Parallel()

	cfg := codexCostConfig()
	cfg.PollWatermarkAt = time.Time{}
	cfg.PollCheckpoint = timewindowpoller.CompletedCheckpoint(time.Time{})
	endTime := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	client := &stubCodexComplianceClient{
		listPages: []*codexapi.LogsPage{
			{Data: nil, HasMore: false, LastEndTime: time.Time{}},
		},
		listParams: nil,
		downloads:  nil,
	}
	source := &codexCostSource{
		client:      client,
		cfg:         cfg,
		principalID: "org-openai",
		pageLimit:   codexCompliancePageLimit,
		processPage: func(context.Context, []telemetry.LogParams) error {
			return fmt.Errorf("process page should not be called")
		},
		progress: &CodexCostSyncProgress{},
	}
	store := &captureWatermarkStore{checkpoints: nil}
	runner := &timewindowpoller.Poller[[]codexapi.LogFile]{
		Store:    store,
		Schedule: ScheduleCodexCompliance,
		State: timewindowpoller.SyncState{
			SyncID:      cfg.SyncID,
			WatermarkAt: cfg.PollWatermarkAt,
			Checkpoint:  cfg.PollCheckpoint,
		},
		Source:  source,
		EndTime: endTime,
		Heartbeat: func(context.Context, int) {
		},
		InitialLookback: InitialUsagePollLookback,
		MaxWindow:       0,
		Granularity:     0,
		ResumeOffset:    0,
	}

	err := runner.Do(t.Context())

	require.NoError(t, err)
	require.Empty(t, store.checkpoints)
	require.Len(t, client.listParams, 1)
}

func TestCodexCostSourceFetchPageStopsAtWindowEnd(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC)
	inWindow := codexapi.LogFile{ID: "eclf_1", EventType: codexComplianceCostsEventType, EndTime: start.Add(30 * time.Minute), FileName: "", FileSize: 0, FileSHA256: ""}
	afterWindow := codexapi.LogFile{ID: "eclf_2", EventType: codexComplianceCostsEventType, EndTime: end.Add(time.Minute), FileName: "", FileSize: 0, FileSHA256: ""}
	client := &stubCodexComplianceClient{
		listPages: []*codexapi.LogsPage{
			{Data: []codexapi.LogFile{inWindow, afterWindow}, HasMore: true, LastEndTime: afterWindow.EndTime},
		},
		listParams: nil,
		downloads:  nil,
	}
	source := &codexCostSource{
		client:      client,
		cfg:         codexCostConfig(),
		principalID: "org-openai",
		pageLimit:   codexCompliancePageLimit,
		processPage: nil,
		progress:    &CodexCostSyncProgress{},
	}

	page, err := source.FetchPage(t.Context(), start, end, "")

	require.NoError(t, err)
	require.False(t, page.HasMore)
	require.Empty(t, page.NextPage)
	require.Equal(t, []codexapi.LogFile{inWindow}, page.Payload)
}

func codexCostConfig() Config {
	extOrgID := "org-openai"
	return Config{
		ID:                     uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		SyncID:                 uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		OrganizationID:         "org_gram",
		Provider:               ProviderCodexCompliance,
		ProjectID:              uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		ExternalOrganizationID: &extOrgID,
		BillingMode:            "",
		APIKey:                 "codex-key",
		Enabled:                true,
		PollWatermarkAt:        time.Date(2026, 7, 15, 22, 0, 0, 0, time.UTC),
		PollCheckpoint:         timewindowpoller.CompletedCheckpoint(time.Date(2026, 7, 15, 22, 0, 0, 0, time.UTC)),
		NextPollAfter:          time.Time{},
		LastPollError:          "",
		LastPollFailedAt:       time.Time{},
		LastPollSuccessAt:      time.Time{},
		ConsecutiveFailures:    0,
		LastCursor:             "",
		CreatedAt:              time.Time{},
		UpdatedAt:              time.Time{},
	}
}

type stubCodexComplianceClient struct {
	listPages  []*codexapi.LogsPage
	listParams []codexapi.ListLogsParams
	downloads  map[string][]byte
}

func (c *stubCodexComplianceClient) ListLogs(_ context.Context, params codexapi.ListLogsParams) (*codexapi.LogsPage, error) {
	c.listParams = append(c.listParams, params)
	if len(c.listPages) == 0 {
		return nil, fmt.Errorf("unexpected codex list logs call")
	}
	page := c.listPages[0]
	c.listPages = c.listPages[1:]
	return page, nil
}

func (c *stubCodexComplianceClient) DownloadLog(_ context.Context, _ string, logID string) ([]byte, error) {
	body, ok := c.downloads[logID]
	if !ok {
		return nil, fmt.Errorf("unexpected codex download log call for %s", logID)
	}
	return body, nil
}

type captureWatermarkStore struct {
	checkpoints []timewindowpoller.PollCheckpoint
}

func (s *captureWatermarkStore) AdvanceWatermark(_ context.Context, _ uuid.UUID, checkpoint timewindowpoller.PollCheckpoint) error {
	s.checkpoints = append(s.checkpoints, checkpoint)
	return nil
}
