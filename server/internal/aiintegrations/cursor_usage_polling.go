package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	cursorUsageMetricsURN       = "cursor:usage:metrics"
	cursorHeartbeatInterval     = 10 * time.Second
	cursorUsageEventsBufferSize = 1500
)

type UsagePollService struct {
	users           *usersrepo.Queries
	apiClient       *cursorapi.Client
	telemetryLogger *telemetry.Logger
	heartbeat       func(ctx context.Context, page int)
}

func NewUsagePollService(db *pgxpool.Pool, telemetryLogger *telemetry.Logger, guardianPolicy *guardian.Policy, heartbeat func(ctx context.Context, page int)) *UsagePollService {
	if heartbeat == nil {
		panic("ai integration usage poll service requires heartbeat")
	}
	return &UsagePollService{
		users:           usersrepo.New(db),
		apiClient:       cursorapi.New(guardianPolicy),
		telemetryLogger: telemetryLogger,
		heartbeat:       heartbeat,
	}
}

func (s *UsagePollService) SyncCursorUsage(ctx context.Context, cfg Config, endTime time.Time) error {
	if cfg.Provider != ProviderCursor {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for usage polling: %s", cfg.Provider)
	}

	g, gctx := errgroup.WithContext(ctx)
	rawEvents := make(chan cursorapi.UsageEvent, cursorUsageEventsBufferSize)
	fetchErr := make(chan error, 1)

	// Cursor includes both time bounds, so advance past our stored inclusive watermark.
	startTime := cfg.PollWatermarkAt.Add(time.Millisecond)
	g.Go(func() (err error) {
		defer close(rawEvents)
		defer func() {
			fetchErr <- err
			close(fetchErr)
		}()

		for pageNum := 1; ; {
			s.heartbeat(gctx, pageNum)

			page, err := s.apiClient.FetchUsageEventsPage(gctx, cursorapi.FetchUsageEventsPageParams{
				Start: startTime,
				End:   endTime,
				Page:  pageNum,
			})
			if err != nil {
				var rateLimitErr *cursorapi.RateLimitError
				if errors.As(err, &rateLimitErr) {
					if err := s.sleep(gctx, calculateCursorRateLimitSleep(rateLimitErr.RetryAfter), pageNum); err != nil {
						return oops.E(oops.CodeUnexpected, err, "sleep after cursor rate limit")
					}
					continue
				}
				return oops.E(oops.CodeUnexpected, err, "fetch cursor usage events page")
			}

			for _, event := range page.Events {
				select {
				case <-gctx.Done():
					return gctx.Err()
				case rawEvents <- event:
				}
			}

			if !page.HasNextPage {
				return nil
			}
			pageNum++
		}
	})

	g.Go(func() error {
		logParams := make([]telemetry.LogParams, 0)
		userIDsByEmail := make(map[string]string)
		for event := range rawEvents {
			logParams = append(logParams, s.buildCursorUsageEvent(cfg, event))

			email := strings.ToLower(strings.TrimSpace(event.UserEmail))
			if email == "" {
				continue
			}
			if _, ok := userIDsByEmail[email]; ok {
				continue
			}
			userIDsByEmail[email] = ""
		}

		if err := <-fetchErr; err != nil {
			return err
		}

		emails := make([]string, 0, len(userIDsByEmail))
		for email := range userIDsByEmail {
			emails = append(emails, email)
		}

		users, err := s.users.GetConnectedUsersByEmails(gctx, usersrepo.GetConnectedUsersByEmailsParams{
			Emails:         emails,
			OrganizationID: cfg.OrganizationID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "internal error hydrating events")
		}

		for _, user := range users {
			userIDsByEmail[strings.ToLower(strings.TrimSpace(user.Email))] = user.ID
		}

		for _, logParam := range logParams {
			userEmail, _ := logParam.Attributes[attr.UserEmailKey].(string)
			if userID := userIDsByEmail[userEmail]; userID != "" {
				logParam.Attributes[attr.UserIDKey] = userID
			}
		}

		if err := s.writeCursorUsageTelemetry(gctx, logParams); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to write to clickhouse")
		}
		return nil
	})

	return g.Wait() //nolint:wrapcheck // Preserve the original goroutine error for callers.
}

