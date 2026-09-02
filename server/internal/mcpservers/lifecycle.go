package mcpservers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LifecycleUpdateInput describes the narrow MCP-server fields shared by
// dashboard management and Platform MCP lifecycle operations. Backend references
// are deliberately inherited from the locked server; callers cannot use this
// command to rewire a server.
type LifecycleUpdateInput struct {
	OrganizationID    string
	ProjectID         uuid.UUID
	ActorUserID       string
	ActorEmail        *string
	ServerID          uuid.UUID
	Name              *string
	Visibility        string
	NetworkAccessMode *networkaccess.Mode

	EnvironmentID         uuid.NullUUID
	UserSessionIssuerID   uuid.NullUUID
	RemoteMcpServerID     uuid.NullUUID
	TunneledMcpServerID   uuid.NullUUID
	ToolsetID             uuid.NullUUID
	UnproxiedMcpServerID  uuid.NullUUID
	ToolVariationsGroupID uuid.NullUUID
}

const maxLifecycleMCPServerNameBytes = 256

// MCPServerVisibilityResult reports only the post-update server and root domains
// that need reconciliation after the caller commits its transaction.
type MCPServerVisibilityResult struct {
	Server               repo.McpServer
	ClearedRootDomainIDs []uuid.UUID
}

// LockMCPServerVisibilityDependencies preserves the domain -> root endpoint ->
// MCP-server lock order used by deletion and root-selection paths. Call it before
// locking the MCP server for a disabling transition.
func LockMCPServerVisibilityDependencies(ctx context.Context, tx pgx.Tx, organizationID string, projectID, serverID uuid.UUID) error {
	if tx == nil || organizationID == "" || projectID == uuid.Nil || serverID == uuid.Nil {
		return fmt.Errorf("invalid MCP server visibility lock input")
	}
	// Root-selection mutations lock the organization's custom-domain row before
	// endpoint/server rows. Take that same lock before discovering dependencies so
	// an endpoint cannot gain a domain after the list but before endpoint locking.
	if _, err := customdomainsrepo.New(tx).LockCustomDomainByOrganization(ctx, organizationID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock MCP server custom domain: %w", err)
	}
	domainIDs, err := mcpendpointsrepo.New(tx).ListCustomDomainIDsByMCPServerID(ctx, mcpendpointsrepo.ListCustomDomainIDsByMCPServerIDParams{McpServerID: serverID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("list MCP server custom domains: %w", err)
	}
	if err := lockMcpServerCustomDomains(ctx, tx, domainIDs); err != nil {
		return fmt.Errorf("lock MCP server custom domains: %w", err)
	}
	if _, err := mcpendpointsrepo.New(tx).LockRootMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockRootMCPEndpointsByMCPServerIDParams{McpServerID: serverID, ProjectID: projectID}); err != nil {
		return fmt.Errorf("lock MCP server root endpoints: %w", err)
	}
	return nil
}

// UpdateMCPServerVisibilityInTransaction applies the shared visibility state
// transition without selecting or changing any plugin attachment. Its caller
// must first lock dependencies through LockMCPServerVisibilityDependencies, then
// lock the server. It clears roots and audits the automatic route cleanup in the
// same transaction; callers reconcile returned domains only after commit.
func UpdateMCPServerVisibilityInTransaction(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, existing repo.McpServer, input LifecycleUpdateInput) (MCPServerVisibilityResult, error) {
	if input.Visibility != VisibilityDisabled && input.Visibility != VisibilityPrivate {
		return MCPServerVisibilityResult{}, fmt.Errorf("invalid MCP server lifecycle visibility")
	}
	updated, err := UpdateMCPServerLifecycleInTransaction(ctx, tx, auditLogger, existing, input)
	if err != nil {
		return MCPServerVisibilityResult{}, err
	}
	var cleared []mcpendpointsrepo.McpEndpoint
	if updated.Visibility == VisibilityDisabled {
		cleared, err = mcpendpointsrepo.New(tx).ClearRootMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ClearRootMCPEndpointsByMCPServerIDParams{McpServerID: updated.ID, ProjectID: input.ProjectID})
		if err != nil {
			return MCPServerVisibilityResult{}, fmt.Errorf("clear MCP server root endpoints: %w", err)
		}
		if err := logMCPServerRootAutoClears(ctx, tx, auditLogger, input.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorUserID), input.ActorEmail, cleared); err != nil {
			return MCPServerVisibilityResult{}, err
		}
	}
	return MCPServerVisibilityResult{Server: updated, ClearedRootDomainIDs: rootDomainIDs(cleared)}, nil
}

