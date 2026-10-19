// Member snapshot resolution for the meta-server MCP surface: one
// project-scoped query per request, classified by backend and RBAC-filtered
// before anything is exposed.

package mcp

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// metaMemberBackend classifies how the meta MCP reaches one member.
type metaMemberBackend int

const (
	// Toolset-backed: executes in-process.
	metaMemberBackendHosted metaMemberBackend = iota
	// Remote/tunneled: dispatched through the member's own upstream (AIM-87).
	metaMemberBackendProxied
)

// metaMember is one servable, authorized member in a request's snapshot.
type metaMember struct {
	serverID uuid.UUID
	// The member's own project. A stored gateway's members all share the
	// gateway's project, but an agent gateway's can span several, so member
	// dispatch keys on this rather than on the request's project.
	projectID             uuid.UUID
	projectSlug           string
	slug                  string
	name                  string
	sortOrder             int32
	backend               metaMemberBackend
	toolsetID             uuid.NullUUID
	remoteServerID        uuid.NullUUID
	tunneledServerID      uuid.NullUUID
	visibility            string
	environmentID         uuid.NullUUID
	toolVariationsGroupID uuid.NullUUID
	// The member server's own derived provider issuer (see
	// mcpserverissuersync.go). Token routing for tunneled members keys on it,
	// so NULL or stale degrades to an anonymous call, never a wrong bearer.
	remoteSessionIssuerID uuid.NullUUID

	// A tunneled member's recorded RFC 8707 resource identifier, empty when it
	// records none. Routing accepts a grant qualified to this value; it never
	// selects across issuers, so it is not an address and is never dialed.
	tunneledResourceIdentifier string
}

// memberStatus is the list_servers connection state. Hosted members execute
// in-process, so they are always available. A tunneled member's liveness is
// one route-store read; a remote member's would be a credentialed network
// probe per listing, so it stays unknown until cached health exists.
func (s *Service) memberStatus(ctx context.Context, member metaMember) string {
	switch {
	case member.backend == metaMemberBackendHosted:
		return metamcp.StatusAvailable
	case member.tunneledServerID.Valid:
		if s.tunnelManager == nil || s.tunnelManager.routes == nil {
			return metamcp.StatusUnknown
		}
		candidates, err := s.tunnelManager.routes.Candidates(ctx, member.tunneledServerID.UUID.String())
		if err != nil {
			return metamcp.StatusUnknown
		}
		if len(candidates) == 0 {
			return metamcp.StatusUnavailable
		}
		return metamcp.StatusAvailable
	default:
		return metamcp.StatusUnknown
	}
}

// metaMemberCandidate is one membership before admission: the member server's
// identity and backend wiring, carried independently of where the membership
// came from. Stored meta servers enumerate meta_mcp_server_members rows; an
// agent gateway enumerates the servers its delegated policy names. Admission
// is the same for both, so it must not be reimplemented per source.
type metaMemberCandidate struct {
	serverID                   uuid.UUID
	projectID                  uuid.UUID
	projectSlug                string
	slug                       string
	name                       string
	sortOrder                  int32
	visibility                 string
	unproxied                  bool
	toolsetID                  uuid.NullUUID
	remoteServerID             uuid.NullUUID
	tunneledServerID           uuid.NullUUID
	environmentID              uuid.NullUUID
	toolVariationsGroupID      uuid.NullUUID
	remoteSessionIssuerID      uuid.NullUUID
	tunneledResourceIdentifier string
}

// resolveMetaMemberSnapshot loads a stored meta server's servable members and
// applies the per-member RBAC filter; unproxied members (no meta MCP dispatch
// path) are excluded, so pre-validation memberships degrade to invisibility.
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

	candidates := make([]metaMemberCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, metaMemberCandidate{
			serverID: row.McpServerID,
			// A stored gateway's members are all in its own project, which the
			// membership query already constrains. Its slugs are unique within
			// that project, so they need no qualifying prefix.
			projectID:                  projectID,
			projectSlug:                "",
			slug:                       conv.PtrValOr(conv.FromPGText[string](row.McpServerSlug), ""),
			name:                       conv.PtrValOr(conv.FromPGText[string](row.McpServerName), ""),
			sortOrder:                  row.SortOrder,
			visibility:                 row.McpServerVisibility,
			unproxied:                  row.McpServerUnproxiedMcpServerID.Valid,
			toolsetID:                  row.McpServerToolsetID,
			remoteServerID:             row.McpServerRemoteMcpServerID,
			tunneledServerID:           row.McpServerTunneledMcpServerID,
			environmentID:              row.McpServerEnvironmentID,
			toolVariationsGroupID:      row.McpServerToolVariationsGroupID,
			remoteSessionIssuerID:      row.McpServerRemoteSessionIssuerID,
			tunneledResourceIdentifier: row.TunneledResourceIdentifier,
		})
	}

	return s.admitMetaMembers(ctx, logger, candidates)
}