func (s *UsagePollService) buildCursorUsageEvent(cfg Config, event cursorapi.UsageEvent) telemetry.LogParams {
	userEmail := strings.ToLower(strings.TrimSpace(event.UserEmail))

	return telemetry.LogParams{
		Timestamp: event.Timestamp,
		ToolInfo: telemetry.ToolInfo{
			Name:           "cursor",
			OrganizationID: cfg.OrganizationID,
			ProjectID:      cfg.ProjectID.String(),
			ID:             "",
			URN:            cursorUsageMetricsURN,
			DeploymentID:   "",
			FunctionID:     nil,
		},
		Attributes: map[attr.Key]any{
			attr.EventSourceKey:                        string(telemetry.EventSourceAPI),
			attr.LogBodyKey:                            "Cursor usage metrics",
			attr.ProjectIDKey:                          cfg.ProjectID.String(),
			attr.OrganizationIDKey:                     cfg.OrganizationID,
			attr.ResourceURNKey:                        cursorUsageMetricsURN,
			attr.HookSourceKey:                         string(telemetry.EventSourceHook),
			attr.GenAIUsageInputTokensKey:              event.TokenUsage.InputTokens,
			attr.GenAIUsageOutputTokensKey:             event.TokenUsage.OutputTokens,
			attr.GenAIUsageCacheReadInputTokensKey:     event.TokenUsage.CacheReadTokens,
			attr.GenAIUsageCacheCreationInputTokensKey: event.TokenUsage.CacheWriteTokens,
			attr.GenAIUsageCostKey:                     event.TokenUsage.TotalCents / 100,
			attr.GenAIResponseModelKey:                 event.Model,
			attr.UserEmailKey:                          userEmail,
			attr.CursorUsageEventHashKey:               generateCursorUsageEventHash(event),
			attr.CursorChargedCentsKey:                 event.ChargedCents,
		},
	}
}

func (s *UsagePollService) writeCursorUsageTelemetry(ctx context.Context, logParams []telemetry.LogParams) error {
	if len(logParams) == 0 {
		return nil
	}

	if err := s.telemetryLogger.LogBulk(ctx, logParams); err != nil {
		return oops.E(oops.CodeUnexpected, err, "insert telemetry logs")
	}
	return nil
}

func (s *UsagePollService) sleep(ctx context.Context, d time.Duration, page int) error {
	deadline := time.Now().Add(d)
	for remaining := time.Until(deadline); remaining > 0; remaining = time.Until(deadline) {
		s.heartbeat(ctx, page)
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
		case <-time.After(min(remaining, cursorHeartbeatInterval)):
		}
	}
	return nil
}

func calculateCursorRateLimitSleep(retryAfter time.Duration) time.Duration {
	if retryAfter <= 0 {
		retryAfter = time.Minute
	}

	jitter := time.Duration(time.Now().UnixNano() % int64(time.Second))
	return retryAfter + jitter
}

func generateCursorUsageEventHash(event cursorapi.UsageEvent) string {
	fields := []string{
		strconv.FormatInt(event.Timestamp.UTC().UnixMilli(), 10),
		strings.ToLower(strings.TrimSpace(event.UserEmail)),
		event.Model,
		event.Kind,
		strconv.FormatFloat(event.ChargedCents, 'f', -1, 64),
		strconv.FormatInt(event.TokenUsage.InputTokens, 10),
		strconv.FormatInt(event.TokenUsage.OutputTokens, 10),
		strconv.FormatInt(event.TokenUsage.CacheReadTokens, 10),
		strconv.FormatInt(event.TokenUsage.CacheWriteTokens, 10),
	}

	sum := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return hex.EncodeToString(sum[:])
}
