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
}

type ToolsetIndexTarget struct {
	ProjectID     uuid.UUID
	ToolsetSlug   types.Slug
	IndexRevision string
}

type ListToolsetsForIndexing struct {
	db *pgxpool.Pool
}

func NewListToolsetsForIndexing(db *pgxpool.Pool) *ListToolsetsForIndexing {
	return &ListToolsetsForIndexing{db: db}
}

func (a *ListToolsetsForIndexing) Do(
	ctx context.Context,
	input ListToolsetsForIndexingInput,
) ([]ToolsetIndexTarget, error) {
	rows, err := repo.New(a.db).ListToolsetsForIndexing(ctx, repo.ListToolsetsForIndexingParams{
		RotationSeed: input.RotationSeed,
		ScanLimit:    input.ScanLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list toolsets for indexing: %w", err)
	}

	targets := make([]ToolsetIndexTarget, len(rows))
	for i, row := range rows {
		targets[i] = ToolsetIndexTarget{
			ProjectID:     row.ProjectID,
			ToolsetSlug:   types.Slug(row.Slug),
			IndexRevision: fmt.Sprintf("%d:%s", row.ToolsetVersion, row.DeploymentID),
		}
	}

	return targets, nil
}