func logMCPServerRootAutoClears(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, organizationID string, actor urn.Principal, actorDisplayName *string, rootEndpoints []mcpendpointsrepo.McpEndpoint) error {
	if auditLogger == nil || organizationID == "" {
		return fmt.Errorf("invalid MCP root cleanup audit input")
	}
	repository := customdomainsrepo.New(tx)
	for _, endpoint := range rootEndpoints {
		if !endpoint.CustomDomainID.Valid {
			continue
		}
		domain, err := repository.GetCustomDomainByID(ctx, endpoint.CustomDomainID.UUID)
		if err != nil {
			return fmt.Errorf("load custom domain for MCP root cleanup audit: %w", err)
		}
		if err := auditLogger.LogCustomDomainUpdate(ctx, tx, audit.LogCustomDomainUpdateEvent{
			OrganizationID:             organizationID,
			Actor:                      actor,
			ActorDisplayName:           actorDisplayName,
			ActorSlug:                  nil,
			CustomDomainURN:            urn.NewCustomDomain(domain.ID),
			DomainName:                 domain.Domain,
			CustomDomainSnapshotBefore: mv.BuildCustomDomainView(domain, false, endpoint.ID),
			CustomDomainSnapshotAfter:  mv.BuildCustomDomainView(domain, false, uuid.Nil),
		}); err != nil {
			return fmt.Errorf("audit MCP root cleanup: %w", err)
		}
	}
	return nil
}

// UpdateMCPServerLifecycleInTransaction updates only the name-derived slug and
// visibility of a locked MCP server. It synchronizes auto-derived plugin display
// names and writes the normal MCP update audit event, but it never creates,
// removes, or selects a plugin attachment.
func UpdateMCPServerLifecycleInTransaction(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, existing repo.McpServer, input LifecycleUpdateInput) (repo.McpServer, error) {
	if tx == nil || auditLogger == nil || input.OrganizationID == "" || input.ProjectID == uuid.Nil || input.ActorUserID == "" || input.ServerID == uuid.Nil || existing.ID != input.ServerID || existing.ProjectID != input.ProjectID || input.Visibility == "" {
		return repo.McpServer{}, fmt.Errorf("invalid MCP server lifecycle update input")
	}

	name := existing.Name
	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		if trimmed == "" || len(trimmed) > maxLifecycleMCPServerNameBytes || strings.ContainsAny(trimmed, "\r\n") {
			return repo.McpServer{}, fmt.Errorf("MCP server name must be non-empty, at most %d bytes, and contain no line breaks", maxLifecycleMCPServerNameBytes)
		}
		name = conv.ToPGText(trimmed)
	}
	slug, err := computeServerSlug(conv.FromPGTextOrEmpty[string](name), existing.ID)
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("compute MCP server slug: %w", err)
	}

	storedMode := pgtype.Text{String: "", Valid: false}
	if input.NetworkAccessMode != nil {
		if _, err := networkaccess.Parse(string(*input.NetworkAccessMode)); err != nil {
			return repo.McpServer{}, fmt.Errorf("validate MCP server network access mode: %w", err)
		}
		storedMode = networkaccess.Storage(*input.NetworkAccessMode)
	}

	updated, err := repo.New(tx).UpdateMCPServer(ctx, repo.UpdateMCPServerParams{
		Name:                  name,
		Slug:                  conv.ToPGText(slug),
		EnvironmentID:         input.EnvironmentID,
		UserSessionIssuerID:   input.UserSessionIssuerID,
		RemoteMcpServerID:     input.RemoteMcpServerID,
		TunneledMcpServerID:   input.TunneledMcpServerID,
		ToolsetID:             input.ToolsetID,
		UnproxiedMcpServerID:  input.UnproxiedMcpServerID,
		ToolVariationsGroupID: input.ToolVariationsGroupID,
		Visibility:            input.Visibility,
		NetworkAccessModeSet:  input.NetworkAccessMode != nil,
		NetworkAccessMode:     storedMode,
		ID:                    existing.ID,
		ProjectID:             input.ProjectID,
	})
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("update MCP server lifecycle: %w", err)
	}

	oldDisplayName := ServerDisplayName(existing)
	newDisplayName := ServerDisplayName(updated)
	if oldDisplayName != newDisplayName {
		if _, err := pluginsrepo.New(tx).SyncMcpServerDisplayName(ctx, pluginsrepo.SyncMcpServerDisplayNameParams{
			NewDisplayName: newDisplayName,
			ProjectID:      input.ProjectID,
			McpServerID:    uuid.NullUUID{UUID: updated.ID, Valid: true},
			OldDisplayName: oldDisplayName,
		}); err != nil {
			return repo.McpServer{}, fmt.Errorf("sync MCP server plugin display name: %w", err)
		}
	}

	if err := auditLogger.LogMcpServerUpdate(ctx, tx, audit.LogMcpServerUpdateEvent{
		OrganizationID:          input.OrganizationID,
		ProjectID:               input.ProjectID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorUserID),
		ActorDisplayName:        input.ActorEmail,
		ActorSlug:               nil,
		McpServerURN:            urn.NewMcpServer(updated.ID),
		McpServerName:           conv.FromPGTextOrEmpty[string](updated.Name),
		McpServerSlug:           conv.FromPGTextOrEmpty[string](updated.Slug),
		McpServerSnapshotBefore: mv.BuildMcpServerView(existing),
		McpServerSnapshotAfter:  mv.BuildMcpServerView(updated),
	}); err != nil {
		return repo.McpServer{}, fmt.Errorf("audit MCP server lifecycle update: %w", err)
	}
	return updated, nil
}
