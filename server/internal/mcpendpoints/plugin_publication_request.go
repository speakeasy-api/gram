package mcpendpoints

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

// requestPublicationForMCPMembership only enqueues when an affected backend is
// distributed by a plugin. The probes share the endpoint mutation transaction.
func (s *Service) requestPublicationForMCPMembership(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, serverIDs []uuid.NullUUID, gatewayIDs ...uuid.NullUUID) error {
	if !s.publicationRequests.Enabled {
		return nil
	}
	attached, err := hasPluginMembershipForEndpoint(ctx, pluginsrepo.New(tx), *authCtx.ProjectID, serverIDs, gatewayIDs)
	if err != nil {
		return err
	}
	if !attached {
		return nil
	}
	if err := s.publicationRequests.Project(ctx, tx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID); err != nil {
		return fmt.Errorf("request plugin publication: %w", err)
	}
	return nil
}

// deliverEligibleGateway applies endpoint eligibility without changing authorization.
func (s *Service) deliverEligibleGateway(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, gatewayIDs ...uuid.NullUUID) *oops.ShareableError {
	// Endpoint writers already hold ingress and project admission locks. Deliver
	// newly usable gateways before probing membership for publication. Replays
	// preserve deleted membership, including an explicit manual removal.
	for _, gatewayID := range uniqueIDs(gatewayIDs...) {
		admissionCtx, err := admission.AdministratorWriteContext(ctx, s.authz, authCtx.ActiveOrganizationID)
		if err != nil {
			return gatewayDeliveryError(err)
		}
		if _, err := roledelivery.Eligible(admissionCtx, tx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, gatewayID, s.distributionAdmission); err != nil {
			return gatewayDeliveryError(err)
		}
	}
	return nil
}
