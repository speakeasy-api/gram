package mcpservers

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// HostedServerUpdate is an mcpServers.update aimed at a canonical hosted wrapper (id = toolset id).
type HostedServerUpdate struct {
	// ToolsetID is the backing toolset, which is also the wrapper id.
	ToolsetID uuid.UUID

	// Name renames the server when set.
	Name *string

	// Visibility is the requested disabled, private or public state.
	Visibility string

	// UserSessionIssuerID links an issuer when valid; invalid keeps the current one.
	UserSessionIssuerID uuid.NullUUID

	// ClearUserSessionIssuer detaches the issuer; UserSessionIssuerID is then invalid.
	ClearUserSessionIssuer bool

	// ToolVariationsGroupID replaces the tool filtering group; invalid clears it.
	ToolVariationsGroupID uuid.NullUUID

	// NetworkAccessMode changes the network policy when set.
	NetworkAccessMode *networkaccess.Mode
}

// HostedServerWriter writes canonical hosted wrappers, whose name and tool variations group the toolset shares.
type HostedServerWriter interface {
	UpdateHostedServer(ctx context.Context, update HostedServerUpdate) (repo.McpServer, error)

	// DisableHostedServer disables the hosted server, keeping the wrapper and its endpoints.
	DisableHostedServer(ctx context.Context, toolsetID uuid.UUID) error
}

// WithHostedServerWriter lets update and delete manage canonical hosted wrappers; without one they are rejected.
func (s *Service) WithHostedServerWriter(writer HostedServerWriter) *Service {
	s.hostedServers = writer
	return s
}

func (s *Service) updateHostedServer(ctx context.Context, logger *slog.Logger, organizationID string, existing repo.McpServer, payload *gen.UpdateMcpServerPayload, ids serverIDs) (*types.McpServer, error) {
	if s.hostedServers == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "hosted MCP server writer is not configured")
	}
	if ids.ToolsetID != existing.ToolsetID {
		return nil, oops.E(oops.CodeInvalid, nil, "a hosted MCP server keeps its toolset backend").LogWarn(ctx, logger)
	}
	if ids.EnvironmentID != existing.EnvironmentID {
		return nil, oops.E(oops.CodeInvalid, nil, "a hosted MCP server uses its toolset's default environment").LogWarn(ctx, logger)
	}
	switch payload.Visibility {
	case VisibilityDisabled, VisibilityPrivate, VisibilityPublic:
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid visibility").LogWarn(ctx, logger)
	}

	var name *string
	if payload.Name != nil {
		trimmed := strings.TrimSpace(*payload.Name)
		if trimmed == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "name must be non-empty").LogWarn(ctx, logger)
		}
		name = &trimmed
	}
	var mode *networkaccess.Mode
	if payload.NetworkAccessMode != nil {
		parsed, err := networkaccess.Parse(string(*payload.NetworkAccessMode))
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid network access mode").LogWarn(ctx, logger)
		}
		mode = &parsed
	}

	updated, err := s.hostedServers.UpdateHostedServer(ctx, HostedServerUpdate{
		ToolsetID:              existing.ID,
		Name:                   name,
		Visibility:             string(payload.Visibility),
		UserSessionIssuerID:    ids.UserSessionIssuerID,
		ClearUserSessionIssuer: payload.ClearUserSessionIssuer != nil && *payload.ClearUserSessionIssuer,
		ToolVariationsGroupID:  ids.ToolVariationsGroupID,
		NetworkAccessMode:      mode,
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // The writer returns oops errors whose codes reach the caller.
	}
	view, _ := s.buildMcpServerView(ctx, organizationID, updated)
	return view, nil
}

// deleteHostedServer disables rather than tombstones: the wrapper id is the toolset id and cannot be reused.
func (s *Service) deleteHostedServer(ctx context.Context, toolsetID uuid.UUID) error {
	if s.hostedServers == nil {
		return oops.E(oops.CodeUnexpected, nil, "hosted MCP server writer is not configured")
	}
	return s.hostedServers.DisableHostedServer(ctx, toolsetID) //nolint:wrapcheck // The writer returns oops errors whose codes reach the caller.
}
