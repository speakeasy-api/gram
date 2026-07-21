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
	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
)

const (
	PollUsageMaxAttempts = 5
)

type PollCursorUsageMetrics struct {
	integrations    *aiintegrations.Store
	telemetryLogger *telemetry.Logger
	guardianPolicy  *guardian.Policy
}

func NewPollCursorUsageMetrics(
	logger *slog.Logger,
	db *pgxpool.Pool,
	encryptionClient *encryption.Client,
	telemetryLogger *telemetry.Logger,
	guardianPolicy *guardian.Policy,
) *PollCursorUsageMetrics {
	store := aiintegrations.NewStore(logger, db, encryptionClient)
	return &PollCursorUsageMetrics{
		integrations:    store,
		telemetryLogger: telemetryLogger,
		guardianPolicy:  guardianPolicy,
	}
}

// Do polls cursor for all usage metrics within the given time window (usually one hour)
// and writes everything at once to the telemetry logs. Duplicates are safe since
// we save the hash of the event in the telemetry logs.
func (p *PollCursorUsageMetrics) Do(ctx context.Context, configID string) (err error) {
	id, err := uuid.Parse(configID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid ai integration config id")
	}

	endTime := time.Now().UTC()
	// temporal records failures, but that's not visible to the user
	// we need to record the failure here so that it's visible to the user
	// especially when they might have provided a bad api key
	defer func() {
		if err == nil || activity.GetInfo(ctx).Attempt < PollUsageMaxAttempts {
			return
		}

		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		if recordErr := p.integrations.RecordUsagePollFailure(recordCtx, id, aiintegrations.ProviderCursor, endTime, err); recordErr != nil {
			err = errors.Join(err, fmt.Errorf("record usage poll failure: %w", recordErr))
		}
	}()

	cfg, err := p.integrations.GetUsagePollConfig(ctx, id, aiintegrations.ScheduleCursor)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load ai integration configuration")
	}

	if cfg.Provider != aiintegrations.ProviderCursor {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for usage polling: %s", cfg.Provider)
	}

	source, err := aiintegrations.NewCursorUsageSource(p.guardianPolicy, cfg, p.telemetryLogger.LogBulk, "", 0)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build cursor usage source")
	}
	poller := &timewindowpoller.Poller[[]telemetry.LogParams]{
		Store:    p.integrations,
		Schedule: aiintegrations.ScheduleCursor,
		State: timewindowpoller.SyncState{
			SyncID:      cfg.SyncID,
			WatermarkAt: cfg.PollWatermarkAt,
			Checkpoint:  cfg.PollCheckpoint,
		},
		Source:  source,
		EndTime: endTime,
		Heartbeat: func(ctx context.Context, page int) {
			activity.RecordHeartbeat(ctx, map[string]any{
				"page": page,
			})
		},
		InitialLookback: aiintegrations.InitialUsagePollLookback,
		MaxWindow:       0,
		Granularity:     0,
		ResumeOffset:    time.Millisecond,
	}
	if err := poller.Do(ctx); err != nil {
		var httpErr *cursorapi.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 401 {
			return oops.E(oops.CodeUnauthorized, err, "cursor rejected the configured api key")
		}
		return oops.E(oops.CodeUnexpected, err, "fetch cursor usage window")
	}

	if err := p.integrations.RecordUsagePollSuccess(ctx, id, aiintegrations.ProviderCursor, endTime, cfg.LastCursor); err != nil {
		return oops.E(oops.CodeUnexpected, err, "record usage poll success")
	}

	return nil
}
