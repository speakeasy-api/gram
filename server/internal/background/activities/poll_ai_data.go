package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
)

// ErrTypeAIUsagePollFailed is the Temporal application error type emitted
// when an AI integration poll fails. Its details payload carries the
// provider, per-stage failures, and run progress so the Temporal UI shows
// the whole failure story instead of a single opaque message.
const ErrTypeAIUsagePollFailed = "AIUsagePollFailed"

// aiUsagePollFailureDetails is the structured details payload attached to
// ErrTypeAIUsagePollFailed application errors.
type aiUsagePollFailureDetails struct {
	ConfigID     string                      `json:"config_id"`
	Provider     string                      `json:"provider,omitempty"`
	Attempt      int32                       `json:"attempt"`
	MaxAttempts  int32                       `json:"max_attempts"`
	NonRetryable bool                        `json:"non_retryable"`
	Stages       []stageFailureDetail        `json:"stages,omitempty"`
	Progress     aiintegrations.SyncProgress `json:"progress,omitempty"`
}

type stageFailureDetail struct {
	Stage string `json:"stage"`
	Error string `json:"error"`
}

type PollAIData struct {
	integrations         *aiintegrations.Store
	cursorUsagePoller    *aiintegrations.UsagePollService
	complianceImporter   *aiintegrations.ComplianceImportService
	analyticsUsagePoller *aiintegrations.AnthropicAnalyticsPoller
	analyticsCostPoller  *aiintegrations.AnthropicAnalyticsPoller
}

func NewPollAIData(
	logger *slog.Logger,
	db *pgxpool.Pool,
	encryptionClient *encryption.Client,
	telemetryLogger *telemetry.Logger,
	guardianPolicy *guardian.Policy,
	chatWriter *chat.ChatMessageWriter,
) *PollAIData {
	store := aiintegrations.NewStore(logger, db, encryptionClient)
	complianceHeartbeat := func(ctx context.Context, scope string, page int) {
		activity.RecordHeartbeat(ctx, map[string]any{
			"schedule": aiintegrations.ScheduleAnthropicCompliance,
			"scope":    scope,
			"page":     page,
		})
	}
	analyticsHeartbeat := func(ctx context.Context, schedule string, page int) {
		activity.RecordHeartbeat(ctx, map[string]any{
			"schedule": schedule,
			"page":     page,
		})
	}
	return &PollAIData{
		integrations: store,
		cursorUsagePoller: aiintegrations.NewUsagePollService(store, telemetryLogger, guardianPolicy, func(ctx context.Context, page int) {
			activity.RecordHeartbeat(ctx, map[string]any{
				"schedule": aiintegrations.ScheduleCursor,
				"page":     page,
			})
		}),
		complianceImporter:   aiintegrations.NewComplianceImportService(logger, db, guardianPolicy, chatWriter, complianceHeartbeat),
		analyticsUsagePoller: aiintegrations.NewAnthropicUsageAnalyticsPoller(store, guardianPolicy, telemetryLogger, analyticsHeartbeat),
		analyticsCostPoller:  aiintegrations.NewAnthropicCostAnalyticsPoller(store, guardianPolicy, telemetryLogger, analyticsHeartbeat),
	}
}

type PollAIDataInput struct {
	ConfigID string    `json:"config_id"`
	Schedule string    `json:"schedule"`
	EndTime  time.Time `json:"end_time"`
}

// UnmarshalJSON keeps activity tasks scheduled with the legacy scalar config
// ID readable after the activity input changed to this object.
func (i *PollAIDataInput) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var configID string
		if err := json.Unmarshal(trimmed, &configID); err != nil {
			return fmt.Errorf("decode legacy poll ai data input: %w", err)
		}
		*i = PollAIDataInput{
			ConfigID: configID,
			Schedule: "",
			EndTime:  time.Time{},
		}
		return nil
	}

	type input PollAIDataInput
	var decoded input
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decode poll ai data input: %w", err)
	}
	*i = PollAIDataInput(decoded)
	return nil
}

