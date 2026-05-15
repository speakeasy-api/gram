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
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const cursorUsageMetricsURN = "cursor:usage:metrics"

var (
	cursorBillingKindKey = attr.Key("cursor.billing.kind")
	cursorMaxModeKey     = attr.Key("cursor.max_mode")
	cursorIsHeadlessKey  = attr.Key("cursor.is_headless")
	cursorEventHashKey   = attr.Key("cursor.event_hash")
)

type CursorUsageMetrics struct {
	logger          *slog.Logger
	db              *pgxpool.Pool
	integrations    *aiintegrations.Store
	apiClient       *cursorapi.Client
	telemetryLogger *telemetry.Logger
	telemetryRepo   *telemetryrepo.Queries
}

func NewCursorUsageMetrics(logger *slog.Logger, db *pgxpool.Pool, guardianPolicy *guardian.Policy, encryptionClient *encryption.Client, telemetryLogger *telemetry.Logger, telemetryRepo *telemetryrepo.Queries) *CursorUsageMetrics {
	return &CursorUsageMetrics{
		logger:          logger.With(attr.SlogComponent("cursor_usage_metrics")),
		db:              db,
		integrations:    aiintegrations.NewStore(logger, db, encryptionClient),
		apiClient:       cursorapi.New(guardianPolicy),
		telemetryLogger: telemetryLogger,
		telemetryRepo:   telemetryRepo,
	}
}

type CursorAIIntegrationConfig struct {
	ID             string
	OrganizationID string
	ProjectID      string
	APIKey         string
	LastPolledAt   time.Time
}

type CursorUsageEvent = cursorapi.UsageEvent

type PollCursorUsageEventsPageInput struct {
	Config  CursorAIIntegrationConfig
	EndTime time.Time
	Page    int
}

type PollCursorUsageEventsPageOutput struct {
	Events      []CursorUsageEvent
	HasNextPage bool
}

type DeduplicateAndWriteCursorEventsInput struct {
	Config  CursorAIIntegrationConfig
	EndTime time.Time
	Events  []CursorUsageEvent
}

type UpdateCursorPollWatermarkInput struct {
	ConfigID string
	At       time.Time
}

func (c *CursorUsageMetrics) ListCursorAIIntegrationConfigs(ctx context.Context) ([]CursorAIIntegrationConfig, error) {
	configs, err := c.integrations.ListEnabledConfigsByProvider(ctx, aiintegrations.ProviderCursor)
	if err != nil {
		return nil, fmt.Errorf("list enabled cursor ai integration configs: %w", err)
	}

	out := make([]CursorAIIntegrationConfig, 0, len(configs))
	for _, cfg := range configs {
		out = append(out, CursorAIIntegrationConfig{
			ID:             cfg.ID.String(),
			OrganizationID: cfg.OrganizationID,
			ProjectID:      cfg.ProjectID.String(),
			APIKey:         cfg.APIKey,
			LastPolledAt:   cfg.LastPolledAt,
		})
	}
	return out, nil
}

func (c *CursorUsageMetrics) PollCursorUsageEventsPage(ctx context.Context, input PollCursorUsageEventsPageInput) (*PollCursorUsageEventsPageOutput, error) {
	page, err := c.apiClient.FetchUsageEventsPage(ctx, input.Config.APIKey, input.Config.LastPolledAt, input.EndTime, input.Page)
	if err != nil {
		var rateLimitErr *cursorapi.RateLimitError
		if errors.As(err, &rateLimitErr) {
			return nil, temporal.NewApplicationErrorWithOptions("cursor usage request rate limited", "cursor_rate_limited", temporal.ApplicationErrorOptions{
				Cause:          err,
				NextRetryDelay: rateLimitErr.RetryAfter,
			})
		}
		return nil, fmt.Errorf("fetch cursor usage events page: %w", err)
	}
	return &PollCursorUsageEventsPageOutput{
		Events:      page.Events,
		HasNextPage: page.HasNextPage,
	}, nil
}

func (c *CursorUsageMetrics) DeduplicateAndWriteCursorEvents(ctx context.Context, input DeduplicateAndWriteCursorEventsInput) error {
	if len(input.Events) == 0 {
		return nil
	}

	hashes := make([]string, 0, len(input.Events))
	for _, event := range input.Events {
		hash := CursorEventHash(event)
		hashes = append(hashes, hash)
	}

	if c.telemetryRepo == nil {
		return oops.E(oops.CodeUnexpected, nil, "cursor usage metrics missing telemetry repo")
	}
	existingHashes, err := c.telemetryRepo.ListExistingCursorEventHashes(ctx, input.Config.ProjectID, input.Config.LastPolledAt.UnixNano(), input.EndTime.UnixNano(), hashes)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "query existing cursor usage event hashes")
	}
	existing := make(map[string]struct{}, len(existingHashes))
	for _, hash := range existingHashes {
		existing[hash] = struct{}{}
	}

	userIDsByEmail := c.resolveUserIDsByEmail(ctx, input.Config.OrganizationID, input.Events)
	for i, event := range input.Events {
		hash := hashes[i]
		if _, ok := existing[hash]; ok {
			continue
		}
		existing[hash] = struct{}{}

		timestamp, err := event.TimestampTime()
		if err != nil {
			return oops.E(oops.CodeInvalid, err, "parse cursor usage event timestamp")
		}

		userEmail := strings.ToLower(strings.TrimSpace(event.UserEmail))
		attrs := map[attr.Key]any{
			attr.EventSourceKey:                        "polling",
			attr.LogBodyKey:                            "Cursor usage metrics",
			attr.ProjectIDKey:                          input.Config.ProjectID,
			attr.OrganizationIDKey:                     input.Config.OrganizationID,
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
			cursorEventHashKey:                         hash,
		}
		if userID := userIDsByEmail[userEmail]; userID != "" {
			attrs[attr.UserIDKey] = userID
		}

		c.telemetryLogger.Log(ctx, telemetry.LogParams{
			Timestamp: timestamp,
			ToolInfo: telemetry.ToolInfo{
				Name:           "cursor",
				OrganizationID: input.Config.OrganizationID,
				ProjectID:      input.Config.ProjectID,
				ID:             "",
				URN:            cursorUsageMetricsURN,
				DeploymentID:   "",
				FunctionID:     nil,
			},
			Attributes: attrs,
		})
	}

	return nil
}

func (c *CursorUsageMetrics) UpdateCursorPollWatermark(ctx context.Context, input UpdateCursorPollWatermarkInput) error {
	id, err := uuid.Parse(input.ConfigID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid ai integration config id")
	}
	if err := c.integrations.UpdateSyncLastPolledAt(ctx, id, input.At); err != nil {
		return fmt.Errorf("update cursor poll watermark: %w", err)
	}
	return nil
}

func CursorEventHash(event CursorUsageEvent) string {
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

func (c *CursorUsageMetrics) resolveUserIDsByEmail(ctx context.Context, orgID string, events []CursorUsageEvent) map[string]string {
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
