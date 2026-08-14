package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
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
	// Compliance rows come from the org's own enterprise feed, so they are
	// team by construction; billing mode only rides when the config declares
	// one (this fixture doesn't).
	require.Equal(t, complianceAccountTypeTeam, attrs[attr.AccountTypeKey])
	require.NotContains(t, attrs, attr.BillingModeKey)
	require.Equal(t, cfg.ID.String(), attrs[attr.AIIntegrationConfigIDKey])
	require.Equal(t, "event_1", attrs[attr.CodexComplianceEventIDKey])
	require.Equal(t, "90eb39010cad917b66d5b9d7ce27fe9b7217b93b02760406e55aee41eb5433c3", attrs[attr.CodexComplianceEventHashKey])
	require.Equal(t, "eclf_123", attrs[attr.CodexComplianceLogIDKey])
	require.Equal(t, "CREDITS", attrs[attr.CodexComplianceCostUnitKey])
	require.Equal(t, "github", attrs[attr.CodexComplianceClientKey])
	require.Equal(t, "github_code_review", attrs[attr.CodexComplianceSurfaceKey])
	require.Equal(t, "default", attrs[attr.CodexComplianceServiceTierKey])
	require.Equal(t, "high", attrs[attr.CodexComplianceReasoningKey])
	require.Equal(t, "GPT-5.5 - Output,GPT-5.5 - Input,GPT-5.5 - Cached Input", attrs[attr.CodexComplianceBillingSKUsKey])
	require.Equal(t, "org-openai", attrs[attr.ExternalOrgIDKey])
	require.Equal(t, "gpt-5.5", attrs[attr.GenAIResponseModelKey])
	// client=github is a cloud surface OTEL never sees, so this row promotes
	// its token counts to gen_ai.usage.* and meters from the compliance feed
	// (DNO-751); the raw codex.compliance.* copies still ride along.
	require.Equal(t, int64(75348), attrs[attr.GenAIUsageInputTokensKey])
	require.Equal(t, int64(879616), attrs[attr.GenAIUsageCacheReadInputTokensKey])
	require.Equal(t, int64(4858), attrs[attr.GenAIUsageOutputTokensKey])
	require.Equal(t, int64(959822), attrs[attr.GenAIUsageTotalTokensKey])
	require.Equal(t, int64(75348), attrs[attr.CodexComplianceInputTokensKey])
	require.Equal(t, int64(879616), attrs[attr.CodexComplianceCachedInputTokensKey])
	require.Equal(t, int64(4858), attrs[attr.CodexComplianceOutputTokensKey])
	require.Equal(t, int64(959822), attrs[attr.CodexComplianceTotalTokensKey])
	require.InDelta(t, 0.962288, attrs[attr.GenAIUsageCostKey], 0.000001)
}

