package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

	svc := NewCodexCostImportService(testenv.NewLogger(t), nil, nil, func(context.Context, string, int) {})
	logParams, err := svc.buildCodexCostLogParams(cfg, file, body)
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
	svc := NewCodexCostImportService(testenv.NewLogger(t), nil, nil, func(context.Context, string, int) {})

	_, err := svc.buildCodexCostLogParams(cfg, file, []byte("{}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "sha256 mismatch")
}

func codexCostConfig() Config {
	extOrgID := "org-openai"
	return Config{
		ID:                     uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		OrganizationID:         "org_gram",
		Provider:               ProviderCodexCompliance,
		ProjectID:              uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		ExternalOrganizationID: &extOrgID,
		BillingMode:            "",
		APIKey:                 "codex-key",
		Enabled:                true,
		PollWatermarkAt:        time.Date(2026, 7, 15, 22, 0, 0, 0, time.UTC),
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
