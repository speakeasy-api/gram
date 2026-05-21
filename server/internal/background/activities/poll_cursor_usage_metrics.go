package activities

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	cursorUsageMetricsURN             = "cursor:usage:metrics"
	cursorHeartbeatInterval           = 10 * time.Second
	SyncAIIntegrationUsageMaxAttempts = 3
)

var (
	cursorBillingKindKey    = attr.Key("cursor.billing.kind")
	cursorMaxModeKey        = attr.Key("cursor.max_mode")
	cursorIsHeadlessKey     = attr.Key("cursor.is_headless")
	cursorUsageEventHashKey = attr.Key("cursor.event_hash")
	cursorChargedCentsKey   = attr.Key("cursor.charged_cents")
	cursorChargedUSDKey     = attr.Key("cursor.charged_usd")
)

type PollCursorUsageMetrics struct {
	logger          *slog.Logger
	db              *pgxpool.Pool
	integrations    *aiintegrations.Store
	apiClient       *cursorapi.Client
	telemetryLogger *telemetry.Logger
}

func NewPollCursorUsageMetrics(logger *slog.Logger, db *pgxpool.Pool, encryptionClient *encryption.Client, telemetryLogger *telemetry.Logger, guardianPolicy *guardian.Policy, tracerProvider trace.TracerProvider) *PollCursorUsageMetrics {
	return &PollCursorUsageMetrics{
		logger:          logger.With(attr.SlogComponent("poll_cursor_usage_metrics")),
		db:              db,
		integrations:    aiintegrations.NewStore(logger, db, encryptionClient),
		apiClient:       cursorapi.New(guardianPolicy),
		telemetryLogger: telemetryLogger,
	}
}

type SyncAIIntegrationUsageInput struct {
	ConfigID string
	EndTime  time.Time
}

func (c *PollCursorUsageMetrics) Do(ctx context.Context, input SyncAIIntegrationUsageInput) error {
	id, err := uuid.Parse(input.ConfigID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid ai integration config id")
	}

	cfg, err := c.integrations.GetUsagePollConfig(ctx, id)
	if err != nil {
		return fmt.Errorf("load ai integration usage poll config: %w", err)
	}
	if cfg.Provider != aiintegrations.ProviderCursor {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for usage polling: %s", cfg.Provider)
	}

	events, err := c.fetchUsageEvents(ctx, cfg, input.EndTime)
	if err != nil {
		return c.recordFailureAfterFinalAttempt(ctx, id, cfg, input.EndTime, fmt.Errorf("fetch cursor usage window: %w", err))
	}

	if err := ctx.Err(); err != nil {
		return c.recordFailureAfterFinalAttempt(ctx, id, cfg, input.EndTime, fmt.Errorf("cursor usage sync canceled before write: %w", err))
	}
	if err := c.writeCursorUsageTelemetry(ctx, cfg, events); err != nil {
		return c.recordFailureAfterFinalAttempt(ctx, id, cfg, input.EndTime, fmt.Errorf("write cursor usage telemetry: %w", err))
	}

	if err := c.integrations.RecordUsagePollSuccess(ctx, id, input.EndTime); err != nil {
		return c.recordFailureAfterFinalAttempt(ctx, id, cfg, input.EndTime, fmt.Errorf("record usage poll success: %w", err))
	}
	return nil
}

func (c *PollCursorUsageMetrics) recordFailureAfterFinalAttempt(ctx context.Context, configID uuid.UUID, cfg aiintegrations.Config, endTime time.Time, cause error) error {
	if activity.GetInfo(ctx).Attempt < SyncAIIntegrationUsageMaxAttempts {
		return cause
	}

	if err := c.integrations.RecordUsagePollFailure(ctx, configID, endTime, cause); err != nil {
		return fmt.Errorf("%w; record usage poll failure: %v", cause, err)
	}

	c.logger.WarnContext(ctx, "cursor usage sync failed after final attempt; recorded failure",
		attr.SlogError(cause),
		slog.String("ai_integration_config_id", configID.String()),
		attr.SlogOrganizationID(cfg.OrganizationID),
		attr.SlogProjectID(cfg.ProjectID.String()),
		slog.Time("next_poll_after", endTime.Add(time.Hour)),
	)
	return cause
}

func (c *PollCursorUsageMetrics) fetchUsageEvents(ctx context.Context, cfg aiintegrations.Config, endTime time.Time) ([]cursorapi.UsageEvent, error) {
	// Cursor includes both time bounds, so advance past our stored inclusive watermark.
	startTime := cfg.PollWatermarkAt.Add(time.Millisecond)
	seen := make(map[string]struct{})
	events := make([]cursorapi.UsageEvent, 0)
	for pageNum := 1; ; {
		activity.RecordHeartbeat(ctx, map[string]any{
			"config_id": cfg.ID.String(),
			"provider":  cfg.Provider,
			"page":      pageNum,
		})
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("cursor usage fetch canceled: %w", err)
		}

		page, err := c.apiClient.FetchUsageEventsPage(ctx, cfg.APIKey, startTime, endTime, pageNum)
		if err != nil {
			var rateLimitErr *cursorapi.RateLimitError
			if errors.As(err, &rateLimitErr) {
				retryAfter := rateLimitErr.RetryAfter
				if retryAfter <= 0 {
					retryAfter = time.Minute
				}
				sleepFor := retryAfter + time.Duration(time.Now().UnixNano()%int64(time.Second))
				c.logger.InfoContext(ctx, "cursor usage request rate limited",
					attr.SlogOrganizationID(cfg.OrganizationID),
					attr.SlogPaginationLimit(pageNum),
					attr.SlogRetryWait(sleepFor),
				)
				if err := sleepWithActivityHeartbeat(ctx, sleepFor); err != nil {
					return nil, fmt.Errorf("sleep after cursor rate limit: %w", err)
				}
				continue
			}
			return nil, fmt.Errorf("fetch cursor usage events page: %w", err)
		}

		for _, event := range page.Events {
			hash := hashCursorUsageEvent(event)
			if _, ok := seen[hash]; ok {
				continue
			}
			seen[hash] = struct{}{}
			events = append(events, event)
		}
		if !page.HasNextPage {
			return events, nil
		}
		pageNum++
	}
}

