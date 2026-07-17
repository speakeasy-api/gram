package aiintegrations

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
)

type SyncScheduleBackfillStatus struct {
	PrimarySyncsPending   int64
	AnalyticsSyncsPending int64
}

type SyncScheduleBackfillBatch struct {
	ConfigsProcessed          int
	PrimarySyncsUpdated       int
	AnalyticsSchedulesCreated int
	LastConfigID              uuid.UUID
}

// SyncScheduleBackfiller performs the one-off, project-scoped data migration
// for primary ai_integration_syncs rows and missing Anthropic analytics
// schedules. Phase 3 removed the legacy config-only unique index, so secondary
// schedule creation is now safe.
type SyncScheduleBackfiller struct {
	db        repo.DBTX
	projectID uuid.UUID
}

func NewSyncScheduleBackfiller(db repo.DBTX, projectID uuid.UUID) *SyncScheduleBackfiller {
	return &SyncScheduleBackfiller{
		db:        db,
		projectID: projectID,
	}
}

func (b *SyncScheduleBackfiller) Status(ctx context.Context) (SyncScheduleBackfillStatus, error) {
	pending, err := repo.New(b.db).GetSyncScheduleBackfillStatus(ctx, b.projectID)
	if err != nil {
		return SyncScheduleBackfillStatus{}, fmt.Errorf("get sync schedule backfill status: %w", err)
	}

	return SyncScheduleBackfillStatus{
		PrimarySyncsPending:   pending.PrimarySyncsPending,
		AnalyticsSyncsPending: pending.AnalyticsSyncsPending,
	}, nil
}

func (b *SyncScheduleBackfiller) BackfillBatch(ctx context.Context, after uuid.UUID, limit int32) (SyncScheduleBackfillBatch, error) {
	if limit <= 0 {
		return SyncScheduleBackfillBatch{}, fmt.Errorf("backfill batch limit must be positive")
	}

	rows, err := repo.New(b.db).BackfillSyncSchedulesBatch(ctx, repo.BackfillSyncSchedulesBatchParams{
		ProjectID:     b.projectID,
		AfterConfigID: after,
		LimitCount:    limit,
	})
	if err != nil {
		return SyncScheduleBackfillBatch{}, fmt.Errorf("backfill sync schedule batch after %s: %w", after, err)
	}

	result := SyncScheduleBackfillBatch{
		ConfigsProcessed:          0,
		PrimarySyncsUpdated:       0,
		AnalyticsSchedulesCreated: 0,
		LastConfigID:              uuid.Nil,
	}
	for _, row := range rows {
		result.ConfigsProcessed++
		if row.UpdatedPrimary {
			result.PrimarySyncsUpdated++
		}
		result.AnalyticsSchedulesCreated += int(row.InsertedAnalytics)
		result.LastConfigID = row.AiIntegrationConfigID
	}
	return result, nil
}
