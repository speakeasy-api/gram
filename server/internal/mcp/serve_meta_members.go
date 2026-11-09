// Member snapshot resolution for the meta-server MCP surface: one
// project-scoped query per request, classified by backend and RBAC-filtered
// before anything is exposed.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
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
	// configurationHash is loaded with the request's approved member set.
	configurationHash string
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

	// chosenClientID is the upstream client the request's credential chose
	// for a proxied member, when serving reads credential choices. When set,
	// remoteSessionIssuerID is that client's issuer rather than the derived
	// one.
	chosenClientID uuid.NullUUID

	// upstreamNoAuth reports that the credential chose no upstream
	// authentication for this member.
	upstreamNoAuth bool

	// typedAuthClient reports that chosenClientID is the registration of a
	// typed auth client the resolver checked against the member, which
	// authorizes it without a sign-in link.
	typedAuthClient bool

	// upstreamUnavailable is why the credential cannot reach this member
	// under its choices, nil otherwise.
	upstreamUnavailable error
	authClient          *authClientResolution

	// associatedClients is a proxied member's associated upstream clients;
	// nil for a hosted member, which matches clients by authorization server
	// instead.
	associatedClients map[uuid.UUID]bool

	// typedRegistrations reports that associatedClients is exactly the
	// registrations the member offers through typed auth clients, so its
	// derived authorization server says nothing about how it signs in.
	typedRegistrations bool
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
	surface string,
	organizationID string,
	metaServerID uuid.UUID,
	projectID uuid.UUID,
) (context.Context, []metaMember, []metaMember, error) {
	// Unconditional so later per-tool checks (member dispatch on a private
	// toolset) never hit an unprepared context; no-op for callers RBAC never
	// enforces (AGE-2672).
	ctx, err := s.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "load access grants").LogError(ctx, logger)
	}

	rows, err := metamcprepo.New(s.db).ListServableMetaMCPMembers(ctx, metamcprepo.ListServableMetaMCPMembersParams{
		MetaMcpServerID: metaServerID,
		ProjectID:       projectID,
	})
	if err != nil {
		return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "list meta mcp members").LogError(ctx, logger)
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

	return s.admitMetaMembers(ctx, logger, surface, organizationID, candidates)
}

// resolveEndpointMemberSnapshot resolves the members an OAuth flow for a
// gateway endpoint authorizes against: a stored gateway's membership rows, or
// an organization gateway's derived members.
func (s *Service) resolveEndpointMemberSnapshot(ctx context.Context, logger *slog.Logger, surface string, endpoint *ResolvedMcpEndpoint) (context.Context, []metaMember, error) {
	if endpoint.IsOrganizationGateway() {
		ctx, members, _, err := s.resolveOrganizationMemberSnapshot(ctx, logger, surface, endpoint.OrganizationID, endpoint.UserSessionIssuerID, endpoint.ProjectID)
		return ctx, members, err
	}
	ctx, members, _, err := s.resolveMetaMemberSnapshot(ctx, logger, surface, endpoint.OrganizationID, endpoint.MetaMcpServerID.UUID, endpoint.ProjectID)
	if err != nil {
		return ctx, nil, err
	}
	// Only consent and its validation read a stored gateway's associated
	// clients, and only a gateway that signs people in has any to match.
	if endpoint.UserSessionIssuerID != uuid.Nil {
		if err := s.attachAssociatedClients(ctx, endpoint.OrganizationID, members); err != nil {
			return ctx, nil, oops.E(oops.CodeUnexpected, err, "load gateway member clients").LogError(ctx, logger)
		}
	}
	return ctx, members, nil
}

