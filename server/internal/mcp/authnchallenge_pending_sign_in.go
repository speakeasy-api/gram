package mcp

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// pendingSignInSetupStep is the first-party connect `setup` value a Platform
// MCP server card links with, so the consent page connects the server's
// sign-in provider before it lists sign-in options.
const pendingSignInSetupStep = "sign-in"

// PendingSignInConnector connects the sign-in provider of an MCP server that
// Speakeasy added from chat and has none yet. It does nothing for any other
// server, or when the user may not change the server.
type PendingSignInConnector interface {
	ConnectPendingSignIn(ctx context.Context, userID, organizationID string, projectID, mcpServerID uuid.UUID) error
}

func (s *Service) SetPendingSignInConnector(connector PendingSignInConnector) {
	s.pendingSignIn = connector
}

// connectPendingSignIn runs before the consent page lists its sign-in options,
// so a server card's Connect lands on the provider's sign-in instead of a page
// with nothing to connect.
func (s *Service) connectPendingSignIn(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	if !state.FirstParty || !state.ConnectPendingSignIn || s.pendingSignIn == nil || !endpoint.McpServerID.Valid {
		return nil
	}
	if state.Subject == nil || state.Subject.Kind != urn.SessionSubjectKindUser || state.Subject.ID == "" {
		return nil
	}
	if err := s.pendingSignIn.ConnectPendingSignIn(ctx, state.Subject.ID, endpoint.OrganizationID, endpoint.ProjectID, endpoint.McpServerID.UUID); err != nil {
		logger.ErrorContext(ctx, "connect pending sign-in provider", attr.SlogError(err))
		return oops.E(oops.CodeUnavailable, err, "Speakeasy could not connect this MCP server's sign-in. In the dashboard, open the MCP server's Settings and finish sign-in setup in its Authentication section.")
	}
	return nil
}
