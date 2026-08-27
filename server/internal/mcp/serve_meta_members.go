// Member snapshot resolution for the meta-server MCP surface: one
// project-scoped query per request, classified by backend and RBAC-filtered
// before anything is exposed.

package mcp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// metaMemberBackend classifies how the gateway reaches one member.
type metaMemberBackend int

const (
	// Toolset-backed: executes in-process.
	metaMemberBackendHosted metaMemberBackend = iota
	// Remote/tunneled: dispatched through the member's own upstream (AIM-87).
	metaMemberBackendProxied
)

// metaMember is one servable, authorized member in a request's snapshot.
type metaMember struct {
	serverID              uuid.UUID
	slug                  string
	name                  string
	sortOrder             int32
	backend               metaMemberBackend
	toolsetID             uuid.NullUUID
	environmentID         uuid.NullUUID
	toolVariationsGroupID uuid.NullUUID
}

// status is the list_servers connection state: hosted members are always
// available; proxied stay unknown until the runtime holds live sessions.
func (m metaMember) status() string {
	if m.backend == metaMemberBackendHosted {
		return metamcp.StatusAvailable
	}
	return metamcp.StatusUnknown
}

// resolveMetaMemberSnapshot loads the servable members and applies the
// per-member RBAC filter; unproxied members (no gateway dispatch path) are
// excluded, so pre-validation memberships degrade to invisibility.
func (s *Service) resolveMetaMemberSnapshot(
	ctx context.Context,
	logger *slog.Logger,
	metaServerID uuid.UUID,
	projectID uuid.UUID,
) (context.Context, []metaMember, error) {
	// Unconditional so later per-tool checks (member dispatch on a private
	// toolset) never hit an unprepared context; no-op for callers RBAC never
	// enforces (AGE-2672).
	ctx, err := s.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnexpected, err, "load access grants").LogError(ctx, logger)
	}

	rows, err := metamcprepo.New(s.db).ListServableMetaMCPMembers(ctx, metamcprepo.ListServableMetaMCPMembersParams{
		MetaMcpServerID: metaServerID,
		ProjectID:       projectID,
	})
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnexpected, err, "list meta mcp members").LogError(ctx, logger)
	}

	members := make([]metaMember, 0, len(rows))
	for _, row := range rows {
		if row.McpServerUnproxiedMcpServerID.Valid {
			continue
		}

		backend := metaMemberBackendProxied
		if row.McpServerToolsetID.Valid {
			backend = metaMemberBackendHosted
		}

		// mcp:connect grants are keyed on the toolset id for toolset-backed
		// servers and the mcp_servers id for proxy backends. Checking the
		// wrong one silently hides the member from every resource-scoped
		// grant the dashboard writes (see grantResourceIdForMcpServer).
		connectResourceID := row.McpServerID
		if row.McpServerToolsetID.Valid {
			connectResourceID = row.McpServerToolsetID.UUID
		}

		// Visibility gates exposure and fails closed: public members are open,
		// private members require mcp:connect (as authorizeProxyBackendAccess)
		// with denied members filtered so unauthorized reads as nonexistent,
		// and any other value (malformed or future) is filtered the same way.
		// This gates on the member server's own visibility only: a hosted
		// member whose toolset is private still lists here (its endpoint is
		// public), then reads as nonexistent on drill-down (loadMemberToolset).
		switch row.McpServerVisibility {
		case mcpservers.VisibilityPublic:
		case mcpservers.VisibilityPrivate:
			if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, connectResourceID.String(), projectID.String())); err != nil {
				var oopsErr *oops.ShareableError
				if errors.As(err, &oopsErr) && oopsErr.Code == oops.CodeForbidden {
					continue
				}
				return ctx, nil, oops.E(oops.CodeUnexpected, err, "check member authz").LogError(ctx, logger)
			}
		default:
			continue
		}

		members = append(members, metaMember{
			serverID:              row.McpServerID,
			slug:                  conv.PtrValOr(conv.FromPGText[string](row.McpServerSlug), ""),
			name:                  conv.PtrValOr(conv.FromPGText[string](row.McpServerName), ""),
			sortOrder:             row.SortOrder,
			backend:               backend,
			toolsetID:             row.McpServerToolsetID,
			environmentID:         row.McpServerEnvironmentID,
			toolVariationsGroupID: row.McpServerToolVariationsGroupID,
		})
	}

	return ctx, members, nil
}

// findMetaMember resolves a slug against the snapshot; nonexistent,
// disabled, excluded, and unauthorized members all miss identically.
func findMetaMember(members []metaMember, serverSlug string) (metaMember, bool) {
	for _, member := range members {
		if member.slug == serverSlug {
			return member, true
		}
	}
	var none metaMember
	return none, false
}
