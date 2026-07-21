package activities

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type ReconcileSkillObservationsParams struct {
	ProjectID uuid.UUID `json:"project_id"`
	BatchSize int32     `json:"batch_size"`
}

type ReconcileSkillObservationsResult = skills.ReconcileSkillObservationsResult

type ListPendingSkillObservationProjectsParams struct {
	AfterProjectID uuid.UUID `json:"after_project_id"`
	PageLimit      int32     `json:"page_limit"`
}

type SyncSkillSessionVersionsParams struct {
	ProjectID uuid.UUID `json:"project_id"`
	BatchSize int32     `json:"batch_size"`
}

type SyncSkillSessionVersionsResult struct {
	Processed int  `json:"processed"`
	HasMore   bool `json:"has_more"`
}

type SkillObservationReconciler struct {
	db            *pgxpool.Pool
	telemetryRepo *telemetryrepo.Queries
}

func NewSkillObservationReconciler(db *pgxpool.Pool, telemetryRepo *telemetryrepo.Queries) *SkillObservationReconciler {
	return &SkillObservationReconciler{db: db, telemetryRepo: telemetryRepo}
}

func (r *SkillObservationReconciler) SyncSessionVersions(ctx context.Context, params SyncSkillSessionVersionsParams) (*SyncSkillSessionVersionsResult, error) {
	queries := repo.New(r.db)
	rows, err := queries.ListPendingSkillSessionVersions(ctx, repo.ListPendingSkillSessionVersionsParams{
		ProjectID: params.ProjectID,
		BatchSize: params.BatchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list pending skill session versions: %w", err)
	}

	mappings := make([]telemetryrepo.SkillSessionVersion, len(rows))
	observationIDs := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		mappings[i] = telemetryrepo.SkillSessionVersion{
			ID:              row.ID,
			CreatedAt:       row.CreatedAt.Time,
			SeenAt:          row.SeenAt.Time,
			OrganizationID:  row.OrganizationID,
			ProjectID:       row.ProjectID,
			SessionID:       row.SessionID,
			SkillID:         row.SkillID,
			SkillVersionID:  row.SkillVersionID,
			CanonicalSHA256: row.CanonicalSha256,
			Surface:         row.Surface,
		}
		observationIDs[i] = row.ID
	}
	if err := r.telemetryRepo.InsertSkillSessionVersions(ctx, mappings); err != nil {
		return nil, fmt.Errorf("insert skill session versions: %w", err)
	}
	if len(observationIDs) > 0 {
		marked, err := queries.MarkSkillSessionVersionsSynced(ctx, repo.MarkSkillSessionVersionsSyncedParams{
			ProjectID:      params.ProjectID,
			ObservationIds: observationIDs,
		})
		if err != nil {
			return nil, fmt.Errorf("mark skill session versions synced: %w", err)
		}
		if marked != int64(len(observationIDs)) {
			return nil, fmt.Errorf("mark skill session versions synced: marked %d of %d observations", marked, len(observationIDs))
		}
	}

	return &SyncSkillSessionVersionsResult{
		Processed: len(rows),
		HasMore:   len(rows) == int(params.BatchSize),
	}, nil
}

func (r *SkillObservationReconciler) Reconcile(ctx context.Context, params ReconcileSkillObservationsParams) (*ReconcileSkillObservationsResult, error) {
	result, err := skills.ReconcileSkillObservations(ctx, r.db, params.ProjectID, params.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("reconcile skill observations: %w", err)
	}
	return result, nil
}

func (r *SkillObservationReconciler) ListProjects(ctx context.Context, params ListPendingSkillObservationProjectsParams) ([]uuid.UUID, error) {
	projects, err := repo.New(r.db).ListProjectsWithPendingSkillObservations(ctx, repo.ListProjectsWithPendingSkillObservationsParams{
		PageLimit:     params.PageLimit,
		ProjectCursor: params.AfterProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list projects with pending skill observations: %w", err)
	}
	return projects, nil
}
