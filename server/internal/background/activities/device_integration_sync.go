package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

type GetDeviceIntegrationSyncCandidates struct {
	syncer *deviceintegrations.Syncer
}

func NewGetDeviceIntegrationSyncCandidates(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, encryptionClient *encryption.Client, guardianPolicy *guardian.Policy, features feature.Provider) *GetDeviceIntegrationSyncCandidates {
	return &GetDeviceIntegrationSyncCandidates{
		syncer: deviceintegrations.NewSyncer(logger, meterProvider, db, encryptionClient, guardianPolicy, features),
	}
}

type GetDeviceIntegrationSyncCandidatesInput struct {
	Limit int32
	// ExcludeSyncIDs are syncs already attempted this coordinator pass;
	// excluding them in the query keeps stuck syncs from occupying the LIMIT
	// window and starving later due candidates.
	ExcludeSyncIDs []uuid.UUID
}

// Due-ness is evaluated on the database clock inside the candidates query,
// so no timestamp crosses the Temporal/DB clock boundary.
func (c *GetDeviceIntegrationSyncCandidates) Do(ctx context.Context, input GetDeviceIntegrationSyncCandidatesInput) ([]deviceintegrations.SyncCandidate, error) {
	candidates, err := c.syncer.ListCandidates(ctx, input.Limit, input.ExcludeSyncIDs)
	if err != nil {
		return nil, fmt.Errorf("get device integration sync candidates: %w", err)
	}
	return candidates, nil
}

// RunDeviceIntegrationSync executes one sync. Its input is the sync id ONLY:
// Temporal persists every activity payload in workflow history, so
// credentials are decrypted inside the activity, never passed through it.
type RunDeviceIntegrationSync struct {
	syncer *deviceintegrations.Syncer
}

func NewRunDeviceIntegrationSync(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, encryptionClient *encryption.Client, guardianPolicy *guardian.Policy, features feature.Provider) *RunDeviceIntegrationSync {
	return &RunDeviceIntegrationSync{
		syncer: deviceintegrations.NewSyncer(logger, meterProvider, db, encryptionClient, guardianPolicy, features),
	}
}

func (r *RunDeviceIntegrationSync) Do(ctx context.Context, syncID string) error {
	id, err := uuid.Parse(syncID)
	if err != nil {
		return fmt.Errorf("parse device integration sync id: %w", err)
	}

	// Heartbeat for the duration of the run so the workflow's
	// HeartbeatTimeout can detect a dead attempt and cancel this context —
	// without it, a timed-out attempt's goroutine would keep writing
	// alongside its replacement.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx)
			}
		}
	}()

	if err := r.syncer.RunSync(ctx, id); err != nil {
		return fmt.Errorf("run device integration sync: %w", err)
	}
	return nil
}
