package aiintegrations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
)

const (
	codexComplianceCostsEventType = "COSTS"
	codexCompliancePageLimit      = 100
	codexUsageMetricsURN          = "codex:usage:metrics"
	codexHookSource               = "codex"
	codexProviderOpenAI           = "openai"
)

type codexComplianceClient interface {
	ListLogs(ctx context.Context, params codexapi.ListLogsParams) (*codexapi.LogsPage, error)
	DownloadLog(ctx context.Context, principalID, logID string) ([]byte, error)
}

type CodexCostImportService struct {
	logger          *slog.Logger
	guardianPolicy  *guardian.Policy
	telemetryLogger *telemetry.Logger
	heartbeat       func(ctx context.Context, scope string, page int)
}

func NewCodexCostImportService(logger *slog.Logger, telemetryLogger *telemetry.Logger, guardianPolicy *guardian.Policy, heartbeat func(ctx context.Context, scope string, page int)) *CodexCostImportService {
	if heartbeat == nil {
		panic("codex cost import service requires heartbeat")
	}
	return &CodexCostImportService{
		logger:          logger.With(attr.SlogComponent("aiintegrations.codex_compliance")),
		guardianPolicy:  guardianPolicy,
		telemetryLogger: telemetryLogger,
		heartbeat:       heartbeat,
	}
}

// SyncCodexCosts imports Codex cost compliance logs and returns the newest
// compliance log end_time reached by the run. The caller persists that as the
// next `after` watermark; if no files are available, the watermark stays put so
// late-arriving compliance files are not skipped.
func (s *CodexCostImportService) SyncCodexCosts(ctx context.Context, cfg Config) (time.Time, error) {
	if cfg.Provider != ProviderCodexCompliance {
		return time.Time{}, oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for codex cost import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == nil {
		return time.Time{}, oops.E(oops.CodeInvalid, nil, "external_organization_id is required for codex_compliance")
	}

	client := codexapi.New(s.guardianPolicy, codexapi.WithAPIKey(cfg.APIKey))
	progress := &CodexCostSyncProgress{
		WindowStart:       cfg.PollWatermarkAt,
		LogPages:          0,
		LogFiles:          0,
		CostEvents:        0,
		CostEventsWritten: 0,
		WatermarkReached:  cfg.PollWatermarkAt,
	}
	watermark, err := s.syncCodexCosts(ctx, client, cfg, progress)
	if err != nil {
		return time.Time{}, newSyncError("sync codex costs", *progress,
			SyncStageError{Stage: "import_cost_logs", Err: err},
		)
	}
	return watermark, nil
}

func (s *CodexCostImportService) syncCodexCosts(ctx context.Context, client codexComplianceClient, cfg Config, progress *CodexCostSyncProgress) (time.Time, error) {
	principalID := *cfg.ExternalOrganizationID
	after := cfg.PollWatermarkAt
	watermark := cfg.PollWatermarkAt

	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "cost_log_discovery", pageNum)
		page, err := client.ListLogs(ctx, codexapi.ListLogsParams{
			PrincipalID: principalID,
			EventType:   codexComplianceCostsEventType,
			After:       after,
			Limit:       codexCompliancePageLimit,
		})
		if err != nil {
			return watermark, oops.E(oops.CodeUnexpected, err, "list codex compliance cost logs")
		}
		progress.LogPages++

		for _, file := range page.Data {
			if file.EventType != "" && file.EventType != codexComplianceCostsEventType {
				continue
			}
			body, err := client.DownloadLog(ctx, principalID, file.ID)
			if err != nil {
				return watermark, oops.E(oops.CodeUnexpected, err, "download codex compliance cost log")
			}
			progress.LogFiles++

			logParams, err := s.buildCodexCostLogParams(cfg, file, body)
			if err != nil {
				return watermark, err
			}
			progress.CostEvents += len(logParams)

			if err := s.writeCodexCostTelemetry(ctx, logParams); err != nil {
				return watermark, err
			}
			progress.CostEventsWritten += len(logParams)

			if file.EndTime.After(watermark) {
				watermark = file.EndTime
				progress.WatermarkReached = watermark
			}
		}

		if page.LastEndTime.After(watermark) {
			watermark = page.LastEndTime
			progress.WatermarkReached = watermark
		}
		if !page.HasMore {
			return watermark, nil
		}
		if page.LastEndTime.IsZero() {
			return watermark, oops.E(oops.CodeUnexpected, nil, "codex compliance logs page had has_more without last_end_time")
		}
		after = page.LastEndTime
	}
}

