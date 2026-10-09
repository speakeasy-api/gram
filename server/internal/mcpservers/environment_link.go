package mcpservers

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// An MCP server's environment link hands that environment's values to
// whatever the server fronts. Changing the link, or keeping it while the
// server is repointed at a different backend, therefore needs the same
// project-wide environment authority (authz.EnvironmentLinkCheck) the
// environments service requires to link an environment to a source or toolset;
// MCP-server write alone is not enough. Remote URL changes and tunnel key
// rotations move a linked server's destination without touching the server
// row, so their handlers apply the same check when a live server on that
// source is linked.
//
// Every writer that changes a link and every destination change that checks
// for one holds admission.LockProject before reading the state it decides on,
// so a link committed concurrently cannot slip past a destination change that
// already looked.

// environmentLinkNeedsAuthority reports whether writing ids requires
// environment authority. existing is nil for a create.
func environmentLinkNeedsAuthority(existing *repo.McpServer, ids serverIDs) bool {
	if existing == nil {
		return ids.EnvironmentID.Valid
	}
	if ids.EnvironmentID != existing.EnvironmentID {
		return true
	}
	return ids.EnvironmentID.Valid && serverBackendChanged(*existing, ids)
}

func serverBackendChanged(existing repo.McpServer, ids serverIDs) bool {
	return ids.RemoteMcpServerID != existing.RemoteMcpServerID ||
		ids.TunneledMcpServerID != existing.TunneledMcpServerID ||
		ids.ToolsetID != existing.ToolsetID ||
		ids.UnproxiedMcpServerID != existing.UnproxiedMcpServerID
}

// RemoteHasEnvironmentLinkedServers reports whether any live MCP server on
// the remote source carries an environment link. The caller must hold
// admission.LockProject for projectID.
func RemoteHasEnvironmentLinkedServers(ctx context.Context, tx pgx.Tx, projectID, remoteMCPServerID uuid.UUID) (bool, error) {
	linked, err := repo.New(tx).HasEnvironmentLinkedMCPServerForRemote(ctx, repo.HasEnvironmentLinkedMCPServerForRemoteParams{
		ProjectID:         projectID,
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteMCPServerID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("check environment-linked MCP servers for remote source: %w", err)
	}
	return linked, nil
}

// TunnelHasEnvironmentLinkedServers reports whether any live MCP server on the
// tunneled source carries an environment link. The caller must hold
// admission.LockProject for projectID.
func TunnelHasEnvironmentLinkedServers(ctx context.Context, tx pgx.Tx, projectID, tunneledMCPServerID uuid.UUID) (bool, error) {
	linked, err := repo.New(tx).HasEnvironmentLinkedMCPServerForTunnel(ctx, repo.HasEnvironmentLinkedMCPServerForTunnelParams{
		ProjectID:           projectID,
		TunneledMcpServerID: uuid.NullUUID{UUID: tunneledMCPServerID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("check environment-linked MCP servers for tunneled source: %w", err)
	}
	return linked, nil
}

// logEnvironmentLinkChange records a link or unlink when the stored
// environment moved from before to server.EnvironmentID. An unchanged link
// records nothing; the surrounding create or update event covers the rest.
func logEnvironmentLinkChange(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, organizationID string, projectID uuid.UUID, actorUserID string, actorEmail *string, before uuid.NullUUID, server repo.McpServer) error {
	after := server.EnvironmentID
	if before == after {
		return nil
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, actorUserID)
	serverURN := urn.NewMcpServer(server.ID)
	name := conv.FromPGTextOrEmpty[string](server.Name)
	slug := conv.FromPGTextOrEmpty[string](server.Slug)

	if !after.Valid {
		if err := auditLogger.LogMcpServerEnvironmentUnlink(ctx, tx, audit.LogMcpServerEnvironmentUnlinkEvent{
			OrganizationID:   organizationID,
			ProjectID:        projectID,
			Actor:            actor,
			ActorDisplayName: actorEmail,
			ActorSlug:        nil,
			McpServerURN:     serverURN,
			McpServerName:    name,
			McpServerSlug:    slug,
			EnvironmentURN:   urn.NewEnvironment(before.UUID),
		}); err != nil {
			return fmt.Errorf("log MCP server environment unlink: %w", err)
		}
		return nil
	}

	var previous *urn.Environment
	if before.Valid {
		previousURN := urn.NewEnvironment(before.UUID)
		previous = &previousURN
	}
	if err := auditLogger.LogMcpServerEnvironmentLink(ctx, tx, audit.LogMcpServerEnvironmentLinkEvent{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		Actor:                  actor,
		ActorDisplayName:       actorEmail,
		ActorSlug:              nil,
		McpServerURN:           serverURN,
		McpServerName:          name,
		McpServerSlug:          slug,
		EnvironmentURN:         urn.NewEnvironment(after.UUID),
		PreviousEnvironmentURN: previous,
	}); err != nil {
		return fmt.Errorf("log MCP server environment link: %w", err)
	}
	return nil
}
