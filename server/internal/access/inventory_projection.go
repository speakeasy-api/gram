package access

import (
	"context"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// inventoryProjection is how much of the Shadow AI section one caller may see.
//
// The same rows answer two questions. "What is this organization using, and
// what have we decided about it?" a project viewer needs. "Which people, on
// which machines?" is surveillance of named employees and stays with
// org:admin. Splitting the projection rather than the endpoint lets one read
// serve both.
type inventoryProjection struct {
	// Attributed is true for a caller holding org:admin, who sees user and
	// device counts, may filter by team, and may reach a named person.
	Attributed bool
}

// resolveAIInventoryProjection is the projection for the AI tools half. Its
// attributed tier re-checks live organization membership rather than trusting
// the session's grants — a stale grant must not open an organization-wide
// list of what named employees run. Such a session falls to the unattributed
// tier rather than being refused.
func (s *Service) resolveAIInventoryProjection(ctx context.Context, ac *contextvalues.AuthContext, projectID uuid.UUID) (inventoryProjection, error) {
	return s.resolveInventoryProjection(ctx, ac, projectID, func() error {
		return s.authz.RequireUserOrganizationScope(ctx, ac.ActiveOrganizationID, ac.UserID, authz.ScopeOrgAdmin)
	})
}

// resolveShadowMCPInventoryProjection is the projection for the MCP servers
// half, whose rows are project-scoped already. It proves the attributed tier
// the way every other shadow MCP endpoint does.
func (s *Service) resolveShadowMCPInventoryProjection(ctx context.Context, ac *contextvalues.AuthContext, projectID uuid.UUID) (inventoryProjection, error) {
	return s.resolveInventoryProjection(ctx, ac, projectID, func() error {
		return s.authz.Require(ctx, authz.Check{
			Scope:        authz.ScopeOrgAdmin,
			ResourceKind: "",
			ResourceID:   ac.ActiveOrganizationID,
			Dimensions:   nil,
		})
	})
}

// resolveInventoryProjection authorizes a Shadow AI inventory read and reports
// which projection the caller gets. Org admins are admitted without a
// projectID. attributed proves the upper tier and differs between halves.
//
// Support sessions are held to org:admin outright: the lower rung would widen
// what an impersonated session can see without any operator asking.
func (s *Service) resolveInventoryProjection(ctx context.Context, ac *contextvalues.AuthContext, projectID uuid.UUID, attributed func() error) (inventoryProjection, error) {
	if contextvalues.IsSupportSession(ctx) {
		if err := s.authz.Require(ctx, authz.Check{
			Scope:        authz.ScopeOrgAdmin,
			ResourceKind: "",
			ResourceID:   ac.ActiveOrganizationID,
			Dimensions:   nil,
		}); err != nil {
			return inventoryProjection{Attributed: false}, err
		}
		return inventoryProjection{Attributed: true}, nil
	}
	if err := attributed(); err == nil {
		return inventoryProjection{Attributed: true}, nil
	}

	if projectID == uuid.Nil {
		return inventoryProjection{Attributed: false}, oops.E(oops.CodeUnauthorized, nil, "a project is required to read the Shadow AI inventory without organization administrator access").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeProjectRead,
		ResourceKind: "",
		ResourceID:   projectID.String(),
		Dimensions:   nil,
	}); err != nil {
		return inventoryProjection{Attributed: false}, err
	}
	if err := s.requireProjectInOrganization(ctx, ac.ActiveOrganizationID, projectID); err != nil {
		return inventoryProjection{Attributed: false}, err
	}
	return inventoryProjection{Attributed: false}, nil
}

// redactAIDetectionAttribution drops the fields a project-read caller may not
// see. Every attribution-bearing field of gen.AIDetection must be listed here;
// adding one to the design without adding it here leaks it, which is what
// TestProjectReadProjectionCarriesNoAttribution exists to catch.
func redactAIDetectionAttribution(detections []*gen.AIDetection) {
	for _, detection := range detections {
		detection.UserCount = nil
		detection.DeviceCount = nil
		if detection.Access != nil {
			// Who decided and why names an administrator, not an employee,
			// but it is still personal and still not needed to render a
			// status column.
			detection.Access.DecidedBy = nil
			detection.Access.DecidedAt = nil
			detection.Access.Rationale = nil
		}
	}
}

// redactShadowMCPInventoryAttribution is the same rule for the MCP half of the
// section, so one status vocabulary does not come with two privacy postures.
func redactShadowMCPInventoryAttribution(servers []*gen.ShadowMCPInventoryServer) {
	for _, server := range servers {
		server.UserCount = nil
		server.TopUsers = nil
	}
}
