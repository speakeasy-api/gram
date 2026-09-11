package activities

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
)

type ListToolsetsForIndexingInput struct {
	RotationSeed int64
	ScanLimit    int32
	ProjectIDs   []uuid.UUID
}

type ListProjectsForToolsetIndexingInput struct {
	RotationSeed int64
	ProjectLimit int32
}

type ToolsetIndexTarget struct {
	ProjectID      uuid.UUID
	ToolsetID      uuid.UUID
	ToolsetSlug    types.Slug
	ToolsetVersion int64
	DeploymentID   uuid.UUID
}

type ListToolsetsForIndexing struct {
	db *pgxpool.Pool
}

func NewListToolsetsForIndexing(db *pgxpool.Pool) *ListToolsetsForIndexing {
	return &ListToolsetsForIndexing{db: db}
}

func (a *ListToolsetsForIndexing) ListProjects(
	ctx context.Context,
	input ListProjectsForToolsetIndexingInput,
) ([]uuid.UUID, error) {
	projectIDs, err := repo.New(a.db).ListProjectsForToolsetIndexing(ctx, repo.ListProjectsForToolsetIndexingParams{
		RotationSeed: input.RotationSeed,
		ProjectLimit: input.ProjectLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list projects for toolset indexing: %w", err)
	}

	return projectIDs, nil
}

func (a *ListToolsetsForIndexing) Do(
	ctx context.Context,
	input ListToolsetsForIndexingInput,
) ([]ToolsetIndexTarget, error) {
	rows, err := repo.New(a.db).ListToolsetsForIndexing(ctx, repo.ListToolsetsForIndexingParams{
		RotationSeed: input.RotationSeed,
		ScanLimit:    input.ScanLimit,
		ProjectIds:   input.ProjectIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list toolsets for indexing: %w", err)
	}

	targets := make([]ToolsetIndexTarget, len(rows))
	for i, row := range rows {
		targets[i] = ToolsetIndexTarget{
			ProjectID:      row.ProjectID,
			ToolsetID:      row.ToolsetID,
			ToolsetSlug:    types.Slug(row.Slug),
			ToolsetVersion: row.ToolsetVersion,
			DeploymentID:   row.DeploymentID,
		}
	}

	return targets, nil
}
