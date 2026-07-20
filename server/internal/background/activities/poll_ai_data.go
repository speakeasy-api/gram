package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
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

const PollAIDataInputPrefix = "urn:gram:ai-usage-poller:v1:"

func EncodePollAIDataInput(configID uuid.UUID, schedule string, endTime time.Time) string {
	return fmt.Sprintf("%s%s:%s:%d", PollAIDataInputPrefix, configID.String(), schedule, endTime.UTC().UnixNano())
}

func DecodePollAIDataInput(input string, workflowID string, fallbackEndTime time.Time) (PollAIDataInput, error) {
	if !strings.HasPrefix(input, PollAIDataInputPrefix) {
		if _, err := uuid.Parse(input); err != nil {
			return PollAIDataInput{}, fmt.Errorf("parse legacy poll ai data config id: %w", err)
		}
		schedule, err := legacyScheduleFromWorkflowID(workflowID)
		if err != nil {
			return PollAIDataInput{}, err
		}
		return PollAIDataInput{
			ConfigID: input,
			Schedule: schedule,
			EndTime:  fallbackEndTime.UTC(),
		}, nil
	}

	parts := strings.Split(strings.TrimPrefix(input, PollAIDataInputPrefix), ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return PollAIDataInput{}, fmt.Errorf("parse poll ai data input urn: invalid format")
	}
	if _, err := uuid.Parse(parts[0]); err != nil {
		return PollAIDataInput{}, fmt.Errorf("parse poll ai data input config id: %w", err)
	}
	if err := validatePollAIDataSchedule(parts[1]); err != nil {
		return PollAIDataInput{}, err
	}
	endTimeNanos, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return PollAIDataInput{}, fmt.Errorf("parse poll ai data input end time: %w", err)
	}
	return PollAIDataInput{
		ConfigID: parts[0],
		Schedule: parts[1],
		EndTime:  time.Unix(0, endTimeNanos).UTC(),
	}, nil
}

func legacyScheduleFromWorkflowID(workflowID string) (string, error) {
	parts := strings.Split(strings.TrimPrefix(workflowID, "v1:ai-usage-poller:"), ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", fmt.Errorf("parse legacy poll ai data workflow id: invalid format")
	}
	if _, err := uuid.Parse(parts[1]); err != nil {
		return "", fmt.Errorf("parse legacy poll ai data workflow id config id: %w", err)
	}
	if err := validatePollAIDataSchedule(parts[2]); err != nil {
		return "", err
	}
	return parts[2], nil
}

func validatePollAIDataSchedule(schedule string) error {
	switch schedule {
	case aiintegrations.ScheduleCursor, aiintegrations.ScheduleAnthropicCompliance, aiintegrations.ScheduleAnthropicAnalyticsUsage, aiintegrations.ScheduleAnthropicAnalyticsCost:
		return nil
	default:
		return fmt.Errorf("parse poll ai data input schedule: unsupported schedule %q", schedule)
	}
}

// Do runs exactly one sync schedule. Temporal gives every schedule its own
// workflow and retry budget, so a slow or failing schedule cannot block the
// other schedules attached to the same integration config.
func (p *PollAIData) Do(ctx context.Context, input string) (err error) {
	decoded, err := DecodePollAIDataInput(input, activity.GetInfo(ctx).WorkflowExecution.ID, time.Now().UTC())
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid ai integration usage poll input")
	}

	id, err := uuid.Parse(decoded.ConfigID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid ai integration config id")
	}
	if decoded.Schedule == "" {
		return oops.E(oops.CodeInvalid, nil, "ai integration sync schedule is required")
	}
	if decoded.EndTime.IsZero() {
		return oops.E(oops.CodeInvalid, nil, "ai integration sync end time is required")
	}

	endTime := decoded.EndTime.UTC()
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

			if recordErr := p.integrations.RecordSchedulePollFailure(recordCtx, id, decoded.Schedule, endTime, err); recordErr != nil {
				err = errors.Join(err, fmt.Errorf("record ai integration schedule failure: %w", recordErr))
			}
		}

		err = newPollFailureError(id, cfg.Provider, attempt, nonRetryable, err)
	}()

	cfg, err = p.integrations.GetUsagePollConfig(ctx, id, decoded.Schedule)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load ai integration configuration")
	}

	switch decoded.Schedule {
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
		if err := p.integrations.RecordSchedulePollSuccess(ctx, id, decoded.Schedule, endTime); err != nil {
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
		if err := p.integrations.RecordUsagePollSuccess(ctx, id, decoded.Schedule, endTime, nextCursor); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record anthropic compliance schedule success")
		}
	case aiintegrations.ScheduleAnthropicAnalyticsUsage:
		if err := p.analyticsUsagePoller.Sync(ctx, cfg, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "sync anthropic analytics usage")
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, id, decoded.Schedule, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record anthropic analytics usage success")
		}
	case aiintegrations.ScheduleAnthropicAnalyticsCost:
		if err := p.analyticsCostPoller.Sync(ctx, cfg, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "sync anthropic analytics cost")
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, id, decoded.Schedule, endTime); err != nil {
			return oops.E(oops.CodeUnexpected, err, "record anthropic analytics cost success")
		}
	default:
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration sync schedule: %s", decoded.Schedule)
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