// resolveEndpointMemberSnapshot resolves the members an OAuth flow for a
// gateway endpoint authorizes against: a stored gateway's membership rows, or
// an organization gateway's derived members.
func (s *Service) resolveEndpointMemberSnapshot(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint) (context.Context, []metaMember, error) {
	if endpoint.IsOrganizationGateway() {
		return s.resolveOrganizationMemberSnapshot(ctx, logger, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
	}
	return s.resolveMetaMemberSnapshot(ctx, logger, endpoint.MetaMcpServerID.UUID, endpoint.ProjectID)
}

// resolveOrganizationMemberSnapshot derives a gateway's members from the
// caller's own access rather than from stored membership rows: every servable
// server in the organization is a candidate, and the caller's grants decide
// which survive admission. It serves both derived gateways, the agent gateway
// (grants carried by the agent key) and the organization gateway (grants of
// the signed-in user). Nothing is persisted, so a grant revoked in the
// dashboard stops appearing on the caller's very next request with nothing to
// redistribute or re-install.
//
// userSessionIssuerID is the organization gateway's issuer, whose clients are
// the only way a person connects a member that signs in upstream. uuid.Nil
// (the agent gateway) skips that check.
//
// The organization gateway also drops servers backed by a toolset with
// Platform tools, mirroring automatic plugin delivery: those toolsets serve
// assistants and explicit MCP consumers, never a broad distribution surface.
// The agent gateway keeps them: its members are what an agent key was
// explicitly granted.
func (s *Service) resolveOrganizationMemberSnapshot(
	ctx context.Context,
	logger *slog.Logger,
	organizationID string,
	userSessionIssuerID uuid.UUID,
) (context.Context, []metaMember, error) {
	// Unconditional for the same reason resolveMetaMemberSnapshot prepares it.
	ctx, err := s.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnexpected, err, "load access grants").LogError(ctx, logger)
	}

	rows, err := mcpserversrepo.New(s.db).ListServableMCPServersByOrganizationID(ctx, organizationID)
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnexpected, err, "list derived gateway members").LogError(ctx, logger)
	}

	// The gateway is mounted on both the public and the private listener, but a
	// member's own network access mode decides which of them may reach it. The
	// stored serving path enforces this when it resolves an endpoint by slug
	// (mcpendpoints.Resolve); a derived snapshot resolves nothing by slug, so
	// the same rule has to be applied to each candidate here or a private_only
	// server would list and dispatch over the public ingress.
	surface := requestNetworkSurface(ctx)

	candidates := make([]metaMemberCandidate, 0, len(rows))
	for _, row := range rows {
		if !servableOnSurface(row.McpServerNetworkAccessMode, surface) {
			continue
		}
		if userSessionIssuerID != uuid.Nil && row.HasPlatformTool {
			continue
		}
		candidates = append(candidates, metaMemberCandidate{
			serverID:    row.McpServerID,
			projectID:   row.McpServerProjectID,
			projectSlug: row.McpServerProjectSlug,
			slug:        conv.PtrValOr(conv.FromPGText[string](row.McpServerSlug), ""),
			name:        conv.PtrValOr(conv.FromPGText[string](row.McpServerName), ""),
			// A derived gateway has no operator-authored ordering; the query
			// orders by project and slug so the listing is stable across
			// requests.
			sortOrder:                  0,
			visibility:                 row.McpServerVisibility,
			unproxied:                  row.McpServerUnproxiedMcpServerID.Valid,
			toolsetID:                  row.McpServerToolsetID,
			remoteServerID:             row.McpServerRemoteMcpServerID,
			tunneledServerID:           row.McpServerTunneledMcpServerID,
			environmentID:              row.McpServerEnvironmentID,
			toolVariationsGroupID:      row.McpServerToolVariationsGroupID,
			remoteSessionIssuerID:      row.McpServerRemoteSessionIssuerID,
			tunneledResourceIdentifier: row.TunneledResourceIdentifier,
		})
	}

	ctx, members, err := s.admitMetaMembers(ctx, logger, candidates)
	if err != nil {
		return ctx, nil, err
	}
	var clients []remotesessions.Client
	if userSessionIssuerID != uuid.Nil {
		clients, err = s.remoteChallengeMgr.ListOrganizationClients(ctx, organizationID, userSessionIssuerID)
		if err != nil {
			return ctx, nil, oops.E(oops.CodeUnexpected, err, "list gateway sign-in clients").LogError(ctx, logger)
		}
	}
	eligible := make([]metaMember, 0, len(members))
	for _, member := range members {
		supported, needsOAuth, err := s.derivedMemberAuth(ctx, member)
		if err != nil {
			return ctx, nil, oops.E(oops.CodeUnexpected, err, "check gateway member authentication").LogError(ctx, logger)
		}
		if !supported {
			continue
		}
		if needsOAuth && userSessionIssuerID != uuid.Nil && !slices.ContainsFunc(clients, func(client remotesessions.Client) bool {
			return clientServesAnyMember(client, []metaMember{member})
		}) {
			continue
		}
		eligible = append(eligible, member)
	}
	return ctx, qualifyMemberSlugsByProject(eligible), nil
}