func TestBuildCodexCostLogParamsRoutesNonCodexProductsToChatGPTURN(t *testing.T) {
	t.Parallel()

	// One file mixing the three product surfaces observed in the compliance
	// feed. Only the codex row may land under codex:usage:metrics; ChatGPT and
	// Work rows are parked under chatgpt:usage:metrics.
	body := []byte(
		`{"event_id":"event_codex","type":"COSTS","timestamp":"2026-07-19T15:59:59Z","payload":{"day":"2026-07-19","hour":15,"identity":{"user_id":"user_1","email":"dev@example.com"},"product":"codex","client":"exec","surface":"exec","model":"gpt-5.6-sol","measures":{"usage":{"text_input_tokens":10,"text_output_tokens":5},"billing":[{"sku":"GPT-5.6 - Input","quantity":{"value":10,"unit":"tokens"},"cost":{"value":0.5,"unit":"CREDITS"}}]}}}` + "\n" +
			`{"event_id":"event_chatgpt","type":"COSTS","timestamp":"2026-07-19T16:59:59Z","payload":{"day":"2026-07-19","hour":16,"identity":{"user_id":"user_2","email":"dev2@example.com"},"product":"ChatGPT","client":"web","model":"gpt-5-5","measures":{"usage":{"text_input_tokens":20,"text_output_tokens":8},"billing":[]}}}` + "\n" +
			`{"event_id":"event_work","type":"COSTS","timestamp":"2026-07-19T17:59:59Z","payload":{"day":"2026-07-19","hour":17,"identity":{"user_id":"user_3","email":"dev3@example.com"},"product":"Work","client":"web","model":"gpt-5-5","measures":{"usage":{"text_input_tokens":30,"text_output_tokens":9},"billing":[{"sku":"GPT-5.5 - Input","quantity":{"value":30,"unit":"tokens"},"cost":{"value":1.0,"unit":"CREDITS"}}]}}}` + "\n",
	)

	cfg := codexCostConfig()
	file := codexapi.LogFile{
		ID:         "eclf_products",
		EventType:  codexComplianceCostsEventType,
		EndTime:    time.Date(2026, 7, 19, 18, 0, 0, 0, time.UTC),
		FileName:   "COSTS_2026-07-19T18:00:00.000000+00:00.jsonl",
		FileSize:   int64(len(body)),
		FileSHA256: "",
	}

	logParams, err := buildCodexCostLogParams(cfg, file, body)
	require.NoError(t, err)
	require.Len(t, logParams, 3)

	codexRow := logParams[0]
	require.Equal(t, codexUsageMetricsURN, codexRow.ToolInfo.URN)
	require.Equal(t, codexHookSource, codexRow.ToolInfo.Name)
	require.Equal(t, codexUsageMetricsURN, codexRow.Attributes[attr.ResourceURNKey])
	require.Equal(t, codexHookSource, codexRow.Attributes[attr.HookSourceKey])
	require.Equal(t, "codex", codexRow.Attributes[attr.CodexComplianceProductKey])
	// Metered cost only on the codex row; the raw token counts move to the
	// codex.compliance.* keys, out of reach of the agent-usage predicates.
	require.NotContains(t, codexRow.Attributes, attr.GenAIUsageInputTokensKey)
	require.NotContains(t, codexRow.Attributes, attr.GenAIUsageOutputTokensKey)
	require.NotContains(t, codexRow.Attributes, attr.GenAIUsageTotalTokensKey)
	require.Equal(t, int64(10), codexRow.Attributes[attr.CodexComplianceInputTokensKey])
	require.Equal(t, int64(5), codexRow.Attributes[attr.CodexComplianceOutputTokensKey])
	require.Equal(t, int64(15), codexRow.Attributes[attr.CodexComplianceTotalTokensKey])
	require.InDelta(t, 0.02, codexRow.Attributes[attr.GenAIUsageCostKey], 0.000001)

	chatgptRow := logParams[1]
	require.Equal(t, chatgptUsageMetricsURN, chatgptRow.ToolInfo.URN)
	require.Equal(t, chatgptHookSource, chatgptRow.ToolInfo.Name)
	require.Equal(t, chatgptUsageMetricsURN, chatgptRow.Attributes[attr.ResourceURNKey])
	require.Equal(t, chatgptHookSource, chatgptRow.Attributes[attr.HookSourceKey])
	require.Equal(t, "ChatGPT", chatgptRow.Attributes[attr.CodexComplianceProductKey])
	// Parked non-Codex rows keep gen_ai.usage token counts — the compliance
	// feed is their only usage source — and gain no codex.compliance.* copies.
	require.Equal(t, int64(20), chatgptRow.Attributes[attr.GenAIUsageInputTokensKey])
	require.Equal(t, int64(8), chatgptRow.Attributes[attr.GenAIUsageOutputTokensKey])
	require.Equal(t, int64(28), chatgptRow.Attributes[attr.GenAIUsageTotalTokensKey])
	require.NotContains(t, chatgptRow.Attributes, attr.CodexComplianceInputTokensKey)

	workRow := logParams[2]
	require.Equal(t, chatgptUsageMetricsURN, workRow.ToolInfo.URN)
	// Work shares the chatgpt URN but gets its own hook_source: hook_source
	// is a summary GROUP BY dimension, so this is what keeps ChatGPT and
	// Work spend separable after the raw rows age out.
	require.Equal(t, chatgptWorkHookSource, workRow.ToolInfo.Name)
	require.Equal(t, chatgptWorkHookSource, workRow.Attributes[attr.HookSourceKey])
	require.Equal(t, "Work", workRow.Attributes[attr.CodexComplianceProductKey])
	require.Equal(t, int64(30), workRow.Attributes[attr.GenAIUsageInputTokensKey])
	// Billing still prices non-Codex rows; the cost just lands under the
	// chatgpt URN instead of the codex stream.
	require.InDelta(t, 0.04, workRow.Attributes[attr.GenAIUsageCostKey], 0.000001)
}

