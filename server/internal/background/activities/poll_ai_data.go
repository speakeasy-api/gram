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

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
)

type PollAIData struct {
	integrations       *aiintegrations.Store
	cursorUsagePoller  *aiintegrations.UsagePollService
	complianceImporter *aiintegrations.ComplianceImportService
}

func NewPollAIData(
	logger *slog.Logger,
	db *pgxpool.Pool,
	encryptionClient *encryption.Client,
	telemetryLogger *telemetry.Logger,
	guardianPolicy *guardian.Policy,
	chatWriter *chat.ChatMessageWriter,
) *PollAIData {
	return &PollAIData{
		integrations: aiintegrations.NewStore(logger, db, encryptionClient),
		cursorUsagePoller: aiintegrations.NewUsagePollService(telemetryLogger, guardianPolicy, func(ctx context.Context, page int) {
			activity.RecordHeartbeat(ctx, map[string]any{
				"provider": aiintegrations.ProviderCursor,
				"page":     page,
			})
		}),
		complianceImporter: aiintegrations.NewComplianceImportService(logger, db, guardianPolicy, chatWriter, func(ctx context.Context, scope string, page int) {
			activity.RecordHeartbeat(ctx, map[string]any{
				"provider": aiintegrations.ProviderAnthropicCompliance,
				"scope":    scope,
				"page":     page,
			})
		}),
	}
}

// Do polls an AI integration provider and persists the provider-specific data.
// It records provider-visible failure state only on the final Temporal retry.
func (p *PollAIData) Do(ctx context.Context, configID string) (err error) {
	id, err := uuid.Parse(configID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid ai integration config id")
	}

	endTime := time.Now().UTC()
	// cfg is declared before the defer so the failure path can resolve the
	// provider-specific poll interval; if loading failed the provider is empty
	// and the default interval applies.
	var cfg aiintegrations.Config
	defer func() {
		if err == nil || activity.GetInfo(ctx).Attempt < PollUsageMaxAttempts {
			return
		}

		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		if recordErr := p.integrations.RecordUsagePollFailure(recordCtx, id, cfg.Provider, endTime, err); recordErr != nil {
			err = errors.Join(err, fmt.Errorf("record usage poll failure: %w", recordErr))
		}
	}()

	cfg, err = p.integrations.GetUsagePollConfig(ctx, id)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load ai integration configuration")
	}

	// Providers with cursor-based pagination advance lastCursor; time-window
	// providers leave the stored value untouched.
	lastCursor := cfg.LastCursor
	switch cfg.Provider {
	case aiintegrations.ProviderCursor:
		if err := p.cursorUsagePoller.SyncCursorUsage(ctx, cfg, endTime); err != nil {
			var httpErr *cursorapi.HTTPError
			if errors.As(err, &httpErr) && httpErr.StatusCode == 401 {
				return oops.E(oops.CodeUnauthorized, err, "cursor rejected the configured api key")
			}
			return oops.E(oops.CodeUnexpected, err, "fetch cursor usage window")
		}
	case aiintegrations.ProviderAnthropicCompliance:
		nextCursor, err := p.complianceImporter.SyncAnthropicCompliance(ctx, cfg)
		if err != nil {
			var httpErr *anthropicapi.HTTPError
			if errors.As(err, &httpErr) && (httpErr.StatusCode == 401 || httpErr.StatusCode == 403) {
				return oops.E(oops.CodeUnauthorized, err, "anthropic compliance rejected the configured api key")
			}
			return oops.E(oops.CodeUnexpected, err, "sync anthropic compliance data")
		}
		lastCursor = nextCursor
	default:
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for usage polling: %s", cfg.Provider)
	}

	if err := p.integrations.RecordUsagePollSuccess(ctx, id, cfg.Provider, endTime, lastCursor); err != nil {
		return oops.E(oops.CodeUnexpected, err, "record usage poll success")
	}
	return nil
}
