package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	codexapi "github.com/speakeasy-api/gram/server/internal/thirdparty/codex"
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
	integrations                  *aiintegrations.Store
	cursorUsagePoller             *aiintegrations.UsagePollService
	anthropicComplianceImporter   *aiintegrations.ComplianceImportService
	anthropicAnalyticsUsagePoller *aiintegrations.AnthropicAnalyticsPoller
	anthropicAnalyticsCostPoller  *aiintegrations.AnthropicAnalyticsPoller
	codexCostImporter             *aiintegrations.CodexCostImportService
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
	codexHeartbeat := func(ctx context.Context, page int) {
		activity.RecordHeartbeat(ctx, map[string]any{
			"schedule": aiintegrations.ScheduleCodexCompliance,
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
		anthropicComplianceImporter:   aiintegrations.NewComplianceImportService(logger, db, guardianPolicy, chatWriter, complianceHeartbeat),
		anthropicAnalyticsUsagePoller: aiintegrations.NewAnthropicUsageAnalyticsPoller(store, guardianPolicy, telemetryLogger, analyticsHeartbeat),
		anthropicAnalyticsCostPoller:  aiintegrations.NewAnthropicCostAnalyticsPoller(store, guardianPolicy, telemetryLogger, analyticsHeartbeat),
		codexCostImporter:             aiintegrations.NewCodexCostImportService(logger, store, telemetryLogger, guardianPolicy, codexHeartbeat),
	}
}

// Do runs exactly one sync schedule. Temporal gives every schedule its own
// workflow and retry budget, so a slow or failing schedule cannot block the
// other schedules attached to the same integration config.
func (p *PollAIData) Do(ctx context.Context, input string) (err error) {
	id, err := uuid.Parse(input)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid ai integration usage poll id")
	}

	cfg, schedule, err := p.integrations.GetUsagePollConfigBySyncID(ctx, id)
	if err != nil {
		var shareable *oops.ShareableError
		if errors.As(err, &shareable) && shareable.Code == oops.CodeNotFound {
			cfg, schedule, err = p.integrations.GetProviderUsagePollConfig(ctx, id)
		}
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load ai integration configuration")
	}
	if schedule == "" {
		return oops.E(oops.CodeInvalid, nil, "ai integration sync schedule is required")
	}

	endTime := time.Now().UTC()
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

			if recordErr := p.integrations.RecordSchedulePollFailure(recordCtx, cfg.ID, schedule, endTime, err); recordErr != nil {
				err = errors.Join(err, fmt.Errorf("record ai integration schedule failure: %w", recordErr))
			}
		}

		err = newPollFailureError(cfg.ID, cfg.Provider, attempt, nonRetryable, err)
	}()

	switch schedule {
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
		if err := p.integrations.RecordSchedulePollSuccess(ctx, cfg.ID, schedule, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record cursor schedule success")
		}
	case aiintegrations.ScheduleAnthropicCompliance:
		if cfg.Provider != aiintegrations.ProviderAnthropicCompliance {
			return oops.E(oops.CodeInvalid, nil, "anthropic compliance schedule cannot run for provider %s", cfg.Provider)
		}
		nextCursor, err := p.anthropicComplianceImporter.SyncAnthropicCompliance(ctx, cfg)
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
		if err := p.integrations.RecordUsagePollSuccess(ctx, cfg.SyncID, schedule, endTime, nextCursor); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record anthropic compliance schedule success")
		}
	case aiintegrations.ScheduleAnthropicAnalyticsUsage:
		if err := p.anthropicAnalyticsUsagePoller.Sync(ctx, cfg, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "sync anthropic analytics usage")
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, cfg.ID, schedule, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record anthropic analytics usage success")
		}
	case aiintegrations.ScheduleAnthropicAnalyticsCost:
		if err := p.anthropicAnalyticsCostPoller.Sync(ctx, cfg, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "sync anthropic analytics cost")
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, cfg.ID, schedule, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record anthropic analytics cost success")
		}
	case aiintegrations.ScheduleCodexCompliance:
		if cfg.Provider != aiintegrations.ProviderCodexCompliance {
			return oops.E(oops.CodeInvalid, nil, "codex compliance schedule cannot run for provider %s", cfg.Provider)
		}
		if err := p.codexCostImporter.SyncCodexCosts(ctx, cfg, endTime); err != nil {
			var httpErr *codexapi.HTTPError
			if errors.As(err, &httpErr) {
				switch httpErr.StatusCode {
				case 401, 403:
					return oops.E(oops.CodeUnauthorized, err, "codex compliance rejected the configured api key")
				case 404:
					return oops.E(oops.CodeNotFound, err, "codex compliance organization not found or compliance api access not enabled")
				}
			}
			return oops.E(oops.CodeUnexpected, err, "sync codex cost data")
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, cfg.ID, schedule, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record codex compliance schedule success")
		}
	default:
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration sync schedule: %s", schedule)
	}
	return nil
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
	var codexErr *codexapi.HTTPError
	if errors.As(err, &codexErr) {
		return codexErr.StatusCode == 401 || codexErr.StatusCode == 403 || codexErr.StatusCode == 404
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
