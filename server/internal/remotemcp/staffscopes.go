package remotemcp

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// StaffScopes reads and writes MCP server scope pins for staff surfaces. It
// checks no customer grants, so callers must verify staff access and that the
// project belongs to the organization first. It does not evaluate the
// remote-session-live-resource-scopes rollout flag: every view is resolved as
// if discovery were on, and a pin can be set whether or not logins honor it yet.
type StaffScopes struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	audit  *audit.Logger
}

func NewStaffScopes(logger *slog.Logger, db *pgxpool.Pool, auditLogger *audit.Logger) *StaffScopes {
	return &StaffScopes{logger: logger.With(attr.SlogComponent("remotemcp_staff_scopes")), db: db, audit: auditLogger}
}

// StaffScopeTarget is one MCP server in a project the caller resolved inside
// the organization.
type StaffScopeTarget struct {
	OrganizationID string
	ProjectID      uuid.UUID
	McpServerID    uuid.UUID
}

// StaffScopePin is a pin write and the staff member making it.
type StaffScopePin struct {
	Target StaffScopeTarget

	// Scopes is the new pin; empty clears it.
	Scopes []string

	Actor            urn.Principal
	ActorDisplayName *string
}

// Describe returns the server's scope view. It fails with ErrNotRemoteBacked
// when no remote MCP server backs the server.
func (s *StaffScopes) Describe(ctx context.Context, target StaffScopeTarget) (*gen.RemoteMcpServerScopes, error) {
	logger := s.logger.With(attr.SlogProjectID(target.ProjectID.String()))
	server, err := loadPinnableServer(ctx, s.db, logger, target.ProjectID, target.McpServerID)
	if err != nil {
		return nil, err
	}
	sharing, err := repo.New(s.db).ListMcpServerIDsByRemoteURL(ctx, repo.ListMcpServerIDsByRemoteURLParams{ProjectID: target.ProjectID, Url: server.resourceURL})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers sharing the protected resource").LogError(ctx, logger)
	}
	return readServerScopes(ctx, s.db, logger, target.OrganizationID, target.ProjectID, server, sharing, true)
}

// SetPin writes the pin for every server sharing the target's upstream URL,
// audited as the staff actor, and returns the view as it stands afterwards.
func (s *StaffScopes) SetPin(ctx context.Context, pin StaffScopePin) (*gen.RemoteMcpServerScopes, error) {
	logger := s.logger.With(attr.SlogProjectID(pin.Target.ProjectID.String()))
	scopes, err := normalizePinnedScopes(pin.Scopes)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error())
	}
	server, err := loadPinnableServer(ctx, s.db, logger, pin.Target.ProjectID, pin.Target.McpServerID)
	if err != nil {
		return nil, err
	}
	sharing, err := repo.New(s.db).ListMcpServerIDsByRemoteURL(ctx, repo.ListMcpServerIDsByRemoteURLParams{ProjectID: pin.Target.ProjectID, Url: server.resourceURL})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers sharing the protected resource").LogError(ctx, logger)
	}
	return commitScopePin(ctx, s.db, s.audit, logger, nil, scopePinWrite{
		organizationID:   pin.Target.OrganizationID,
		projectID:        pin.Target.ProjectID,
		actor:            pin.Actor,
		actorDisplayName: pin.ActorDisplayName,
		target:           server,
		authorized:       sharing,
		scopes:           scopes,
	}, true)
}
