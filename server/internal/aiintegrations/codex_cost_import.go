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
	"net/http"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
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
	codexCreditValueUSD           = 0.04
)

type codexComplianceClient interface {
	ListLogs(ctx context.Context, params codexapi.ListLogsParams) (*codexapi.LogsPage, error)
	DownloadLog(ctx context.Context, principalID, logID string) ([]byte, error)
}

type CodexCostImportService struct {
	logger          *slog.Logger
	store           *Store
	guardianPolicy  *guardian.Policy
	telemetryLogger *telemetry.Logger
	heartbeat       func(ctx context.Context, page int)
}

func NewCodexCostImportService(logger *slog.Logger, store *Store, telemetryLogger *telemetry.Logger, guardianPolicy *guardian.Policy, heartbeat func(ctx context.Context, page int)) *CodexCostImportService {
	if heartbeat == nil {
		panic("codex cost import service requires heartbeat")
	}
	return &CodexCostImportService{
		logger:          logger.With(attr.SlogComponent("aiintegrations.codex_compliance")),
		store:           store,
		guardianPolicy:  guardianPolicy,
		telemetryLogger: telemetryLogger,
		heartbeat:       heartbeat,
	}
}

// SyncCodexCosts imports Codex cost compliance logs through the shared
// time-window poller. The source's upper-bound probe returns the latest
// finalized compliance log end_time, so empty polls leave the watermark
// untouched instead of skipping late-arriving log files.
func (s *CodexCostImportService) SyncCodexCosts(ctx context.Context, cfg Config, endTime time.Time) error {
	if cfg.Provider != ProviderCodexCompliance {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for codex cost import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == nil {
		return oops.E(oops.CodeInvalid, nil, "external_organization_id is required for codex_compliance")
	}

	progress := &CodexCostSyncProgress{
		WindowStart:       cfg.PollWatermarkAt,
		LogPages:          0,
		LogFiles:          0,
		CostEvents:        0,
		CostEventsWritten: 0,
		WatermarkReached:  cfg.PollWatermarkAt,
	}

	source := &codexCostSource{
		client:      codexapi.New(s.guardianPolicy, codexapi.WithAPIKey(cfg.APIKey)),
		cfg:         cfg,
		principalID: *cfg.ExternalOrganizationID,
		pageLimit:   codexCompliancePageLimit,
		processPage: s.telemetryLogger.LogBulk,
		progress:    progress,
	}

	runner := &timewindowpoller.Poller[[]codexapi.LogFile]{
		Store:    s.store,
		Schedule: ScheduleCodexCompliance,
		State: timewindowpoller.SyncState{
			SyncID:      cfg.SyncID,
			WatermarkAt: cfg.PollWatermarkAt,
			Checkpoint:  cfg.PollCheckpoint,
		},
		Source:  source,
		EndTime: endTime,
		Heartbeat: func(ctx context.Context, page int) {
			s.heartbeat(ctx, page)
		},
		InitialLookback: InitialUsagePollLookback,
		MaxWindow:       0,
		Granularity:     0,
		ResumeOffset:    0,
	}
	if err := runner.Do(ctx); err != nil {
		return newSyncError("sync codex costs", *progress,
			SyncStageError{Stage: "import_cost_logs", Err: err},
		)
	}
	return nil
}

type codexCostSource struct {
	client      codexComplianceClient
	cfg         Config
	principalID string
	pageLimit   int
	processPage func(ctx context.Context, payload []telemetry.LogParams) error
	progress    *CodexCostSyncProgress
}

func (src *codexCostSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
	after := codexUpperBoundStart(src.cfg, endTime)
	watermark := time.Time{}
	for {
		page, err := src.client.ListLogs(ctx, codexapi.ListLogsParams{
			PrincipalID: src.principalID,
			EventType:   codexComplianceCostsEventType,
			After:       after,
			Limit:       src.pageLimit,
		})
		if err != nil {
			return time.Time{}, err //nolint:wrapcheck // Preserve HTTPError classification upstream.
		}
		if page.LastEndTime.After(watermark) {
			watermark = page.LastEndTime
		}
		if !page.HasMore {
			if watermark.IsZero() {
				return after, nil
			}
			return watermark, nil
		}
		if page.LastEndTime.IsZero() {
			return time.Time{}, fmt.Errorf("codex compliance logs page had has_more without last_end_time")
		}
		if err := validateCodexLastEndTimeAdvanced(after, page.LastEndTime); err != nil {
			return time.Time{}, err
		}
		after = page.LastEndTime
	}
}

func (src *codexCostSource) FetchPage(ctx context.Context, start, end time.Time, pageToken string) (timewindowpoller.Page[[]codexapi.LogFile], error) {
	after := start
	if pageToken != "" {
		parsed, err := time.Parse(time.RFC3339Nano, pageToken)
		if err != nil {
			return timewindowpoller.Page[[]codexapi.LogFile]{Payload: nil, NextPage: "", HasMore: false}, fmt.Errorf("parse codex compliance page token: %w", err)
		}
		after = parsed
	}

	page, err := src.client.ListLogs(ctx, codexapi.ListLogsParams{
		PrincipalID: src.principalID,
		EventType:   codexComplianceCostsEventType,
		After:       after,
		Limit:       src.pageLimit,
	})
	if err != nil {
		return timewindowpoller.Page[[]codexapi.LogFile]{Payload: nil, NextPage: "", HasMore: false}, err //nolint:wrapcheck // Preserve HTTPError classification upstream.
	}
	src.progress.LogPages++
	if page.LastEndTime.After(src.progress.WatermarkReached) {
		src.progress.WatermarkReached = page.LastEndTime
	}

	files := make([]codexapi.LogFile, 0, len(page.Data))
	for _, file := range page.Data {
		if file.EventType != "" && file.EventType != codexComplianceCostsEventType {
			continue
		}
		if file.EndTime.After(end) {
			return timewindowpoller.Page[[]codexapi.LogFile]{Payload: files, NextPage: "", HasMore: false}, nil
		}
		files = append(files, file)
	}

	nextPage := ""
	if page.HasMore {
		if page.LastEndTime.IsZero() {
			return timewindowpoller.Page[[]codexapi.LogFile]{Payload: nil, NextPage: "", HasMore: false}, fmt.Errorf("codex compliance logs page had has_more without last_end_time")
		}
		if err := validateCodexLastEndTimeAdvanced(after, page.LastEndTime); err != nil {
			return timewindowpoller.Page[[]codexapi.LogFile]{Payload: nil, NextPage: "", HasMore: false}, err
		}
		nextPage = page.LastEndTime.UTC().Format(time.RFC3339Nano)
	}
	return timewindowpoller.Page[[]codexapi.LogFile]{
		Payload:  files,
		NextPage: nextPage,
		HasMore:  page.HasMore,
	}, nil
}

func (src *codexCostSource) ProcessPage(ctx context.Context, files []codexapi.LogFile) error {
	logParams := make([]telemetry.LogParams, 0)
	for _, file := range files {
		body, err := src.client.DownloadLog(ctx, src.principalID, file.ID)
		if err != nil {
			return err //nolint:wrapcheck // Preserve HTTPError classification upstream.
		}
		src.progress.LogFiles++

		params, err := buildCodexCostLogParams(src.cfg, file, body)
		if err != nil {
			return err
		}
		src.progress.CostEvents += len(params)
		logParams = append(logParams, params...)

		if file.EndTime.After(src.progress.WatermarkReached) {
			src.progress.WatermarkReached = file.EndTime
		}
	}

	if len(logParams) == 0 {
		return nil
	}
	if err := src.processPage(ctx, logParams); err != nil {
		return oops.E(oops.CodeUnexpected, err, "insert codex cost telemetry logs")
	}
	src.progress.CostEventsWritten += len(logParams)
	return nil
}

func (src *codexCostSource) RetryAfter(err error) (time.Duration, bool) {
	var httpErr *codexapi.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	return 0, true
}

func codexUpperBoundStart(cfg Config, endTime time.Time) time.Time {
	if cfg.PollCheckpoint.Partial() {
		return cfg.PollCheckpoint.Watermark
	}
	if !cfg.PollWatermarkAt.IsZero() {
		return cfg.PollWatermarkAt
	}
	return endTime.UTC().Add(-InitialUsagePollLookback)
}

func buildCodexCostLogParams(cfg Config, file codexapi.LogFile, body []byte) ([]telemetry.LogParams, error) {
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
	eventID := strings.TrimSpace(event.EventID)
	if eventID == "" {
		return telemetry.LogParams{}, false, oops.E(oops.CodeUnexpected, nil, "codex compliance cost event missing event_id")
	}

	timestamp, err := event.TimestampTime()
	if err != nil {
		return telemetry.LogParams{}, false, oops.E(oops.CodeUnexpected, err, "parse codex compliance cost timestamp")
	}
	if timestamp.IsZero() {
		timestamp = file.EndTime
	}
	if timestamp.IsZero() {
		var empty telemetry.LogParams
		return empty, false, nil
	}

	userEmail := conv.NormalizeEmail(event.Payload.Identity.Email)
	usage := event.Payload.Measures.Usage
	totalTokens := usage.TextInputTokens + usage.TextCachedInputTokens + usage.TextOutputTokens
	totalCostUSD, costUnit, billingSKUs := codexBillingSummary(event.Payload.Measures.Billing)

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
	addStringAttr(attrs, attr.CodexComplianceEventIDKey, eventID)
	addStringAttr(attrs, attr.CodexComplianceEventHashKey, generateCodexCostEventHash(eventID))
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
	if cfg.ExternalOrganizationID != nil {
		addStringAttr(attrs, attr.ExternalOrgIDKey, *cfg.ExternalOrganizationID)
	}
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
	if totalCostUSD > 0 {
		attrs[attr.GenAIUsageCostKey] = totalCostUSD
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

func validateCodexLastEndTimeAdvanced(after, lastEndTime time.Time) error {
	if lastEndTime.After(after) {
		return nil
	}
	return fmt.Errorf("codex compliance logs page last_end_time did not advance past after: last_end_time=%s after=%s",
		lastEndTime.UTC().Format(time.RFC3339Nano),
		after.UTC().Format(time.RFC3339Nano),
	)
}

func generateCodexCostEventHash(eventID string) string {
	return eventKey{
		"cost",
		strings.TrimSpace(eventID),
	}.hash()
}

func addStringAttr(attrs map[attr.Key]any, key attr.Key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		attrs[key] = value
	}
}

func codexBillingSummary(lines []codexCostBillingLine) (float64, string, []string) {
	totalUSD := float64(0)
	unit := ""
	skus := make([]string, 0, len(lines))
	for _, line := range lines {
		totalUSD += codexCostValueUSD(line.Cost)
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
	return totalUSD, unit, skus
}

func codexCostValueUSD(amount codexCostAmount) float64 {
	switch strings.ToUpper(strings.TrimSpace(amount.Unit)) {
	case "CREDITS":
		return amount.Value * codexCreditValueUSD
	case "USD":
		return amount.Value
	default:
		return 0
	}
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