// requestNetworkSurface is the ingress surface the request arrived on.
func requestNetworkSurface(ctx context.Context) networkaccess.Surface {
	if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface == requestorigin.SurfacePrivateNetwork {
		return networkaccess.SurfacePrivate
	}
	return networkaccess.SurfacePublic
}

// servableOnSurface reports whether a server with the given network access
// mode may be reached on surface. A mode this build cannot parse is not a mode
// it can serve: fail closed, exactly as the endpoint resolver does.
func servableOnSurface(networkAccessMode pgtype.Text, surface networkaccess.Surface) bool {
	mode, err := networkaccess.Effective(networkAccessMode)
	return err == nil && mode.Allows(surface)
}

// qualifyMemberSlugsByProject prefixes every member slug with its project's.
//
// Slugs are unique per project, not per organization, and a derived gateway can
// span projects — so two members could answer to the same slug, and the
// qualified serverslug--toolname contract would resolve to whichever came
// first. Qualifying every member, rather than only the colliding ones, keeps a
// member's name stable: an unrelated project adding a server must not rename
// anything a caller already holds.
func qualifyMemberSlugsByProject(members []metaMember) []metaMember {
	qualified := make([]metaMember, 0, len(members))
	for _, member := range members {
		if member.projectSlug != "" {
			member.slug = member.projectSlug + "." + member.slug
		}
		qualified = append(qualified, member)
	}
	return qualified
}

// admitMetaMembers applies the visibility and RBAC filter every meta surface
// shares. The caller must have prepared the authz context; admission is not
// the place to discover an unprepared one.
func (s *Service) admitMetaMembers(
	ctx context.Context,
	logger *slog.Logger,
	candidates []metaMemberCandidate,
) (context.Context, []metaMember, error) {
	members := make([]metaMember, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.unproxied {
			continue
		}

		backend := metaMemberBackendProxied
		if candidate.toolsetID.Valid {
			backend = metaMemberBackendHosted
		}

		// mcp:connect grants are keyed on the toolset id for toolset-backed
		// servers and the mcp_servers id for proxy backends. Checking the
		// wrong one silently hides the member from every resource-scoped
		// grant the dashboard writes (see grantResourceIdForMcpServer).
		connectResourceID := candidate.serverID
		if candidate.toolsetID.Valid {
			connectResourceID = candidate.toolsetID.UUID
		}

		// Visibility gates exposure and fails closed: public members are open,
		// private members require mcp:connect (as authorizeProxyBackendAccess)
		// with denied members filtered so unauthorized reads as nonexistent,
		// and any other value (malformed or future) is filtered the same way.
		// This gates on the member server's own visibility only: a hosted
		// member whose toolset is private still lists here (its endpoint is
		// public), then reads as nonexistent on drill-down (loadMemberToolset).
		switch candidate.visibility {
		case mcpservers.VisibilityPublic:
		case mcpservers.VisibilityPrivate:
			if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, connectResourceID.String(), candidate.projectID.String())); err != nil {
				// Forbidden and Unauthorized (anonymous callers on an ungated
				// endpoint carry no AuthContext) are denials: filter the
				// member. Anything else is an evaluation failure.
				var oopsErr *oops.ShareableError
				if errors.As(err, &oopsErr) && (oopsErr.Code == oops.CodeForbidden || oopsErr.Code == oops.CodeUnauthorized) {
					continue
				}
				return ctx, nil, oops.E(oops.CodeUnexpected, err, "check member authz").LogError(ctx, logger)
			}
		default:
			continue
		}

		members = append(members, metaMember{
			serverID:                   candidate.serverID,
			projectID:                  candidate.projectID,
			projectSlug:                candidate.projectSlug,
			slug:                       candidate.slug,
			name:                       candidate.name,
			sortOrder:                  candidate.sortOrder,
			backend:                    backend,
			toolsetID:                  candidate.toolsetID,
			remoteServerID:             candidate.remoteServerID,
			tunneledServerID:           candidate.tunneledServerID,
			visibility:                 candidate.visibility,
			environmentID:              candidate.environmentID,
			toolVariationsGroupID:      candidate.toolVariationsGroupID,
			remoteSessionIssuerID:      candidate.remoteSessionIssuerID,
			tunneledResourceIdentifier: candidate.tunneledResourceIdentifier,
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