func (s *CodexCostImportService) buildCodexCostLogParams(cfg Config, file codexapi.LogFile, body []byte) ([]telemetry.LogParams, error) {
	if file.FileSHA256 != "" {
		sum := sha256.Sum256(body)
		actual := hex.EncodeToString(sum[:])
		if !strings.EqualFold(actual, file.FileSHA256) {
			return nil, oops.E(oops.CodeUnexpected, nil, "codex compliance log sha256 mismatch for %s", file.ID)
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	logParams := make([]telemetry.LogParams, 0)
	for {
		var event codexCostEvent
		err := decoder.Decode(&event)
		switch {
		case errors.Is(err, io.EOF):
			return logParams, nil
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "decode codex compliance cost log")
		case event.Type != codexComplianceCostsEventType:
			continue
		}

		logParam, ok, err := buildCodexCostEventLogParam(cfg, file, event)
		if err != nil {
			return nil, err
		}
		if ok {
			logParams = append(logParams, logParam)
		}
	}
}

func buildCodexCostEventLogParam(cfg Config, file codexapi.LogFile, event codexCostEvent) (telemetry.LogParams, bool, error) {
	timestamp, err := event.TimestampTime()
	if err != nil {
		return telemetry.LogParams{}, false, oops.E(oops.CodeUnexpected, err, "parse codex compliance cost timestamp")
	}
	if timestamp.IsZero() {
		timestamp = file.EndTime
	}
	if timestamp.IsZero() {
		return telemetry.LogParams{}, false, nil
	}

	userEmail := conv.NormalizeEmail(event.Payload.Identity.Email)
	usage := event.Payload.Measures.Usage
	totalTokens := usage.TextInputTokens + usage.TextCachedInputTokens + usage.TextOutputTokens
	totalCost, costUnit, billingSKUs := codexBillingSummary(event.Payload.Measures.Billing)

	attrs := map[attr.Key]any{
		attr.EventSourceKey:           string(telemetry.EventSourceAPI),
		attr.LogBodyKey:               "Codex cost metrics",
		attr.ProjectIDKey:             cfg.ProjectID.String(),
		attr.OrganizationIDKey:        cfg.OrganizationID,
		attr.ResourceURNKey:           codexUsageMetricsURN,
		attr.HookSourceKey:            codexHookSource,
		attr.ProviderKey:              codexProviderOpenAI,
		attr.GenAIProviderNameKey:     codexProviderOpenAI,
		attr.AIIntegrationConfigIDKey: cfg.ID.String(),
	}
	addStringAttr(attrs, attr.CodexComplianceEventIDKey, event.EventID)
	addStringAttr(attrs, attr.CodexComplianceLogIDKey, file.ID)
	addStringAttr(attrs, attr.CodexComplianceCostUnitKey, costUnit)
	addStringAttr(attrs, attr.CodexComplianceClientKey, event.Payload.Client)
	addStringAttr(attrs, attr.CodexComplianceSurfaceKey, event.Payload.Surface)
	addStringAttr(attrs, attr.CodexComplianceServiceTierKey, event.Payload.ServiceTier)
	addStringAttr(attrs, attr.CodexComplianceReasoningKey, event.Payload.Reasoning)
	addStringAttr(attrs, attr.CodexComplianceProductKey, event.Payload.Product)
	addStringAttr(attrs, attr.CodexComplianceBillingSKUsKey, strings.Join(billingSKUs, ","))
	addStringAttr(attrs, attr.ExternalUserIDKey, event.Payload.Identity.UserID)
	addStringAttr(attrs, attr.GenAIResponseModelKey, event.Payload.Model)
	if usage.TextInputTokens > 0 {
		attrs[attr.GenAIUsageInputTokensKey] = usage.TextInputTokens
	}
	if usage.TextCachedInputTokens > 0 {
		attrs[attr.GenAIUsageCacheReadInputTokensKey] = usage.TextCachedInputTokens
	}
	if usage.TextOutputTokens > 0 {
		attrs[attr.GenAIUsageOutputTokensKey] = usage.TextOutputTokens
	}
	if totalTokens > 0 {
		attrs[attr.GenAIUsageTotalTokensKey] = totalTokens
	}
	if totalCost > 0 {
		attrs[attr.GenAIUsageCostKey] = totalCost
	}

	return telemetry.LogParams{
		Timestamp: timestamp,
		ToolInfo: telemetry.ToolInfo{
			Name:           "codex",
			OrganizationID: cfg.OrganizationID,
			ProjectID:      cfg.ProjectID.String(),
			ID:             "",
			URN:            codexUsageMetricsURN,
			DeploymentID:   "",
			FunctionID:     nil,
		},
		UserInfo:   telemetry.UserInfoByEmail(userEmail),
		Attributes: attrs,
	}, true, nil
}

func (s *CodexCostImportService) writeCodexCostTelemetry(ctx context.Context, logParams []telemetry.LogParams) error {
	if len(logParams) == 0 {
		return nil
	}
	if err := s.telemetryLogger.LogBulk(ctx, logParams); err != nil {
		return oops.E(oops.CodeUnexpected, err, "insert codex cost telemetry logs")
	}
	return nil
}

func addStringAttr(attrs map[attr.Key]any, key attr.Key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		attrs[key] = value
	}
}

