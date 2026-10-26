package organizations

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// GetGateway reports the active organization's gateway URL, or why it is not
// served, and the discovery mode it serves connections that do not choose
// one. The URL is under the organization's own host, which is where the
// gateway is served to its members.
func (s *Service) GetGateway(ctx context.Context, _ *gen.GetGatewayPayload) (*gen.OrganizationGateway, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization").LogError(ctx, s.logger)
	}
	return s.gatewayView(ctx, org)
}

// SetGatewayDiscoveryMode sets the discovery mode the active organization's
// gateway serves connections that do not choose one at sign-in. It governs
// the gateway the way turning it off does, so it takes the same org:admin.
// Code Mode follows the project gateway rule: it can be selected only while
// the organization has the entitlement and the runtime is available, and a
// gateway already in Code Mode keeps it after either goes away.
func (s *Service) SetGatewayDiscoveryMode(ctx context.Context, payload *gen.SetGatewayDiscoveryModePayload) (*gen.OrganizationGateway, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	mode := metamcp.DiscoveryMode(payload.DiscoveryMode)
	if !mode.Valid() {
		return nil, oops.E(oops.CodeBadRequest, nil, "unsupported discovery mode").LogWarn(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID))

	current, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization").LogError(ctx, logger)
	}
	// An unset default is stored when chosen explicitly, even as Progressive,
	// so a later default for unset organizations does not override it.
	if current.GatewayDiscoveryMode.Valid && current.GatewayDiscoveryMode.String == string(mode) {
		return s.gatewayView(ctx, current)
	}
	preflight := orggateway.DiscoveryMode(current)
	// Evaluate the Code Mode gate before taking the organization lock. Saving
	// the Code Mode default a gateway already has is not a new selection.
	if mode == metamcp.DiscoveryModeCode && preflight != metamcp.DiscoveryModeCode && !s.codeModeSelection.Allows(ctx, ac.ActiveOrganizationID) {
		return nil, oops.E(oops.CodeForbidden, nil, "organization gateway code mode is not available").LogWarn(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := orgrepo.New(dbtx)
	existing, err := txRepo.LockOrganizationForAdminConfiguration(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock organization").LogError(ctx, logger)
	}
	// Reject a changed baseline instead of using a gate evaluated for another mode.
	if existing.GatewayDiscoveryMode != current.GatewayDiscoveryMode {
		return nil, oops.E(oops.CodeConflict, nil, "organization gateway discovery mode changed concurrently; retry the update").LogWarn(ctx, logger)
	}

	updated, err := txRepo.SetOrganizationGatewayDiscoveryMode(ctx, orgrepo.SetOrganizationGatewayDiscoveryModeParams{
		GatewayDiscoveryMode: conv.ToPGText(string(mode)),
		ID:                   ac.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update organization gateway discovery mode").LogError(ctx, logger)
	}

	if err := s.audit.LogOrganizationGatewayDiscoveryModeUpdated(ctx, dbtx, audit.LogOrganizationGatewayDiscoveryModeUpdatedEvent{
		OrganizationID:                    ac.ActiveOrganizationID,
		Actor:                             urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:                  ac.Email,
		ActorSlug:                         nil,
		OrganizationName:                  updated.Name,
		OrganizationSlug:                  updated.Slug,
		OrganizationGatewaySnapshotBefore: &audit.OrganizationGatewaySnapshot{DiscoveryMode: string(preflight)},
		OrganizationGatewaySnapshotAfter:  &audit.OrganizationGatewaySnapshot{DiscoveryMode: string(mode)},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization gateway discovery mode update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return s.gatewayView(ctx, updated)
}

// gatewayView describes an organization's gateway. Its discovery mode and
// Code Mode capability are reported whether or not it is served, so an admin
// can choose the mode before turning the gateway on.
func (s *Service) gatewayView(ctx context.Context, org orgrepo.OrganizationMetadatum) (*gen.OrganizationGateway, error) {
	_, unavailable, err := orggateway.Availability(ctx, s.db, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve organization gateway").LogError(ctx, s.logger)
	}
	view := &gen.OrganizationGateway{
		Available:         false,
		URL:               nil,
		UnavailableReason: nil,
		DiscoveryMode:     string(orggateway.DiscoveryMode(org)),
		CodeModeEnabled:   s.codeModeSelection.Allows(ctx, org.ID),
	}
	if unavailable != orggateway.UnavailabilityNone {
		view.UnavailableReason = new(string(unavailable))
		return view, nil
	}
	view.Available = true
	view.URL = new(orggateway.URL(s.orgHosts.ServerURL(org.DefaultHost), org.Slug))
	return view, nil
}