// resolveGateMemberSnapshot resolves the members a serving request answers
// from: a derived gateway's from the caller's access, a stored gateway's from
// its membership rows, each proxied member narrowed to the credential's
// chosen client.
func (s *Service) resolveGateMemberSnapshot(ctx context.Context, logger *slog.Logger, gate *metaGateContext, metaServerID, projectID uuid.UUID) (context.Context, []metaMember, error) {
	var (
		members []metaMember
		err     error
	)
	if gate.agentID == uuid.Nil && !gate.organizationGateway {
		ctx, members, gate.deniedMembers, err = s.resolveMetaMemberSnapshot(ctx, logger, mandatorySurfaceGatewayMember, gate.organizationID, metaServerID, projectID)
	} else {
		issuer := uuid.Nil
		scopeProjectID := uuid.Nil
		if gate.organizationGateway {
			issuer = gate.userSessionIssuerID
			scopeProjectID = gate.projectID
		}
		ctx, members, gate.deniedMembers, err = s.resolveOrganizationMemberSnapshot(ctx, logger, mandatorySurfaceGatewayMember, gate.organizationID, issuer, scopeProjectID)
	}
	if err == nil && gate.userID != "" && !isAssistantRequest(ctx) {
		members, gate.hiddenMembers = s.splitHiddenMembers(ctx, logger, gate.organizationID, gate.userID, members)
	}
	if err == nil && gate.catalog != nil {
		members, gate.unapprovedMembers = gate.catalog.approvedMembers(members)
		gate.hiddenMembers, _ = gate.catalog.approvedMembers(gate.hiddenMembers)
	}
	if err == nil && gate.catalog != nil {
		ids := make([]uuid.UUID, 0, len(members))
		for _, member := range members {
			ids = append(ids, member.serverID)
		}
		hashes, hashErr := s.catalogMemberConfigurations(ctx, gate.organizationID, ids)
		if hashErr != nil {
			return ctx, nil, hashErr
		}
		for i := range members {
			members[i].configurationHash = hashes[members[i].serverID]
		}
	}
	if err == nil && gate.memberChoices != nil {
		if gate.catalog == nil {
			gate.memberChoices.apply(ctx, logger, members, false)
		} else if gate.catalogRef.Mode != toolfilter.CatalogModeCached || gate.execute {
			gate.memberChoices.apply(ctx, logger, members, true)
		}
	}
	return ctx, members, err
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
// (the agent gateway) skips that check. A nonzero scopeProjectID restricts
// candidates before admission and credential association for project gateways.
//
// The organization gateway also drops servers backed by a toolset with
// Platform tools, mirroring automatic plugin delivery: those toolsets serve
// assistants and explicit MCP consumers, never a broad distribution surface.
// The agent gateway keeps them: its members are what an agent key was
// explicitly granted.
func (s *Service) resolveOrganizationMemberSnapshot(
	ctx context.Context,
	logger *slog.Logger,
	authzSurface string,
	organizationID string,
	userSessionIssuerID uuid.UUID,
	scopeProjectID uuid.UUID,
) (context.Context, []metaMember, []metaMember, error) {
	// Unconditional for the same reason resolveMetaMemberSnapshot prepares it.
	ctx, err := s.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "load access grants").LogError(ctx, logger)
	}

	rows, err := mcpserversrepo.New(s.db).ListServableMCPServersByOrganizationID(ctx, organizationID)
	if err != nil {
		return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "list derived gateway members").LogError(ctx, logger)
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
		if scopeProjectID != uuid.Nil && row.McpServerProjectID != scopeProjectID {
			continue
		}
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

	ctx, members, denied, err := s.admitMetaMembers(ctx, logger, authzSurface, organizationID, candidates)
	if err != nil {
		return ctx, nil, nil, err
	}
	var clients []remotesessions.Client
	if userSessionIssuerID != uuid.Nil {
		// The agent gateway signs nobody in, so its callers never read
		// associations and its members keep the derived match.
		if err := s.attachAssociatedClients(ctx, organizationID, members); err != nil {
			return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "load gateway member clients").LogError(ctx, logger)
		}
		clients, err = s.remoteChallengeMgr.ListOrganizationClients(ctx, organizationID, userSessionIssuerID)
		if err != nil {
			return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "list gateway sign-in clients").LogError(ctx, logger)
		}
	}
	shadow := s.newMandatoryShadow(ctx, organizationID)
	eligible := make([]metaMember, 0, len(members))
	for _, member := range members {
		supported, needsOAuth, err := s.derivedMemberAuth(ctx, shadow, authzSurface, member)
		if errors.Is(err, errMemberToolsetDenied) {
			// A public server whose private toolset the caller may not
			// connect to: kept aside like a private member admission turned
			// away, so a call that names it records the toolset's denial.
			denied = append(denied, member)
			continue
		}
		if err != nil {
			return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "check gateway member authentication").LogError(ctx, logger)
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
	return ctx, qualifyMemberSlugsByProject(eligible), denied, nil
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

// qualifyMemberSlugsByProject prefixes a member slug with its project's only
// when another member shares it.
//
// Slugs are unique per project, not per organization, and a derived gateway can
// span projects, so two members could answer to the same slug and the
// serverslug--toolname contract would resolve to whichever came first. Every
// member with a shared slug is qualified, so neither silently wins; the rest
// keep their plain slug, which is what Direct and Code Mode expose as names.
func qualifyMemberSlugsByProject(members []metaMember) []metaMember {
	counts := make(map[string]int, len(members))
	for _, member := range members {
		counts[member.slug]++
	}
	qualified := make([]metaMember, 0, len(members))
	for _, member := range members {
		if counts[member.slug] > 1 && member.projectSlug != "" {
			member.slug = member.projectSlug + "." + member.slug
		}
		qualified = append(qualified, member)
	}
	return qualified
}

// admitMetaMembers applies the visibility and RBAC filter every meta surface
// shares, returning the admitted members and the private members the caller
// may not connect to. The caller must have prepared the authz context;
// admission is not the place to discover an unprepared one.
func (s *Service) admitMetaMembers(
	ctx context.Context,
	logger *slog.Logger,
	surface string,
	organizationID string,
	candidates []metaMemberCandidate,
) (context.Context, []metaMember, []metaMember, error) {
	shadow := s.newMandatoryShadow(ctx, organizationID)
	members := make([]metaMember, 0, len(candidates))
	var denied []metaMember
	for _, candidate := range candidates {
		if candidate.unproxied {
			continue
		}

		backend := metaMemberBackendProxied
		if candidate.toolsetID.Valid {
			backend = metaMemberBackendHosted
		}

		// Visibility gates exposure and fails closed: public members are open,
		// private members require mcp:connect (as authorizeProxyBackendAccess)
		// with denied members filtered so unauthorized reads as nonexistent,
		// and any other value (malformed or future) is filtered the same way.
		// This gates on the member server's own visibility only: a hosted
		// member whose toolset is private still lists here (its endpoint is
		// public), then reads as nonexistent on drill-down (loadMemberToolset).
		//
		// Admission records no authorization check: it filters every server
		// the caller could reach, not one it asked for. The check is recorded
		// when a call names the member (findNamedMember), so denied members
		// are kept aside for that.
		connect := memberConnectCheck(candidate.serverID, candidate.toolsetID, candidate.projectID)
		refusal, err := s.memberConnectDecision(authz.WithoutChallenges(ctx), shadow, surface, candidate.visibility, connect)
		if err != nil {
			return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "check member authz").LogError(ctx, logger)
		}

		member := metaMember{
			serverID:                   candidate.serverID,
			configurationHash:          "",
			authClient:                 nil,
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
			chosenClientID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			upstreamNoAuth:             false,
			upstreamUnavailable:        nil,
			typedAuthClient:            false,
			associatedClients:          nil,
			typedRegistrations:         false,
		}
		switch {
		case refusal == nil:
			members = append(members, member)
		case candidate.visibility == mcpservers.VisibilityPrivate:
			// Kept aside so a call that names it records its denied check.
			// Any other visibility (malformed or future) is simply dropped.
			denied = append(denied, member)
		}
	}

	return ctx, members, denied, nil
}