func codexBillingSummary(lines []codexCostBillingLine) (float64, string, []string) {
	total := float64(0)
	unit := ""
	skus := make([]string, 0, len(lines))
	for _, line := range lines {
		total += line.Cost.Value
		if line.Cost.Unit != "" {
			if unit == "" {
				unit = line.Cost.Unit
			} else if unit != line.Cost.Unit {
				unit = "mixed"
			}
		}
		if strings.TrimSpace(line.SKU) != "" {
			skus = append(skus, strings.TrimSpace(line.SKU))
		}
	}
	return total, unit, skus
}

type codexCostEvent struct {
	EventID   string           `json:"event_id"`
	Type      string           `json:"type"`
	Timestamp string           `json:"timestamp"`
	Payload   codexCostPayload `json:"payload"`
}

func (e codexCostEvent) TimestampTime() (time.Time, error) {
	if e.Timestamp == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, e.Timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp: %w", err)
	}
	return t.UTC(), nil
}

type codexCostPayload struct {
	Day            string            `json:"day"`
	Hour           int               `json:"hour"`
	OrganizationID string            `json:"organization_id"`
	Identity       codexCostIdentity `json:"identity"`
	Product        string            `json:"product"`
	Client         string            `json:"client"`
	Surface        string            `json:"surface"`
	Model          string            `json:"model"`
	ServiceTier    string            `json:"service_tier"`
	Reasoning      string            `json:"reasoning"`
	Measures       codexCostMeasures `json:"measures"`
}

type codexCostIdentity struct {
	UserID string   `json:"user_id"`
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	Groups []string `json:"groups"`
}

type codexCostMeasures struct {
	Usage   codexCostUsage         `json:"usage"`
	Billing []codexCostBillingLine `json:"billing"`
}

type codexCostUsage struct {
	TextInputTokens       int64 `json:"text_input_tokens"`
	TextCachedInputTokens int64 `json:"text_cached_input_tokens"`
	TextOutputTokens      int64 `json:"text_output_tokens"`
}

type codexCostBillingLine struct {
	SKU      string            `json:"sku"`
	Quantity codexCostQuantity `json:"quantity"`
	Cost     codexCostAmount   `json:"cost"`
}

type codexCostQuantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type codexCostAmount struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}
