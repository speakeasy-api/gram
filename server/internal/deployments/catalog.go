package deployments

import (
	"context"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Admission applies only to new explicit selections, not accepted attachments
// carried by a clone. A matching slug alone does not preserve source identity.
func (s *Service) admitExternalMCPSelections(ctx context.Context, organizationID, organizationSlug string, upserts []upsertExternalMCP, persisted []repo.ListDeploymentExternalMCPsRow, excluded []string) error {
	for _, candidate := range upserts {
		if !candidate.registryID.Valid {
			continue
		}
		retained := slices.ContainsFunc(persisted, func(saved repo.ListDeploymentExternalMCPsRow) bool {
			return saved.Slug == candidate.slug && saved.RegistryID == candidate.registryID && saved.RegistryServerSpecifier == candidate.registryServerSpecifier && !slices.Contains(excluded, saved.Slug)
		})
		if retained {
			continue
		}
		if err := s.catalog.AdmitNewEntry(ctx, organizationID, organizationSlug, candidate.registryID.UUID, candidate.registryServerSpecifier); err != nil {
			return oops.E(oops.CodeBadRequest, err, "registry entry is not available for new catalog selections")
		}
	}
	return nil
}
