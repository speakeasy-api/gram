package activities

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	cursorUsageMetricsURN     = "cursor:usage:metrics"
	cursorUsageWindowStartGap = time.Millisecond
	cursorHeartbeatInterval   = 10 * time.Second
	cursorRateLimitFallback   = time.Minute
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

func NewPollCursorUsageMetrics(logger *slog.Logger, db *pgxpool.Pool, encryptionClient *encryption.Client, telemetryLogger *telemetry.Logger) *PollCursorUsageMetrics {
	return &PollCursorUsageMetrics{
		logger:          logger.With(attr.SlogComponent("poll_cursor_usage_metrics")),
		db:              db,
		integrations:    aiintegrations.NewStore(logger, db, encryptionClient),
		apiClient:       cursorapi.New(),
		telemetryLogger: telemetryLogger,
	}
}

type AIIntegrationUsagePollConfig struct {
	ID             string
	OrganizationID string
	Provider       string
	ProjectID      string
	APIKey         string
	LastPolledAt   time.Time
	LeaseOwner     string
}

type CursorUsageEvent = cursorapi.UsageEvent

type ClaimAIIntegrationUsagePollsInput struct {
	Provider       string
	EndTime        time.Time
	Limit          int32
	LeaseOwner     string
	LeaseExpiresAt time.Time
}

type ReleaseAIIntegrationUsagePollLeaseInput struct {
	ConfigID   string
	LeaseOwner string
}

type SyncAIIntegrationUsageInput struct {
	Config  AIIntegrationUsagePollConfig
	EndTime time.Time
}

func (c *PollCursorUsageMetrics) ClaimAIIntegrationUsagePolls(ctx context.Context, input ClaimAIIntegrationUsagePollsInput) ([]AIIntegrationUsagePollConfig, error) {
	configs, err := c.integrations.ClaimUsagePolls(ctx, input.Provider, input.EndTime, input.Limit, input.LeaseOwner, input.LeaseExpiresAt)
	if err != nil {
		return nil, err
	}

	out := make([]AIIntegrationUsagePollConfig, 0, len(configs))
	for _, cfg := range configs {
		out = append(out, AIIntegrationUsagePollConfig{
			ID:             cfg.ID.String(),
			OrganizationID: cfg.OrganizationID,
			Provider:       cfg.Provider,
			ProjectID:      cfg.ProjectID.String(),
			APIKey:         cfg.APIKey,
			LastPolledAt:   cfg.LastPolledAt,
			LeaseOwner:     cfg.LeaseOwner,
		})
	}
	return out, nil
}

func (c *PollCursorUsageMetrics) ReleaseAIIntegrationUsagePollLease(ctx context.Context, input ReleaseAIIntegrationUsagePollLeaseInput) error {
	id, err := uuid.Parse(input.ConfigID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid ai integration config id")
	}
	return c.integrations.ReleaseUsagePollLease(ctx, id, input.LeaseOwner)
}

func (c *PollCursorUsageMetrics) SyncAIIntegrationUsage(ctx context.Context, input SyncAIIntegrationUsageInput) error {
	if input.Config.Provider != aiintegrations.ProviderCursor {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for usage polling: %s", input.Config.Provider)
	}

	events, err := c.fetchCursorUsageWindow(ctx, input.Config, input.EndTime)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.writeCursorUsageTelemetry(ctx, input.Config, events); err != nil {
		return err
	}

	id, err := uuid.Parse(input.Config.ID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid ai integration config id")
	}
	if err := c.integrations.UpdateUsagePollWatermark(ctx, id, input.Config.LeaseOwner, input.EndTime); err != nil {
		return err
	}
	return nil
}

func (c *PollCursorUsageMetrics) fetchCursorUsageWindow(ctx context.Context, cfg AIIntegrationUsagePollConfig, endTime time.Time) ([]CursorUsageEvent, error) {
	startTime := cursorUsageWindowStart(cfg.LastPolledAt)
	seen := make(map[string]struct{})
	events := make([]CursorUsageEvent, 0)
	for pageNum := 1; ; {
		activity.RecordHeartbeat(ctx, map[string]any{
			"config_id": cfg.ID,
			"provider":  cfg.Provider,
			"page":      pageNum,
		})
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		page, err := c.apiClient.FetchUsageEventsPage(ctx, cfg.APIKey, startTime, endTime, pageNum)
		if err != nil {
			var rateLimitErr *cursorapi.RateLimitError
			if errors.As(err, &rateLimitErr) {
				sleepFor := cursorRetryDelay(rateLimitErr.RetryAfter)
				c.logger.InfoContext(ctx, "cursor usage request rate limited",
					attr.SlogOrganizationID(cfg.OrganizationID),
					"page", pageNum,
					"retry_after", sleepFor.String(),
				)
				if err := sleepWithActivityHeartbeat(ctx, sleepFor); err != nil {
					return nil, err
				}
				continue
			}
			return nil, err
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

func (c *PollCursorUsageMetrics) writeCursorUsageTelemetry(ctx context.Context, cfg AIIntegrationUsagePollConfig, events []CursorUsageEvent) error {
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
			attr.EventSourceKey:                        "polling",
			attr.LogBodyKey:                            "Cursor usage metrics",
			attr.ProjectIDKey:                          cfg.ProjectID,
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
				ProjectID:      cfg.ProjectID,
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

func cursorUsageWindowStart(lastPolledAt time.Time) time.Time {
	// Cursor includes both time bounds, so advance past our stored inclusive watermark.
	return lastPolledAt.Add(cursorUsageWindowStartGap)
}

func cursorRetryDelay(retryAfter time.Duration) time.Duration {
	if retryAfter <= 0 {
		retryAfter = cursorRateLimitFallback
	}
	jitter := time.Duration(time.Now().UnixNano() % int64(time.Second))
	return retryAfter + jitter
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
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func hashCursorUsageEvent(event CursorUsageEvent) string {
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

func (c *PollCursorUsageMetrics) resolveUserIDsByEmail(ctx context.Context, orgID string, events []CursorUsageEvent) map[string]string {
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
