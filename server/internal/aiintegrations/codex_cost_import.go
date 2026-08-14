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
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
)

const (
	codexComplianceCostsEventType = "COSTS"
	codexCompliancePageLimit      = 100
	codexUsageMetricsURN          = "codex:usage:metrics"
	// chatgptUsageMetricsURN carries non-Codex COSTS events. The compliance
	// feed mixes product surfaces (observed values: "codex", "ChatGPT",
	// "Work"), and only true Codex rows may carry the codex:usage URN — the
	// codex.cost.usd stream and the ClickHouse agent-usage predicates match
	// on those prefixes. The summary MVs admit this URN too, so the product
	// split must survive summarization: hook_source is the persisted
	// dimension (chatgpt vs chatgpt-work below — the summaries have no
	// codex.compliance.product column and outlive the raw-row TTL).
	chatgptUsageMetricsURN = "chatgpt:usage:metrics"
	codexHookSource        = "codex"
	// chatgptHookSource tags ChatGPT chat rows and any unknown non-Codex
	// surface; chatgptWorkHookSource tags Work rows. Splitting at the
	// hook_source dimension keeps ChatGPT and Work separable in every
	// summary read without a schema change.
	chatgptHookSource     = "chatgpt"
	chatgptWorkHookSource = "chatgpt-work"
	// codexComplianceProductCodex / codexComplianceProductWork are the
	// payload.product values marking Codex and Work rows in COSTS events.
	codexComplianceProductCodex = "codex"
	codexComplianceProductWork  = "work"
	codexProviderOpenAI         = "openai"
	codexCreditValueUSD         = 0.04
)

// codexCloudMeteredClients are the payload.client values whose Codex usage
// executes in OpenAI's cloud and is therefore invisible to the OTEL stream —
// the compliance feed is their only token source, so those rows are metered
// here. Deliberately an allowlist: an unrecognized client defaults to
// un-metered, because under-counting a new surface beats double counting a
// device one. Every device client meters via OTEL and so must stay out of this
// map: cli, exec, and — settled under DNO-737 — desktop_app, which is Codex
// mode in the unified ChatGPT app and reports OTEL under the service name
// codex-app-server. Adding a device client here would double count it.
var codexCloudMeteredClients = map[string]bool{
	"github": true,
	"web":    true,
}

func codexCloudMeteredClient(client string) bool {
	return codexCloudMeteredClients[strings.ToLower(strings.TrimSpace(client))]
}

type codexComplianceClient interface {
	ListLogs(ctx context.Context, params codexapi.ListLogsParams) (*codexapi.LogsPage, error)
	DownloadLog(ctx context.Context, logID string) ([]byte, error)
}

// CodexCostLogDecodeError carries the provider payload that failed to decode
// for internal failure surfaces. Customer-facing error rendering must omit
// Payload because compliance events can contain user-identifying data.
type CodexCostLogDecodeError struct {
	// LogID identifies the compliance log file containing the payload.
	LogID string

	// Payload is the JSONL record that failed to decode.
	Payload []byte

	// Cause is the JSON decoder error.
	Cause error
}

func (e *CodexCostLogDecodeError) Error() string {
	return fmt.Sprintf("decode codex compliance cost log %s payload=%q: %v", e.LogID, e.Payload, e.Cause)
}