// Do runs exactly one sync schedule. Temporal gives every schedule its own
// workflow and retry budget, so a slow or failing schedule cannot block the
// other schedules attached to the same integration config.
func (p *PollAIData) Do(ctx context.Context, input PollAIDataInput) (err error) {
	input = normalizePollAIDataInput(input, activity.GetInfo(ctx).WorkflowExecution.ID, time.Now())

	id, err := uuid.Parse(input.ConfigID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid ai integration config id")
	}
	if input.Schedule == "" {
		return oops.E(oops.CodeInvalid, nil, "ai integration sync schedule is required")
	}
	if input.EndTime.IsZero() {
		return oops.E(oops.CodeInvalid, nil, "ai integration sync end time is required")
	}

	endTime := input.EndTime.UTC()
	var cfg aiintegrations.Config
	defer func() {
		if err == nil {
			return
		}

		attempt := activity.GetInfo(ctx).Attempt
		nonRetryable := pollRejectedByProvider(err)

		// Temporal records failures, but that's not visible to the user. We
		// record the failure on the last attempt — or immediately when
		// retrying can't help, e.g. a rejected api key — so it's visible to
		// the user in the dashboard.
		if attempt >= PollUsageMaxAttempts || nonRetryable {
			recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()

			if recordErr := p.integrations.RecordSchedulePollFailure(recordCtx, id, input.Schedule, endTime, err); recordErr != nil {
				err = errors.Join(err, fmt.Errorf("record ai integration schedule failure: %w", recordErr))
			}
		}

		err = newPollFailureError(id, cfg.Provider, attempt, nonRetryable, err)
	}()

	cfg, err = p.integrations.GetUsagePollConfig(ctx, id, input.Schedule)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load ai integration configuration")
	}

	switch input.Schedule {
	case aiintegrations.ScheduleCursor:
		if cfg.Provider != aiintegrations.ProviderCursor {
			return oops.E(oops.CodeInvalid, nil, "cursor schedule cannot run for provider %s", cfg.Provider)
		}
		if err := p.cursorUsagePoller.SyncCursorUsage(ctx, cfg, endTime); err != nil {
			var httpErr *cursorapi.HTTPError
			if errors.As(err, &httpErr) && httpErr.StatusCode == 401 {
				return oops.E(oops.CodeUnauthorized, err, "cursor rejected the configured api key")
			}
			return oops.E(oops.CodeUnexpected, err, "fetch cursor usage window")
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, id, input.Schedule, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record cursor schedule success")
		}
	case aiintegrations.ScheduleAnthropicCompliance:
		if cfg.Provider != aiintegrations.ProviderAnthropicCompliance {
			return oops.E(oops.CodeInvalid, nil, "anthropic compliance schedule cannot run for provider %s", cfg.Provider)
		}
		nextCursor, err := p.complianceImporter.SyncAnthropicCompliance(ctx, cfg)
		if err != nil {
			var httpErr *anthropicapi.HTTPError
			if errors.As(err, &httpErr) {
				switch httpErr.StatusCode {
				case 401, 403:
					return oops.E(oops.CodeUnauthorized, err, "anthropic compliance rejected the configured api key")
				case 404:
					return oops.E(oops.CodeNotFound, err, "anthropic compliance organization not found or compliance api access not enabled")
				}
			}
			return oops.E(oops.CodeUnexpected, err, "sync anthropic compliance data")
		}
		if err := p.integrations.RecordUsagePollSuccess(ctx, id, input.Schedule, endTime, nextCursor); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record anthropic compliance schedule success")
		}
	case aiintegrations.ScheduleAnthropicAnalyticsUsage:
		if err := p.analyticsUsagePoller.Sync(ctx, cfg, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "sync anthropic analytics usage")
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, id, input.Schedule, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record anthropic analytics usage success")
		}
	case aiintegrations.ScheduleAnthropicAnalyticsCost:
		if err := p.analyticsCostPoller.Sync(ctx, cfg, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "sync anthropic analytics cost")
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, id, input.Schedule, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record anthropic analytics cost success")
		}
	default:
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration sync schedule: %s", input.Schedule)
	}
	return nil
}

func normalizePollAIDataInput(input PollAIDataInput, workflowID string, now time.Time) PollAIDataInput {
	// Legacy workflow IDs ended in the provider, which equals the only
	// schedules that existed when PollAIData accepted a scalar input.
	if input.Schedule == "" {
		separator := strings.LastIndexByte(workflowID, ':')
		if separator >= 0 {
			switch schedule := workflowID[separator+1:]; schedule {
			case aiintegrations.ScheduleCursor, aiintegrations.ScheduleAnthropicCompliance:
				input.Schedule = schedule
			}
		}
	}
	if input.EndTime.IsZero() {
		input.EndTime = now.UTC()
	}
	return input
}

// pollRejectedByProvider reports whether the poll failed because the provider
// rejected the request in a way retrying can't fix: a rejected api key
// (401/403), or — for the anthropic compliance api — a 404, which is how it
// reports an unknown organization or one without compliance api access. Those
// failures are permanent until the user fixes the integration configuration,
// so retrying them is wasted work.
func pollRejectedByProvider(err error) bool {
	var cursorErr *cursorapi.HTTPError
	if errors.As(err, &cursorErr) {
		return cursorErr.StatusCode == 401
	}
	var anthropicErr *anthropicapi.HTTPError
	if errors.As(err, &anthropicErr) {
		return anthropicErr.StatusCode == 401 || anthropicErr.StatusCode == 403 || anthropicErr.StatusCode == 404
	}
	return false
}

// newPollFailureError wraps a poll failure in a typed Temporal application
// error. The details payload surfaces the provider, attempt count, per-stage
// failures, and run progress in the Temporal UI; the cause chain is kept
// intact for errors.Is/errors.As callers.
func newPollFailureError(configID uuid.UUID, provider string, attempt int32, nonRetryable bool, cause error) error {
	details := aiUsagePollFailureDetails{
		ConfigID:     configID.String(),
		Provider:     provider,
		Attempt:      attempt,
		MaxAttempts:  PollUsageMaxAttempts,
		NonRetryable: nonRetryable,
		Stages:       nil,
		Progress:     nil,
	}

	var syncErr *aiintegrations.SyncError
	if errors.As(cause, &syncErr) {
		details.Progress = syncErr.Progress
		details.Stages = make([]stageFailureDetail, 0, len(syncErr.Stages))
		for _, stage := range syncErr.Stages {
			details.Stages = append(details.Stages, stageFailureDetail{Stage: stage.Stage, Error: stage.Err.Error()})
		}
	}

	message := fmt.Sprintf("poll ai integration usage: provider=%s config=%s attempt=%d/%d: %s",
		provider, configID, attempt, PollUsageMaxAttempts, cause)
	return temporal.NewApplicationErrorWithOptions(message, ErrTypeAIUsagePollFailed, temporal.ApplicationErrorOptions{
		NonRetryable: nonRetryable,
		Cause:        cause,
		Details:      []any{details},
	})
}