func TestBuildCodexCostLogParamsRoutesMissingProductToChatGPTURN(t *testing.T) {
	t.Parallel()

	// A row with no product claim must not be counted as Codex spend.
	body := []byte(`{"event_id":"event_no_product","type":"COSTS","timestamp":"2026-07-19T15:59:59Z","payload":{"identity":{"user_id":"user_1","email":"dev@example.com"},"measures":{"usage":{"text_input_tokens":10},"billing":[]}}}` + "\n")

	cfg := codexCostConfig()
	file := codexapi.LogFile{
		ID:         "eclf_no_product",
		EventType:  codexComplianceCostsEventType,
		EndTime:    time.Date(2026, 7, 19, 16, 0, 0, 0, time.UTC),
		FileName:   "COSTS_2026-07-19T16:00:00.000000+00:00.jsonl",
		FileSize:   int64(len(body)),
		FileSHA256: "",
	}

	logParams, err := buildCodexCostLogParams(cfg, file, body)
	require.NoError(t, err)
	require.Len(t, logParams, 1)
	require.Equal(t, chatgptUsageMetricsURN, logParams[0].ToolInfo.URN)
	require.Equal(t, chatgptHookSource, logParams[0].Attributes[attr.HookSourceKey])
}

// A timestamp-less event must land on a time intrinsic to the event, not on
// the delivering file's end time. The feed repeats an event_id across files, so
// a file-derived time would place two copies of one event at different times
// and the dedupe lookup would miss the copy already written.
func TestBuildCodexCostLogParamsFallsBackToPayloadBucketNotFileEndTime(t *testing.T) {
	t.Parallel()

	body := []byte(`{"event_id":"event_1","type":"COSTS","payload":{"day":"2026-07-15","hour":22,"product":"codex","client":"web","model":"gpt-5.5","measures":{"usage":{"text_input_tokens":10,"text_cached_input_tokens":0,"text_output_tokens":5},"billing":[]}}}` + "\n")
	cfg := codexCostConfig()

	// The same event delivered by two files with different end times.
	first, err := buildCodexCostLogParams(cfg, codexapi.LogFile{
		ID:        "eclf_first",
		EventType: codexComplianceCostsEventType,
		EndTime:   time.Date(2026, 7, 16, 0, 27, 13, 0, time.UTC),
	}, body)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := buildCodexCostLogParams(cfg, codexapi.LogFile{
		ID:        "eclf_second",
		EventType: codexComplianceCostsEventType,
		EndTime:   time.Date(2026, 7, 18, 9, 3, 44, 0, time.UTC),
	}, body)
	require.NoError(t, err)
	require.Len(t, second, 1)

	bucket := time.Date(2026, 7, 15, 22, 0, 0, 0, time.UTC)
	require.Equal(t, bucket, first[0].Timestamp)
	require.Equal(t, second[0].Timestamp, first[0].Timestamp, "both deliveries must agree on event time")
	require.Equal(t,
		second[0].Attributes[attr.CodexComplianceEventHashKey],
		first[0].Attributes[attr.CodexComplianceEventHashKey],
	)
}

