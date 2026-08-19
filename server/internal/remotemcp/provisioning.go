package remotemcp

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const dashboardRemoteMCPInitialVisibility = "disabled"

// RemoteMCPProvisioningService owns the transactional source-plus-linked-server
// materialization shared by dashboard and future Platform MCP adapters. Surface
// policy is closed and selected by server composition, never by a request.
type RemoteMCPProvisioningService struct {
	db     *pgxpool.Pool
	policy *guardian.Policy
	audit  *audit.Logger
}

type DashboardRemoteMCPProvisioningInput struct {
	Name          *string
	URL           string
	TransportType string
}

type RemoteMCPProvisioningResult struct {
	RemoteMCPServer repo.RemoteMcpServer
	MCPServer       mcpserversrepo.McpServer
}

func NewRemoteMCPProvisioningService(db *pgxpool.Pool, policy *guardian.Policy, auditLogger *audit.Logger) *RemoteMCPProvisioningService {
	if auditLogger == nil {
		auditLogger = audit.NewLogger()
	}
	return &RemoteMCPProvisioningService{db: db, policy: policy, audit: auditLogger}
}

// ProvisionDashboardRemoteMCP preserves the dashboard workflow's disabled
// initial visibility while atomically creating the remote source, its linked
// MCP server, and the required user-session issuer. OAuth auto-configuration
// and best-effort endpoint creation remain outside this core transaction.
func (s *RemoteMCPProvisioningService) ProvisionDashboardRemoteMCP(ctx context.Context, authCtx *contextvalues.AuthContext, input DashboardRemoteMCPProvisioningInput) (RemoteMCPProvisioningResult, error) {
	if s == nil || s.db == nil || s.policy == nil || s.audit == nil || authCtx == nil || authCtx.ProjectID == nil || input.URL == "" || input.TransportType != "streamable-http" {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeBadRequest, nil, "invalid dashboard remote MCP provisioning input")
	}
	if _, err := s.policy.ValidateHTTPURL(ctx, input.URL); err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeBadRequest, err, "invalid url")
	}

	remoteID, err := uuid.NewV7()
	if err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "generate remote MCP server ID")
	}
	remoteSlug, err := conv.URLBackedSlug(input.URL, remoteID)
	if err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "compute remote MCP server slug")
	}
	remoteName := pgtype.Text{String: "", Valid: false}
	if input.Name != nil {
		if trimmed := strings.TrimSpace(*input.Name); trimmed != "" {
			remoteName = pgtype.Text{String: trimmed, Valid: true}
		}
	}
	displayName := strings.TrimPrefix(strings.TrimPrefix(input.URL, "https://"), "http://")
	if remoteName.Valid {
		displayName = remoteName.String
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "begin dashboard remote MCP provisioning")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	remote, err := repo.New(tx).CreateServer(ctx, repo.CreateServerParams{
		ID:            remoteID,
		ProjectID:     *authCtx.ProjectID,
		Name:          remoteName,
		Slug:          conv.ToPGText(remoteSlug),
		TransportType: input.TransportType,
		Url:           input.URL,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return RemoteMCPProvisioningResult{}, oops.E(oops.CodeConflict, err, "remote MCP server slug already in use")
		}
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "create remote MCP server")
	}
	if err := s.audit.LogRemoteMcpServerCreate(ctx, tx, audit.LogRemoteMcpServerCreateEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		RemoteMcpServerURN: urn.NewRemoteMcpServer(remote.ID),
		RemoteMcpServerURL: remote.Url,
	}); err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "log remote MCP server creation")
	}

	mcpServer, err := mcpservers.CreateRemoteBackedMCPServer(ctx, tx, s.audit, mcpservers.RemoteMCPMaterializationInput{
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
		ActorUserID:       authCtx.UserID,
		ActorEmail:        conv.PtrValOr(authCtx.Email, ""),
		RemoteMCPServerID: remote.ID,
		DisplayName:       displayName,
		InitialVisibility: dashboardRemoteMCPInitialVisibility,
	})
	if err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "materialize remote-backed MCP server")
	}
	if err := tx.Commit(ctx); err != nil {
		return RemoteMCPProvisioningResult{}, oops.E(oops.CodeUnexpected, err, "commit dashboard remote MCP provisioning")
	}

	return RemoteMCPProvisioningResult{RemoteMCPServer: remote, MCPServer: mcpServer}, nil
}
