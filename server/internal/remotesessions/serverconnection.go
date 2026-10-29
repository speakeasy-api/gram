package remotesessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ServerConnectionTester checks a stored upstream with one dashboard user's
// credential. Implementations must never execute tools or return credentials.
type ServerConnectionTester interface {
	TestUserConnection(ctx context.Context, organizationID, userID string, projectID, remoteServerID, userSessionIssuerID uuid.UUID) (*gen.ServerConnectionTestResult, error)
}

// SetServerConnectionTester shares the runtime's authenticated readiness probe.
func (s *Service) SetServerConnectionTester(tester ServerConnectionTester) {
	s.connectionTester = tester
}

func (s *Service) TestServerConnection(ctx context.Context, payload *gen.TestServerConnectionPayload) (*gen.ServerConnectionTestResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.UserID == "" || !contextvalues.HasValidatedGramSession(ctx) {
		return nil, oops.E(oops.CodeUnauthorized, nil, "Sign in to Speakeasy before testing this connection.")
	}
	id, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "Invalid MCP server ID.")
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, id.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}
	target, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: id, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "MCP server not found.")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "Could not load the MCP server.").LogError(ctx, s.logger)
	}
	if !target.RemoteMcpServerID.Valid || !target.UserSessionIssuerID.Valid {
		return &gen.ServerConnectionTestResult{Status: "configuration_required", Code: "remote_user_identity_required", Message: "Configure User Identity for this Remote MCP server before testing.", ToolCount: nil}, nil
	}
	if s.connectionTester == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "Connection testing is unavailable. Try again later.")
	}
	result, err := s.connectionTester.TestUserConnection(ctx, authCtx.ActiveOrganizationID, authCtx.UserID, *authCtx.ProjectID, target.RemoteMcpServerID.UUID, target.UserSessionIssuerID.UUID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "Could not test the connection. Try again.").LogError(ctx, s.logger)
	}
	return result, nil
}
