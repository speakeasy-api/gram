package mcpendpoints

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
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
