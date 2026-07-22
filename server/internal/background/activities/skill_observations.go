package activities

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
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

type SkillObservationReconciler struct {
	db *pgxpool.Pool
}

func NewSkillObservationReconciler(db *pgxpool.Pool) *SkillObservationReconciler {
	return &SkillObservationReconciler{db: db}
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
