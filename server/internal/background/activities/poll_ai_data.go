package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
	ConfigID     string `json:"config_id"`
	Provider     string `json:"provider,omitempty"`
	Attempt      int32  `json:"attempt"`
	MaxAttempts  int32  `json:"max_attempts"`
	NonRetryable bool   `json:"non_retryable"`
	// ProviderUnavailable marks failures caused by a provider outage
	// (persistent 429/5xx or transport errors): non-retryable within the
	// run because the schedule-level backoff owns the retry cadence, not
	// because the failure is permanent.
	ProviderUnavailable bool                        `json:"provider_unavailable,omitempty"`
	Stages              []stageFailureDetail        `json:"stages,omitempty"`
	Progress            aiintegrations.SyncProgress `json:"progress,omitempty"`
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
	chatgptConversationImporter   *aiintegrations.ChatGPTConversationImportService
	codexCloudImporter            *aiintegrations.CodexCloudImportService
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
	chatgptHeartbeat := func(ctx context.Context, page int) {
		activity.RecordHeartbeat(ctx, map[string]any{
			"schedule": aiintegrations.ScheduleChatGPTCompliance,
			"page":     page,
		})
	}
	codexCloudHeartbeat := func(ctx context.Context, page int) {
		activity.RecordHeartbeat(ctx, map[string]any{
			"schedule": aiintegrations.ScheduleCodexCloudSessions,
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
		chatgptConversationImporter:   aiintegrations.NewChatGPTConversationImportService(logger, store, db, guardianPolicy, chatWriter, chatgptHeartbeat),
		codexCloudImporter:            aiintegrations.NewCodexCloudImportService(logger, store, db, guardianPolicy, chatWriter, codexCloudHeartbeat),
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
		rejected := pollRejectedByProvider(err)
		// A multi-stage sync can carry both a rejection and an outage;
		// the rejection wins so the auto-pause path and the actionable
		// message stay in charge.
		unavailable := !rejected && pollProviderUnavailable(err)

		// Temporal records failures, but that's not visible to the user. We
		// record the failure on the last attempt — or immediately when
		// retrying within this run can't help: a rejected api key, or a
		// provider outage whose retry cadence the schedule-level backoff
		// already owns — so it's visible to the user in the dashboard.
		if attempt >= PollUsageMaxAttempts || rejected || unavailable {
			recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()

			// Provider rejections are permanent until the user fixes the
			// integration, so after a few of them in a row the schedule is
			// auto-paused instead of re-enqueued forever. Other failures —
			// including provider outages — never pause: the schedule keeps
			// polling, at a cadence that backs off with the failure streak.
			pauseAfter := conv.Ternary[int32](rejected, aiintegrations.AutoPauseAfterRejectedPolls, 0)
			shareableErr := shareablePollError(schedule, err)
			if recordErr := p.integrations.RecordSchedulePollFailure(recordCtx, cfg.ID, schedule, endTime, shareableErr, pauseAfter); recordErr != nil {
				err = errors.Join(err, fmt.Errorf("record ai integration schedule failure: %w", recordErr))
			}
		}

		err = newPollFailureError(cfg.ID, cfg.Provider, attempt, rejected, unavailable, err)
	}()

	switch schedule {
	case aiintegrations.ScheduleCursor:
		if cfg.Provider != aiintegrations.ProviderCursor {
			return oops.E(oops.CodeInvalid, nil, "cursor schedule cannot run for provider %s", cfg.Provider)
		}
		if err := p.cursorUsagePoller.SyncCursorUsage(ctx, cfg, endTime); err != nil {
			return fmt.Errorf("fetch cursor usage window: %w", err)
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, cfg.ID, schedule, endTime); err != nil {
			return fmt.Errorf("record cursor schedule success: %w", err)
		}
	case aiintegrations.ScheduleAnthropicCompliance:
		if cfg.Provider != aiintegrations.ProviderAnthropicCompliance {
			return oops.E(oops.CodeInvalid, nil, "anthropic compliance schedule cannot run for provider %s", cfg.Provider)
		}
		nextCursor, err := p.anthropicComplianceImporter.SyncAnthropicCompliance(ctx, cfg)
		if err != nil {
			return fmt.Errorf("sync anthropic compliance data: %w", err)
		}
		if err := p.integrations.RecordUsagePollSuccess(ctx, cfg.SyncID, schedule, endTime, nextCursor); err != nil {
			return fmt.Errorf("record anthropic compliance schedule success: %w", err)
		}
	case aiintegrations.ScheduleAnthropicAnalyticsUsage:
		if err := p.anthropicAnalyticsUsagePoller.Sync(ctx, cfg, endTime); err != nil {
			return fmt.Errorf("sync anthropic analytics usage: %w", err)
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, cfg.ID, schedule, endTime); err != nil {
			return fmt.Errorf("record anthropic analytics usage success: %w", err)
		}
	case aiintegrations.ScheduleAnthropicAnalyticsCost:
		if err := p.anthropicAnalyticsCostPoller.Sync(ctx, cfg, endTime); err != nil {
			return fmt.Errorf("sync anthropic analytics cost: %w", err)
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, cfg.ID, schedule, endTime); err != nil {
			return fmt.Errorf("record anthropic analytics cost success: %w", err)
		}
	case aiintegrations.ScheduleCodexCompliance:
		if cfg.Provider != aiintegrations.ProviderCodexCompliance {
			return oops.E(oops.CodeInvalid, nil, "codex compliance schedule cannot run for provider %s", cfg.Provider)
		}
		if err := p.codexCostImporter.SyncCodexCosts(ctx, cfg, endTime); err != nil {
			return fmt.Errorf("sync codex cost data: %w", err)
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, cfg.ID, schedule, endTime); err != nil {
			return fmt.Errorf("record codex compliance schedule success: %w", err)
		}
	case aiintegrations.ScheduleChatGPTCompliance:
		if cfg.Provider != aiintegrations.ProviderChatGPTCompliance {
			return oops.E(oops.CodeInvalid, nil, "chatgpt compliance schedule cannot run for provider %s", cfg.Provider)
		}
		if err := p.chatgptConversationImporter.SyncChatGPTConversations(ctx, cfg, endTime); err != nil {
			return fmt.Errorf("sync chatgpt conversation data: %w", err)
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, cfg.ID, schedule, endTime); err != nil {
			return fmt.Errorf("record chatgpt compliance schedule success: %w", err)
		}
	case aiintegrations.ScheduleCodexCloudSessions:
		if cfg.Provider != aiintegrations.ProviderChatGPTCompliance {
			return oops.E(oops.CodeInvalid, nil, "codex cloud sessions schedule cannot run for provider %s", cfg.Provider)
		}
		if err := p.codexCloudImporter.SyncCodexCloudSessions(ctx, cfg, endTime); err != nil {
			return fmt.Errorf("sync codex cloud session data: %w", err)
		}
		if err := p.integrations.RecordSchedulePollSuccess(ctx, cfg.ID, schedule, endTime); err != nil {
			return fmt.Errorf("record codex cloud sessions schedule success: %w", err)
		}
	default:
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration sync schedule: %s", schedule)
	}
	return nil
}

// shareablePollError turns an internal poll failure into the message persisted
// for organization members. Its ShareableError cause remains available to
// internal callers, but RecordSchedulePollFailure stores only Error().
func shareablePollError(schedule string, cause error) error {
	var contentErr *aiintegrations.CodexCostContentError
	if errors.As(cause, &contentErr) {
		return oops.E(oops.CodeUnexpected, cause, "%s", contentErr.ShareableMessage())
	}

	var cursorErr *cursorapi.HTTPError
	if errors.As(cause, &cursorErr) && cursorErr.StatusCode == 401 {
		return oops.E(oops.CodeUnauthorized, cause, "cursor rejected the configured api key")
	}

	var anthropicErr *anthropicapi.HTTPError
	if schedule == aiintegrations.ScheduleAnthropicCompliance && errors.As(cause, &anthropicErr) {
		switch anthropicErr.StatusCode {
		case 401, 403:
			return oops.E(oops.CodeUnauthorized, cause, "anthropic compliance rejected the configured api key")
		case 404:
			return oops.E(oops.CodeNotFound, cause, "anthropic compliance organization not found or compliance api access not enabled")
		}
	}

	var codexErr *codexapi.HTTPError
	if errors.As(cause, &codexErr) {
		switch schedule {
		case aiintegrations.ScheduleCodexCompliance:
			switch codexErr.StatusCode {
			case 401, 403:
				return oops.E(oops.CodeUnauthorized, cause, "codex compliance rejected the configured api key")
			case 404:
				return oops.E(oops.CodeNotFound, cause, "codex compliance organization not found or compliance api access not enabled")
			}
		case aiintegrations.ScheduleChatGPTCompliance:
			switch codexErr.StatusCode {
			case 401, 403:
				return oops.E(oops.CodeUnauthorized, cause, "chatgpt compliance rejected the configured api key")
			case 404:
				return oops.E(oops.CodeNotFound, cause, "chatgpt compliance workspace not found or compliance api access not enabled")
			}
		case aiintegrations.ScheduleCodexCloudSessions:
			switch codexErr.StatusCode {
			case 401, 403:
				return oops.E(oops.CodeUnauthorized, cause, "codex cloud import rejected the configured api key")
			case 404:
				return oops.E(oops.CodeNotFound, cause, "codex cloud workspace not found or compliance api access not enabled")
			}
		}
	}

	// A provider outage — a throttling or server status answered directly
	// or on the final attempt of a spent retry budget — is transient, not a
	// configuration problem. Say so, with the status when one was seen,
	// instead of the generic schedule message. Checked after the provider
	// rejection cases: a multi-stage sync can carry both a rejection and an
	// outage, and the rejection is the actionable one.
	if status := pollUnavailableHTTPStatus(cause); status != 0 {
		return oops.E(oops.CodeUnavailable, cause, "provider api is temporarily unavailable (HTTP %d); the sync will back off and retry", status)
	}
	var exhausted *guardian.RetriesExhaustedError
	if errors.As(cause, &exhausted) {
		return oops.E(oops.CodeUnavailable, cause, "provider api is unreachable; the sync will back off and retry")
	}

	// An interior oops boundary already chose a safe public message and code
	// (e.g. a schedule/provider mismatch or the telemetry insert wrap), so
	// persist that instead of falling back to the generic schedule message.
	var shareable *oops.ShareableError
	if errors.As(cause, &shareable) {
		return shareable
	}

	message := "ai integration sync failed"
	switch schedule {
	case aiintegrations.ScheduleCursor:
		message = "fetch cursor usage window"
	case aiintegrations.ScheduleAnthropicCompliance:
		message = "sync anthropic compliance data"
	case aiintegrations.ScheduleAnthropicAnalyticsUsage:
		message = "sync anthropic analytics usage"
	case aiintegrations.ScheduleAnthropicAnalyticsCost:
		message = "sync anthropic analytics cost"
	case aiintegrations.ScheduleCodexCompliance:
		message = "sync codex cost data"
	case aiintegrations.ScheduleChatGPTCompliance:
		message = "sync chatgpt conversation data"
	case aiintegrations.ScheduleCodexCloudSessions:
		message = "sync codex cloud session data"
	}
	return oops.E(oops.CodeUnexpected, cause, "%s", message)
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

// pollProviderUnavailable reports whether the poll failed because the
// provider was unavailable: the HTTP client's whole retry budget was spent
// on retryable statuses or transport errors, or a throttling/server status
// surfaced directly. Retrying inside the Temporal run cannot outlast an
// outage — the schedule-level failure backoff owns that cadence — so these
// skip the remaining activity attempts. Unlike a provider rejection, an
// outage ends on its own, so it never counts toward auto-pausing.
func pollProviderUnavailable(err error) bool {
	// A throttling or server status is affirmative outage evidence and is
	// checked first: a multi-stage sync can carry a timed-out sibling stage
	// next to the outage (newSyncError drops only Canceled stages), and the
	// sibling must not mask it.
	if pollUnavailableHTTPStatus(err) != 0 {
		return true
	}
	// Otherwise a canceled or expired activity context is our side giving
	// up, not the provider failing; those must keep the normal retry path.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var exhausted *guardian.RetriesExhaustedError
	if errors.As(err, &exhausted) {
		// Repeated transport failures with no response affirm an outage. A
		// single response-less attempt means the retry policy refused to
		// retry (TLS verification, redirect policy, resilience denial) —
		// not evidence the provider is down.
		return exhausted.StatusCode == 0 && exhausted.Attempts > 1
	}
	return false
}

// pollUnavailableHTTPStatus returns the throttling or server status a
// provider answered with — directly, via cursor's typed rate limit, or on
// the final attempt of a spent retry budget — or zero when the failure
// carries no such status. Shared by the outage classification and the
// org-visible message so the two cannot disagree about what counts as an
// outage.
func pollUnavailableHTTPStatus(err error) int {
	statusUnavailable := func(status int) bool {
		return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
	}
	var exhausted *guardian.RetriesExhaustedError
	if errors.As(err, &exhausted) && statusUnavailable(exhausted.StatusCode) {
		return exhausted.StatusCode
	}
	var rateLimited *cursorapi.RateLimitError
	if errors.As(err, &rateLimited) {
		return http.StatusTooManyRequests
	}
	var cursorErr *cursorapi.HTTPError
	if errors.As(err, &cursorErr) && statusUnavailable(cursorErr.StatusCode) {
		return cursorErr.StatusCode
	}
	var anthropicErr *anthropicapi.HTTPError
	if errors.As(err, &anthropicErr) && statusUnavailable(anthropicErr.StatusCode) {
		return anthropicErr.StatusCode
	}
	var codexErr *codexapi.HTTPError
	if errors.As(err, &codexErr) && statusUnavailable(codexErr.StatusCode) {
		return codexErr.StatusCode
	}
	return 0
}

// newPollFailureError wraps a poll failure in a typed Temporal application
// error. The details payload surfaces the provider, attempt count, per-stage
// failures, and run progress in the Temporal UI; the cause chain is kept
// intact for errors.Is/errors.As callers.
func newPollFailureError(configID uuid.UUID, provider string, attempt int32, rejected bool, providerUnavailable bool, cause error) error {
	nonRetryable := rejected || providerUnavailable
	details := aiUsagePollFailureDetails{
		ConfigID:            configID.String(),
		Provider:            provider,
		Attempt:             attempt,
		MaxAttempts:         PollUsageMaxAttempts,
		NonRetryable:        nonRetryable,
		ProviderUnavailable: providerUnavailable,
		Stages:              nil,
		Progress:            nil,
	}

	var syncErr *aiintegrations.SyncError
	if errors.As(cause, &syncErr) {
		details.Progress = syncErr.Progress
		details.Stages = make([]stageFailureDetail, 0, len(syncErr.Stages))
		for _, stage := range syncErr.Stages {
			details.Stages = append(details.Stages, stageFailureDetail{Stage: stage.Stage, Error: stage.Err.Error()})
		}
	}

	diagnostic := oops.Detail(cause)
	var contentErr *aiintegrations.CodexCostContentError
	if errors.As(cause, &contentErr) && len(contentErr.Payload) > 0 {
		diagnostic = fmt.Sprintf("%s payload=%q", diagnostic, contentErr.Payload[:min(len(contentErr.Payload), 4096)])
	}
	message := fmt.Sprintf("poll ai integration usage: provider=%s config=%s attempt=%d/%d: %s",
		provider, configID, attempt, PollUsageMaxAttempts, diagnostic)
	return temporal.NewApplicationErrorWithOptions(message, ErrTypeAIUsagePollFailed, temporal.ApplicationErrorOptions{
		NonRetryable: nonRetryable,
		Cause:        cause,
		Details:      []any{details},
	})
}
