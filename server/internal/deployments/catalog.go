package deployments

import (
	"context"
	"errors"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Platform MCP parity: no new tool or schema is needed for deployment admission.
// Authorized callers already use register_catalog_mcp: RegistrationService inspects
// new selections before creating receipts and inspects retained identities on
// replay. ErrCatalogRejected is already an invalid_request refusal in tools.go, directing callers to read current state.
// The deployment guard closes the direct API path to the same selection rule;
// it does not change that registration outcome or its authorization/audiences.
// Evidence: platformmcp/catalog_retained_test.go and registration_service_test.go.
//
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
			if errors.Is(err, externalmcp.ErrCatalogSourceNotFound) || errors.Is(err, externalmcp.ErrCatalogSourceDisabled) || errors.Is(err, mcpregistry.ErrNotFound) {
				return oops.E(oops.CodeBadRequest, err, "registry entry is not available for new catalog selections")
			}
			return oops.E(oops.CodeUnexpected, err, "error checking catalog selection")
		}
	}
	return nil
}