func (e *CodexCostLogDecodeError) Unwrap() error {
	return e.Cause
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
		return fmt.Errorf("unsupported ai integration provider for codex cost import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == nil {
		return fmt.Errorf("external_organization_id is required for codex_compliance")
	}

	progress := &CodexCostSyncProgress{
		WindowStart:       cfg.PollWatermarkAt,
		LogPages:          0,
		LogFiles:          0,
		CostEvents:        0,
		CostEventsWritten: 0,
		CostEventsDeduped: 0,
		WatermarkReached:  cfg.PollWatermarkAt,
	}

	source := &codexCostSource{
		client:    codexapi.New(s.guardianPolicy, *cfg.ExternalOrganizationID, codexapi.WithAPIKey(cfg.APIKey)),
		cfg:       cfg,
		pageLimit: codexCompliancePageLimit,
		processPage: func(ctx context.Context, params []telemetry.LogParams) (int, int, error) {
			return s.telemetryLogger.LogBulkDeduped(ctx, attr.CodexComplianceEventHashKey, params)
		},
		progress: progress,
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
		InitialLookback: codexComplianceInitialLookback,
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
	client    codexComplianceClient
	cfg       Config
	pageLimit int

	// processPage writes a page's cost events and returns how many it wrote and
	// how many it dropped as already ingested. Dropping is not incidental: the
	// compliance feed repeats an event_id across log files and telemetry_logs
	// has no uniqueness constraint, so an undeduped write counts a repeated
	// event once per file it appears in.
	processPage func(ctx context.Context, payload []telemetry.LogParams) (written int, dropped int, err error)

	progress *CodexCostSyncProgress
}

func (src *codexCostSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
	after := codexUpperBoundStart(src.cfg, endTime)
	watermark := time.Time{}
	for {
		page, err := src.client.ListLogs(ctx, codexapi.ListLogsParams{
			EventType: codexComplianceCostsEventType,
			After:     after,
			Limit:     src.pageLimit,
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
		EventType: codexComplianceCostsEventType,
		After:     after,
		Limit:     src.pageLimit,
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
		body, err := src.client.DownloadLog(ctx, file.ID)
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

	written, dropped, err := src.processPage(ctx, logParams)
	if err != nil {
		return fmt.Errorf("insert codex cost telemetry logs: %w", err)
	}
	src.progress.CostEventsDeduped += dropped
	src.progress.CostEventsWritten += written
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
	return endTime.UTC().Add(-codexComplianceInitialLookback)
}

func buildCodexCostLogParams(cfg Config, file codexapi.LogFile, body []byte) ([]telemetry.LogParams, error) {
	if file.FileSHA256 != "" {
		sum := sha256.Sum256(body)
		actual := hex.EncodeToString(sum[:])
		if !strings.EqualFold(actual, file.FileSHA256) {
			return nil, fmt.Errorf("codex compliance log sha256 mismatch for %s", file.ID)
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	logParams := make([]telemetry.LogParams, 0)
	for {
		var payload json.RawMessage
		err := decoder.Decode(&payload)
		if errors.Is(err, io.EOF) {
			return logParams, nil
		}
		if err != nil {
			return nil, &CodexCostLogDecodeError{
				LogID:   file.ID,
				Payload: codexCostPayloadAt(body, decoder.InputOffset()),
				Cause:   err,
			}
		}

		var event codexCostEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, &CodexCostLogDecodeError{
				LogID:   file.ID,
				Payload: payload,
				Cause:   err,
			}
		}
		if event.Type != codexComplianceCostsEventType {
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

func codexCostPayloadAt(body []byte, offset int64) []byte {
	if offset < 0 || offset >= int64(len(body)) {
		return nil
	}
	payload := bytes.TrimLeft(body[offset:], " \t\r\n")
	if newline := bytes.IndexByte(payload, '\n'); newline >= 0 {
		payload = payload[:newline]
	}
	return bytes.Clone(bytes.TrimSpace(payload))
}

func buildCodexCostEventLogParam(cfg Config, file codexapi.LogFile, event codexCostEvent) (telemetry.LogParams, bool, error) {
	eventID := strings.TrimSpace(event.EventID)
	if eventID == "" {
		return telemetry.LogParams{}, false, fmt.Errorf("codex compliance cost event missing event_id in log %s", file.ID)
	}

	timestamp, err := event.TimestampTime()
	if err != nil {
		return telemetry.LogParams{}, false, fmt.Errorf("parse codex compliance cost timestamp in log %s: %w", file.ID, err)
	}
	if timestamp.IsZero() {
		timestamp = codexCostBucketTime(event.Payload)
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

	// Route rows by product surface so the codex metric only counts Codex:
	// ChatGPT/Work (and any unknown surface) land under the chatgpt URN
	// instead of polluting codex:usage aggregates. Work additionally gets its
	// own hook_source so the ChatGPT/Work split survives summarization.
	product := strings.TrimSpace(event.Payload.Product)
	isCodex := strings.EqualFold(product, codexComplianceProductCodex)
	urn := codexUsageMetricsURN
	hookSource := codexHookSource
	if !isCodex {
		urn = chatgptUsageMetricsURN
		hookSource = chatgptHookSource
		if strings.EqualFold(product, codexComplianceProductWork) {
			hookSource = chatgptWorkHookSource
		}
	}

	attrs := map[attr.Key]any{
		attr.EventSourceKey:           string(telemetry.EventSourceAPI),
		attr.LogBodyKey:               "Codex cost metrics",
		attr.ProjectIDKey:             cfg.ProjectID.String(),
		attr.OrganizationIDKey:        cfg.OrganizationID,
		attr.ResourceURNKey:           urn,
		attr.HookSourceKey:            hookSource,
		attr.ProviderKey:              codexProviderOpenAI,
		attr.GenAIProviderNameKey:     codexProviderOpenAI,
		attr.AIIntegrationConfigIDKey: cfg.ID.String(),
		attr.AccountTypeKey:           complianceAccountTypeTeam,
	}
	// The config's admin-declared billing mode is stamped on Codex rows only —
	// the org-level tier of the billing-mode cascade, keyed here directly
	// since compliance rows have no session. ChatGPT/Work rows stay unlabeled
	// (estimate treatment): the single declaration cannot describe both
	// surfaces, and Codex credits vs ChatGPT seats routinely bill differently,
	// so labeling seat usage with a "metered" Codex declaration would render
	// token-priced estimates as confident real cost downstream.
	if isCodex {
		addStringAttr(attrs, attr.BillingModeKey, cfg.BillingMode)
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
	// Codex token metering is surface-partitioned (DNO-736/DNO-751). Device
	// surfaces (cli, exec) meter through the Codex OTEL stream — the token
	// source of truth — so their compliance rows stay cost-only: gen_ai.usage
	// counts here would double count for orgs that also export OTEL. Cloud
	// surfaces OTEL never sees (GitHub code review, web tasks) have tokens
	// ONLY in this feed, so their rows promote the counts to gen_ai.usage.*
	// and meter (and bill TUM) from here. The raw codex.compliance.* copies
	// (which nothing sums) ride on every Codex row regardless, so un-promoted
	// surfaces stay inspectable. Parked non-Codex rows keep gen_ai.usage
	// token counts because the compliance feed is ChatGPT/Work's only usage
	// source.
	if isCodex {
		if usage.TextInputTokens > 0 {
			attrs[attr.CodexComplianceInputTokensKey] = usage.TextInputTokens
		}
		if usage.TextCachedInputTokens > 0 {
			attrs[attr.CodexComplianceCachedInputTokensKey] = usage.TextCachedInputTokens
		}
		if usage.TextOutputTokens > 0 {
			attrs[attr.CodexComplianceOutputTokensKey] = usage.TextOutputTokens
		}
		if totalTokens > 0 {
			attrs[attr.CodexComplianceTotalTokensKey] = totalTokens
		}
	}
	if !isCodex || codexCloudMeteredClient(event.Payload.Client) {
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
	}
	if totalCostUSD > 0 {
		attrs[attr.GenAIUsageCostKey] = totalCostUSD
	}

	return telemetry.LogParams{
		Timestamp: timestamp,
		ToolInfo: telemetry.ToolInfo{
			Name:           hookSource,
			OrganizationID: cfg.OrganizationID,
			ProjectID:      cfg.ProjectID.String(),
			ID:             "",
			URN:            urn,
			DeploymentID:   "",
			FunctionID:     nil,
		},
		UserInfo:   telemetry.UserInfoByEmail(userEmail),
		Attributes: attrs,
	}, true, nil
}

// codexCostBucketTime derives an event time from the COSTS payload's own
// day/hour bucket, used when the event carries no timestamp of its own.
//
// It sits ahead of the log file's end time deliberately. The feed delivers the
// same event_id in more than one file, and a file-derived time would stamp
// those copies differently — putting the copy already ingested outside the
// event-time window the dedupe lookup searches, so the repeat would be written
// anyway. The day/hour bucket belongs to the event, so every delivery of it
// lands on the same timestamp.
func codexCostBucketTime(payload codexCostPayload) time.Time {
	day := strings.TrimSpace(payload.Day)
	if day == "" || payload.Hour < 0 || payload.Hour > 23 {
		return time.Time{}
	}
	parsed, err := time.ParseInLocation(time.DateOnly, day, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return parsed.Add(time.Duration(payload.Hour) * time.Hour)
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
