package mv

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/oops"
	vr "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

// ToolVariationsGroupName returns the display name of a project-scoped tool
// variations group, or nil if the group row is no longer present (e.g.
// soft-deleted while still referenced by a toolset or mcp_server). Used by the
// listToolFilters endpoints to label the resolved group.
func ToolVariationsGroupName(ctx context.Context, logger *slog.Logger, tx DBTX, groupID, projectID uuid.UUID) (*string, error) {
	group, err := vr.New(tx).GetToolVariationsGroupByID(ctx, vr.GetToolVariationsGroupByIDParams{
		ID:        groupID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get tool variations group").LogError(ctx, logger)
	default:
		return &group.Name, nil
	}
}
