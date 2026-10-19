package telemetry

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// overviewCanonicalServerID resolves a hosted overview to its own server, not
// an alternate on the same toolset. Historical slugs without a live canonical
// row retain their existing slug-only scope.
func (s *Service) overviewCanonicalServerID(ctx context.Context, projectID uuid.UUID, slug string) (string, error) {
	toolset, err := toolsetsRepo.New(s.db).GetToolset(ctx, toolsetsRepo.GetToolsetParams{Slug: slug, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get overview toolset: %w", err)
	}
	canonical, err := mcpserversRepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversRepo.GetMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get overview canonical server: %w", err)
	}
	return canonical.ID.String(), nil
}