// memberConnectDecision is the one connect decision for a server a caller
// reaches through another surface: a gateway member, a member's own toolset,
// a consent candidate, or a proxied backend. A public server is open. A
// private one needs connect. Any other visibility fails closed without a
// check. The decision is recorded in the mandatory shadow under surface.
//
// It returns a non-nil refusal when the caller may not connect, and err only
// when the check could not be evaluated. A refusal is Forbidden, or
// Unauthorized for an anonymous caller on an ungated endpoint, which carries
// no AuthContext. Whether the check is recorded as an authorization check
// follows ctx: callers that filter or list pass authz.WithoutChallenges.
// The caller must have prepared the authz context.
func (s *Service) memberConnectDecision(ctx context.Context, shadow *mandatoryShadow, surface string, visibility string, connect authz.Check) (refusal error, err error) {
	switch visibility {
	case mcpservers.VisibilityPublic:
		shadow.recordServer(ctx, surface, true, authz.MandatoryServerPublic, connect)
		return nil, nil
	case mcpservers.VisibilityPrivate:
	default:
		return oops.E(oops.CodeForbidden, nil, "unrecognized server visibility %q", visibility), nil
	}
	requireErr := s.authz.Require(ctx, connect)
	if requireErr != nil && !isMemberDenial(requireErr) {
		return nil, fmt.Errorf("require member connect: %w", requireErr)
	}
	shadow.recordServer(ctx, surface, requireErr == nil, authz.MandatoryServerPrivate, connect)
	return requireErr, nil
}

