package mcpservers

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// An MCP server's environment link hands that environment's values to
// whatever the server fronts. Changing the link, or keeping it while the
// server is repointed at a different backend, therefore needs the authority
// the environments service requires to link an environment to a source or
// toolset: project-wide environment:read plus read access to each environment
// the change affects (authz.EnvironmentLinkChecks), so an exclusion naming one
// environment refuses it. MCP-server write alone is not enough. Remote URL
// changes and tunnel key rotations move a linked server's destination without
// touching the server row, so their handlers apply the same checks to every
// environment linked on that source.
//
// Every writer that changes a link and every destination change that checks
// for one holds admission.LockProject before reading the state it decides on,
// so a link committed concurrently cannot slip past a destination change that
// already looked.

// environmentLinkAffected reports whether writing ids requires environment
// authority and which stored environments the write affects. existing is nil
// for a create. A hosted server that drops its own environment falls back to
// its toolset's default; resolveHostedFallbackEnvironment adds that one.
func environmentLinkAffected(existing *repo.McpServer, ids serverIDs) (bool, []uuid.UUID) {
	if existing == nil {
		if !ids.EnvironmentID.Valid {
			return false, nil
		}
		return true, []uuid.UUID{ids.EnvironmentID.UUID}
	}
	var affected []uuid.UUID
	if existing.EnvironmentID.Valid {
		affected = append(affected, existing.EnvironmentID.UUID)
	}
	if ids.EnvironmentID != existing.EnvironmentID {
		if ids.EnvironmentID.Valid {
			affected = append(affected, ids.EnvironmentID.UUID)
		}
		return true, affected
	}
	if ids.EnvironmentID.Valid && serverBackendChanged(*existing, ids) {
		return true, affected
	}
	return false, nil
}

// resolveHostedFallbackEnvironment returns the environment a toolset-backed
// server will use once its own environment is cleared: its toolset's live
// default, if any. Only an explicit unlink onto a toolset backend has one.
func resolveHostedFallbackEnvironment(ctx context.Context, tx repo.DBTX, projectID uuid.UUID, existing repo.McpServer, ids serverIDs) (uuid.NullUUID, error) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if ids.EnvironmentID.Valid || !existing.EnvironmentID.Valid || !ids.ToolsetID.Valid {
		return none, nil
	}
	id, err := repo.New(tx).GetToolsetDefaultEnvironmentID(ctx, repo.GetToolsetDefaultEnvironmentIDParams{
		ToolsetID: ids.ToolsetID.UUID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, nil
	case err != nil:
		return none, fmt.Errorf("resolve toolset default environment: %w", err)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func serverBackendChanged(existing repo.McpServer, ids serverIDs) bool {
	return ids.RemoteMcpServerID != existing.RemoteMcpServerID ||
		ids.TunneledMcpServerID != existing.TunneledMcpServerID ||
		ids.ToolsetID != existing.ToolsetID ||
		ids.UnproxiedMcpServerID != existing.UnproxiedMcpServerID
}

// EnvironmentLinkChecks is authz.EnvironmentLinkChecks over environment IDs.
func EnvironmentLinkChecks(projectID uuid.UUID, environmentIDs []uuid.UUID) []authz.Check {
	ids := make([]string, 0, len(environmentIDs))
	for _, id := range environmentIDs {
		ids = append(ids, id.String())
	}
	return authz.EnvironmentLinkChecks(projectID.String(), ids...)
}

// RemoteLinkedEnvironmentIDs returns the distinct environments linked to live
// MCP servers on the remote source, disabled ones included. A caller deciding
// whether to allow a destination change must hold admission.LockProject for
// projectID; a read for display needs no lock.
func RemoteLinkedEnvironmentIDs(ctx context.Context, tx repo.DBTX, projectID, remoteMCPServerID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := repo.New(tx).ListEnvironmentIDsLinkedToRemote(ctx, repo.ListEnvironmentIDsLinkedToRemoteParams{
		ProjectID:         projectID,
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteMCPServerID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list environments linked to remote source: %w", err)
	}
	return ids, nil
}

// TunnelLinkedEnvironmentIDs is the tunneled counterpart of
// RemoteLinkedEnvironmentIDs.
func TunnelLinkedEnvironmentIDs(ctx context.Context, tx repo.DBTX, projectID, tunneledMCPServerID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := repo.New(tx).ListEnvironmentIDsLinkedToTunnel(ctx, repo.ListEnvironmentIDsLinkedToTunnelParams{
		ProjectID:           projectID,
		TunneledMcpServerID: uuid.NullUUID{UUID: tunneledMCPServerID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list environments linked to tunneled source: %w", err)
	}
	return ids, nil
}

// DestinationChangeEligibility is what a source's getServer reports about its
// environment links for the current caller.
type DestinationChangeEligibility struct {
	// Linked is true when a live MCP server on the source has an environment.
	Linked bool
	// Authorized is true when the caller satisfies the environment checks a
	// destination change needs: always when nothing is linked.
	Authorized bool
}

// EvaluateDestinationChange answers DestinationChangeEligibility without
// recording a denial: an unauthorized caller is an ordinary answer here, not
// a refused request.
func EvaluateDestinationChange(ctx context.Context, engine *authz.Engine, projectID uuid.UUID, linked []uuid.UUID) (DestinationChangeEligibility, error) {
	if len(linked) == 0 {
		return DestinationChangeEligibility{Linked: false, Authorized: true}, nil
	}
	ok, err := engine.Evaluate(ctx, EnvironmentLinkChecks(projectID, linked)...)
	if err != nil {
		return DestinationChangeEligibility{}, fmt.Errorf("evaluate environment link authority: %w", err)
	}
	return DestinationChangeEligibility{Linked: true, Authorized: ok}, nil
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