// With neither a timestamp nor a usable day/hour bucket there is nothing
// intrinsic left, and the file's end time is the last resort.
func TestBuildCodexCostLogParamsFallsBackToFileEndTimeWithoutBucket(t *testing.T) {
	t.Parallel()

	body := []byte(`{"event_id":"event_1","type":"COSTS","payload":{"hour":22,"product":"codex","client":"web","model":"gpt-5.5","measures":{"usage":{"text_input_tokens":10,"text_cached_input_tokens":0,"text_output_tokens":5},"billing":[]}}}` + "\n")
	endTime := time.Date(2026, 7, 16, 0, 27, 13, 0, time.UTC)

	logParams, err := buildCodexCostLogParams(codexCostConfig(), codexapi.LogFile{
		ID:        "eclf_123",
		EventType: codexComplianceCostsEventType,
		EndTime:   endTime,
	}, body)
	require.NoError(t, err)
	require.Len(t, logParams, 1)
	require.Equal(t, endTime, logParams[0].Timestamp)
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

func TestBuildCodexCostLogParamsRejectsMissingEventID(t *testing.T) {
	t.Parallel()

	cfg := codexCostConfig()
	file := codexapi.LogFile{
		ID:         "eclf_123",
		EventType:  codexComplianceCostsEventType,
		EndTime:    time.Date(2026, 7, 16, 0, 27, 13, 340496000, time.UTC),
		FileName:   "COSTS_2026-07-16T00:27:13.340496+00:00.jsonl",
		FileSize:   0,
		FileSHA256: "",
	}
	body := []byte(`{"type":"COSTS","timestamp":"2026-07-15T22:59:59Z","payload":{"identity":{"email":"dev@example.com"},"measures":{"usage":{},"billing":[]}}}` + "\n")

	_, err := buildCodexCostLogParams(cfg, file, body)

	require.Error(t, err)
	require.Contains(t, err.Error(), "missing event_id")
	// The message names the log file so a poisoned file in a multi-file
	// window is identifiable from the stored poll error alone.
	require.Contains(t, err.Error(), "eclf_123")
}

func TestBuildCodexCostLogParamsDecodeErrorNamesLogAndCause(t *testing.T) {
	t.Parallel()

	cfg := codexCostConfig()
	file := codexapi.LogFile{
		ID:         "eclf_bad",
		EventType:  codexComplianceCostsEventType,
		EndTime:    time.Date(2026, 7, 16, 0, 27, 13, 340496000, time.UTC),
		FileName:   "COSTS_2026-07-16T00:27:13.340496+00:00.jsonl",
		FileSize:   0,
		FileSHA256: "",
	}

	_, err := buildCodexCostLogParams(cfg, file, []byte("\x1f\x8b not json"))

	require.Error(t, err)
	require.Contains(t, err.Error(), "decode codex compliance cost log eclf_bad")
	// The json cause survives the wrap so last_poll_error shows what was
	// actually wrong with the bytes.
	require.Contains(t, err.Error(), "invalid character")
}

// Pagination edge cases (empty windows, non-advancing last_end_time, window
// truncation, foreign-type filtering) are covered for this source and the
// ChatGPT conversation source together in logfile_source_pagination_test.go.

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
		client:    client,
		cfg:       cfg,
		pageLimit: codexCompliancePageLimit,
		processPage: func(context.Context, []telemetry.LogParams) (int, int, error) {
			return 0, 0, fmt.Errorf("process page should not be called")
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
		InitialLookback: CodexComplianceInitialLookback,
		MaxWindow:       0,
		Granularity:     0,
		ResumeOffset:    0,
	}

	err := runner.Do(t.Context())

	require.NoError(t, err)
	require.Empty(t, store.checkpoints)
	require.Len(t, client.listParams, 1)
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

func (c *stubCodexComplianceClient) DownloadLog(_ context.Context, logID string) ([]byte, error) {
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

// TestBuildCodexCostLogParamsStampsConfigBillingMode: the admin-declared
// billing mode on the codex_compliance config rides on Codex rows only — the
// org-level tier of the billing-mode cascade, keyed directly since compliance
// rows have no session to attribute through (DNO-734). ChatGPT/Work rows stay
// unlabeled: the single declaration cannot describe both surfaces, and
// labeling seat usage with a metered Codex declaration would render
// token-priced estimates as confident real cost.
func TestBuildCodexCostLogParamsStampsConfigBillingMode(t *testing.T) {
	t.Parallel()

	body := []byte(
		`{"event_id":"event_billing","type":"COSTS","timestamp":"2026-07-19T15:59:59Z","payload":{"identity":{"user_id":"user_1","email":"dev@example.com"},"product":"codex","measures":{"usage":{"text_input_tokens":10},"billing":[]}}}` + "\n" +
			`{"event_id":"event_billing_chat","type":"COSTS","timestamp":"2026-07-19T16:59:59Z","payload":{"identity":{"user_id":"user_2","email":"dev2@example.com"},"product":"ChatGPT","measures":{"usage":{"text_input_tokens":20},"billing":[]}}}` + "\n",
	)

	cfg := codexCostConfig()
	cfg.BillingMode = "flat_rate"
	file := codexapi.LogFile{
		ID:         "eclf_billing",
		EventType:  codexComplianceCostsEventType,
		EndTime:    time.Date(2026, 7, 19, 16, 0, 0, 0, time.UTC),
		FileName:   "COSTS_2026-07-19T16:00:00.000000+00:00.jsonl",
		FileSize:   int64(len(body)),
		FileSHA256: "",
	}

	logParams, err := buildCodexCostLogParams(cfg, file, body)
	require.NoError(t, err)
	require.Len(t, logParams, 2)

	codexRow := logParams[0]
	require.Equal(t, complianceAccountTypeTeam, codexRow.Attributes[attr.AccountTypeKey])
	require.Equal(t, "flat_rate", codexRow.Attributes[attr.BillingModeKey])

	chatgptRow := logParams[1]
	require.Equal(t, complianceAccountTypeTeam, chatgptRow.Attributes[attr.AccountTypeKey])
	require.NotContains(t, chatgptRow.Attributes, attr.BillingModeKey)
}

// TestBuildCodexCostLogParamsPartitionsMeteringByClient pins the DNO-751
// surface partition: cloud clients (github, web) promote token counts to
// gen_ai.usage.* and meter from the compliance feed; device clients (cli,
// exec) meter via OTEL so their rows stay cost-only, and ambiguous clients
// (desktop_app, unknown, absent) default to un-metered — the allowlist means
// a new surface can never silently double count.
func TestBuildCodexCostLogParamsPartitionsMeteringByClient(t *testing.T) {
	t.Parallel()

	cfg := codexCostConfig()

	// Pin the production allowlist itself: expectations below are hand-declared
	// on purpose (deriving them from codexCloudMeteredClients would make the
	// assertions tautological), so this equality check is what forces anyone
	// adding a surface to consciously extend the client list and expectations.
	require.Equal(t, map[string]bool{"github": true, "web": true}, codexCloudMeteredClients)

	promoted := map[string]bool{"github": true, "web": true, "GitHub": true}
	for _, client := range []string{"github", "web", "GitHub", "cli", "exec", "desktop_app", "unknown", ""} {
		clientField := ""
		if client != "" {
			clientField = `"client":"` + client + `",`
		}
		body := []byte(`{"event_id":"event_` + strings.ToLower(strings.TrimSpace(client)) + `_partition","type":"COSTS","timestamp":"2026-07-19T15:59:59Z","payload":{"identity":{"user_id":"user_1","email":"dev@example.com"},"product":"codex",` + clientField + `"measures":{"usage":{"text_input_tokens":100,"text_cached_input_tokens":40,"text_output_tokens":10},"billing":[]}}}` + "\n")
		file := codexapi.LogFile{
			ID:         "eclf_partition",
			EventType:  codexComplianceCostsEventType,
			EndTime:    time.Date(2026, 7, 19, 16, 0, 0, 0, time.UTC),
			FileName:   "COSTS_2026-07-19T16:00:00.000000+00:00.jsonl",
			FileSize:   int64(len(body)),
			FileSHA256: "",
		}

		logParams, err := buildCodexCostLogParams(cfg, file, body)
		require.NoError(t, err, client)
		require.Len(t, logParams, 1, client)
		attrs := logParams[0].Attributes

		// Raw copies ride on every codex row regardless of promotion.
		require.Equal(t, int64(100), attrs[attr.CodexComplianceInputTokensKey], client)
		require.Equal(t, int64(150), attrs[attr.CodexComplianceTotalTokensKey], client)

		if promoted[client] {
			require.Equal(t, int64(100), attrs[attr.GenAIUsageInputTokensKey], client)
			require.Equal(t, int64(40), attrs[attr.GenAIUsageCacheReadInputTokensKey], client)
			require.Equal(t, int64(10), attrs[attr.GenAIUsageOutputTokensKey], client)
			require.Equal(t, int64(150), attrs[attr.GenAIUsageTotalTokensKey], client)
		} else {
			require.NotContains(t, attrs, attr.GenAIUsageInputTokensKey, client)
			require.NotContains(t, attrs, attr.GenAIUsageCacheReadInputTokensKey, client)
			require.NotContains(t, attrs, attr.GenAIUsageOutputTokensKey, client)
			require.NotContains(t, attrs, attr.GenAIUsageTotalTokensKey, client)
		}
	}
}