// isMemberDenial reports a refusal to connect rather than a failure to
// evaluate one.
func isMemberDenial(err error) bool {
	var oopsErr *oops.ShareableError
	return errors.As(err, &oopsErr) && (oopsErr.Code == oops.CodeForbidden || oopsErr.Code == oops.CodeUnauthorized)
}

// memberConnectCheck is the mcp:connect check a member's grants answer.
// Grants are keyed on the toolset id for toolset-backed servers and the
// mcp_servers id for proxy backends. Checking the wrong one silently hides the
// member from every resource-scoped grant the dashboard writes (see
// grantResourceIdForMcpServer).
func memberConnectCheck(serverID uuid.UUID, toolsetID uuid.NullUUID, projectID uuid.UUID) authz.Check {
	resourceID := serverID
	if toolsetID.Valid {
		resourceID = toolsetID.UUID
	}
	return authz.MCPCheck(authz.ScopeMCPConnect, resourceID.String(), projectID.String())
}

// findNamedMember resolves the server a call names and records the one
// authorization check the request makes for it: allowed for an admitted or
// hidden private member, denied for a member admission turned away. Admitted
// public members need no grant and record nothing here; a private toolset
// behind one records when the call loads it (loadMemberToolset). A slug that
// names no server records nothing.
func (s *Service) findNamedMember(ctx context.Context, gate *metaGateContext, members []metaMember, slug string) (metaMember, bool) {
	member, ok := findMetaMember(members, slug)
	if ok {
		s.recordNamedPrivateMemberCheck(ctx, member)
		return member, true
	}
	// A turned-away member was refused either its own connect or, behind a
	// public server on a derived gateway, its private toolset's. Both are
	// keyed on the same resource (memberConnectCheck), so this records the
	// refusal that kept it out.
	if denied, found := findDeniedMember(gate.deniedMembers, slug); found {
		s.recordNamedMemberCheck(ctx, denied)
	}
	// A server the caller hid is still one it may use: its check is
	// recorded, though the call is refused as hidden.
	if hidden, found := findMetaMember(gate.hiddenMembers, slug); found {
		s.recordNamedPrivateMemberCheck(ctx, hidden)
	}
	return member, false
}

// recordNamedPrivateMemberCheck records a private member's mcp:connect check.
// A public member needs no grant, so it records nothing.
func (s *Service) recordNamedPrivateMemberCheck(ctx context.Context, member metaMember) {
	if member.visibility != mcpservers.VisibilityPrivate {
		return
	}
	s.recordNamedMemberCheck(ctx, member)
}

// recordNamedMemberCheck records member's mcp:connect check. Admission
// already decided it without recording, so the outcome here only names it.
func (s *Service) recordNamedMemberCheck(ctx context.Context, member metaMember) {
	_ = s.authz.Require(ctx, memberConnectCheck(member.serverID, member.toolsetID, member.projectID))
}

// findDeniedMember matches a slug against members admission turned away.
// Their slugs were never qualified by project, so a qualified slug matches too.
func findDeniedMember(denied []metaMember, slug string) (metaMember, bool) {
	for _, member := range denied {
		if member.slug == slug || (member.projectSlug != "" && member.projectSlug+"."+member.slug == slug) {
			return member, true
		}
	}
	var none metaMember
	return none, false
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

// isAssistantRequest reports an assistant run, which acts for its owner but
// must not inherit the owner's hidden servers.
func isAssistantRequest(ctx context.Context) bool {
	_, ok := contextvalues.GetAssistantPrincipal(ctx)
	return ok
}

// splitHiddenMembers separates the servers the user hid from those served.
// Hiding is a preference, so a failed lookup serves every member rather than
// failing the request.
func (s *Service) splitHiddenMembers(ctx context.Context, logger *slog.Logger, organizationID, userID string, members []metaMember) ([]metaMember, []metaMember) {
	hidden, err := hiddenmcpservers.HiddenServerIDs(ctx, s.db, organizationID, userID)
	if err != nil {
		if ctx.Err() == nil {
			logger.WarnContext(ctx, "load hidden gateway members", attr.SlogError(err))
		}
		return members, nil
	}
	if len(hidden) == 0 {
		return members, nil
	}
	served := make([]metaMember, 0, len(members))
	var removed []metaMember
	for _, member := range members {
		if hidden[member.serverID] {
			removed = append(removed, member)
		} else {
			served = append(served, member)
		}
	}
	return served, removed
}