func (c *PollCursorUsageMetrics) writeCursorUsageTelemetry(ctx context.Context, cfg aiintegrations.Config, events []cursorapi.UsageEvent) error {
	if len(events) == 0 {
		return nil
	}

	userIDsByEmail := c.resolveUserIDsByEmail(ctx, cfg.OrganizationID, events)
	logParams := make([]telemetry.LogParams, 0, len(events))
	for _, event := range events {
		hash := hashCursorUsageEvent(event)
		timestamp, err := event.TimestampTime()
		if err != nil {
			return oops.E(oops.CodeInvalid, err, "parse cursor usage event timestamp")
		}
		userEmail := strings.ToLower(strings.TrimSpace(event.UserEmail))
		attrs := map[attr.Key]any{
			attr.EventSourceKey:                        string(telemetry.EventSourceAPI),
			attr.LogBodyKey:                            "Cursor usage metrics",
			attr.ProjectIDKey:                          cfg.ProjectID.String(),
			attr.OrganizationIDKey:                     cfg.OrganizationID,
			attr.ResourceURNKey:                        cursorUsageMetricsURN,
			attr.HookSourceKey:                         "cursor",
			attr.GenAIUsageInputTokensKey:              event.TokenUsage.InputTokens,
			attr.GenAIUsageOutputTokensKey:             event.TokenUsage.OutputTokens,
			attr.GenAIUsageCacheReadInputTokensKey:     event.TokenUsage.CacheReadTokens,
			attr.GenAIUsageCacheCreationInputTokensKey: event.TokenUsage.CacheWriteTokens,
			attr.GenAIUsageCostKey:                     event.TokenUsage.TotalCents / 100,
			attr.GenAIResponseModelKey:                 event.Model,
			attr.UserEmailKey:                          userEmail,
			cursorBillingKindKey:                       event.Kind,
			cursorMaxModeKey:                           event.MaxMode,
			cursorIsHeadlessKey:                        event.IsHeadless,
			cursorUsageEventHashKey:                    hash,
			cursorChargedCentsKey:                      event.ChargedCents,
			cursorChargedUSDKey:                        event.ChargedCents / 100,
		}
		if userID := userIDsByEmail[userEmail]; userID != "" {
			attrs[attr.UserIDKey] = userID
		}

		logParams = append(logParams, telemetry.LogParams{
			Timestamp: timestamp,
			ToolInfo: telemetry.ToolInfo{
				Name:           "cursor",
				OrganizationID: cfg.OrganizationID,
				ProjectID:      cfg.ProjectID.String(),
				ID:             "",
				URN:            cursorUsageMetricsURN,
				DeploymentID:   "",
				FunctionID:     nil,
			},
			Attributes: attrs,
		})
	}

	if err := c.telemetryLogger.LogBulk(ctx, logParams); err != nil {
		return oops.E(oops.CodeUnexpected, err, "insert cursor usage telemetry logs")
	}
	return nil
}

func sleepWithActivityHeartbeat(ctx context.Context, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil
		}
		waitFor := min(remaining, cursorHeartbeatInterval)
		activity.RecordHeartbeat(ctx)
		timer := time.NewTimer(waitFor)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return fmt.Errorf("cursor usage heartbeat sleep canceled: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func hashCursorUsageEvent(event cursorapi.UsageEvent) string {
	var b strings.Builder
	b.WriteString(event.Timestamp)
	b.WriteByte('|')
	b.WriteString(strings.ToLower(strings.TrimSpace(event.UserEmail)))
	b.WriteByte('|')
	b.WriteString(event.Model)
	b.WriteByte('|')
	b.WriteString(event.Kind)
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(event.ChargedCents, 'f', -1, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(event.TokenUsage.InputTokens, 10))
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(event.TokenUsage.OutputTokens, 10))
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(event.TokenUsage.CacheReadTokens, 10))
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(event.TokenUsage.CacheWriteTokens, 10))

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func (c *PollCursorUsageMetrics) resolveUserIDsByEmail(ctx context.Context, orgID string, events []cursorapi.UsageEvent) map[string]string {
	out := make(map[string]string)
	seen := make(map[string]struct{})
	emails := make([]string, 0, len(events))

	for _, event := range events {
		email := strings.ToLower(strings.TrimSpace(event.UserEmail))
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
	}

	if len(emails) == 0 {
		return out
	}

	users, err := usersrepo.New(c.db).GetConnectedUsersByEmails(ctx, usersrepo.GetConnectedUsersByEmailsParams{
		Emails:         emails,
		OrganizationID: orgID,
	})
	if err != nil {
		c.logger.WarnContext(ctx, "failed to resolve cursor usage users by email",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return out
	}

	for _, user := range users {
		email := strings.ToLower(strings.TrimSpace(user.Email))
		if email != "" {
			out[email] = user.ID
		}
	}

	return out
}
