package deployments

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
)

// AdmitExternalMCPSelection runs catalog admission for one external MCP
// selection against the deployment's persisted attachments.
func (s *Service) AdmitExternalMCPSelection(ctx context.Context, organizationID, organizationSlug string, registryID uuid.NullUUID, slug, registryServerSpecifier string, persisted []repo.ListDeploymentExternalMCPsRow, excluded []string) error {
	return s.admitExternalMCPSelections(ctx, organizationID, organizationSlug, []upsertExternalMCP{{
		registryID:              registryID,
		name:                    "",
		slug:                    slug,
		registryServerSpecifier: registryServerSpecifier,
		selectedRemotes:         nil,
	}}, persisted, excluded)
}
