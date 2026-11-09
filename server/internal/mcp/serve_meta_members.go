// Member snapshot resolution for the meta-server MCP surface: one
// project-scoped query per request, RBAC-filtered before anything is
// exposed.

package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers"
	"github.com/speakeasy-api/gram/server/internal/inv"
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

// metaMember is one servable, authorized member in a request's snapshot.
// Every member is a remote or tunneled server dispatched through its own
// upstream; hosted servers are never gateway members.
type metaMember struct {
	serverID uuid.UUID
	// configuration is loaded with the request's approved member set.
	configuration memberConfiguration

	// otherConfiguration is configuration with the upstream issuer from the
	// source the organization's flag does not make authoritative.
	otherConfiguration memberConfiguration
	// The member's own project. A stored gateway's members all share the
	// gateway's project, but an agent gateway's can span several, so member
	// dispatch keys on this rather than on the request's project.
	projectID        uuid.UUID
	projectSlug      string
	slug             string
	name             string
	sortOrder        int32
	remoteServerID   uuid.NullUUID
	tunneledServerID uuid.NullUUID
	visibility       string
	// The member server's own derived provider issuer (see
	// mcpserverissuersync.go). Token routing for tunneled members keys on it,
	// so NULL or stale degrades to an anonymous call, never a wrong bearer.
	remoteSessionIssuerID uuid.NullUUID

	// upstreamIssuer holds both of the member server's upstream issuer
	// sources, its column and its client associations, as listed. It does not
	// change when a choice replaces remoteSessionIssuerID.
	upstreamIssuer upstreamIssuerKeys

	// upstreamIssuerFromAssociations is the organization's upstream issuer
	// flag: remoteSessionIssuerID starts as the associations' provider when
	// set, and as the column otherwise.
	upstreamIssuerFromAssociations bool

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

	// associatedClients is the member's associated upstream clients; nil when
	// the caller does not read associations, in which case the member matches
	// clients by authorization server.
	associatedClients map[uuid.UUID]bool

	// typedRegistrations reports that associatedClients is exactly the
	// registrations the member offers through typed auth clients, so its
	// derived authorization server says nothing about how it signs in.
	typedRegistrations bool
}

// memberStatus is the list_servers connection state. A tunneled member's
// liveness is one route-store read; a remote member's would be a credentialed
// network probe per listing, so it stays unknown until cached health exists.
func (s *Service) memberStatus(ctx context.Context, member metaMember) string {
	switch {
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
	remoteServerID             uuid.NullUUID
	tunneledServerID           uuid.NullUUID
	upstreamIssuer             upstreamIssuerKeys
	tunneledResourceIdentifier string
}

// memberUpstreamIssuer is a listed member's two upstream issuer sources: its
// column, and the providers its live client associations name.
func memberUpstreamIssuer(column uuid.NullUUID, associationProviderIDs []uuid.UUID) upstreamIssuerKeys {
	association := remotesessions.AssociationIssuerFromProviders(associationProviderIDs)
	return upstreamIssuerKeys{
		column:      column,
		association: associationUpstreamIssuer{Provider: association.Provider, Providers: association.Providers},
		known:       true,
	}
}

// resolveMetaMemberSnapshot loads a stored meta server's servable members and
// applies the per-member RBAC filter. Only remote and tunneled backends have a
// gateway dispatch path; all other members are excluded, so pre-validation
// memberships degrade to invisibility.
//
// It also returns the private members the caller may not connect to, and the
// slugs of hosted members. Hosted servers are never served through a gateway.
// Membership writes refuse them, but rows written before that rule remain, so
// each is refused here too and its slug returned so a call naming it gets a
// tool error saying why.
func (s *Service) resolveMetaMemberSnapshot(
	ctx context.Context,
	logger *slog.Logger,
	surface string,
	organizationID string,
	metaServerID uuid.UUID,
	projectID uuid.UUID,
) (context.Context, []metaMember, []metaMember, []string, error) {
	// Unconditional so later per-tool checks never hit an unprepared context;
	// no-op for callers RBAC never enforces (AGE-2672).
	ctx, err := s.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, nil, nil, nil, oops.E(oops.CodeUnexpected, err, "load access grants").LogError(ctx, logger)
	}

	rows, err := metamcprepo.New(s.db).ListServableMetaMCPMembers(ctx, metamcprepo.ListServableMetaMCPMembersParams{
		MetaMcpServerID: metaServerID,
		ProjectID:       projectID,
	})
	if err != nil {
		return ctx, nil, nil, nil, oops.E(oops.CodeUnexpected, err, "list meta mcp members").LogError(ctx, logger)
	}

	candidates := make([]metaMemberCandidate, 0, len(rows))
	var hostedSlugs []string
	for _, row := range rows {
		slug := conv.PtrValOr(conv.FromPGText[string](row.McpServerSlug), "")
		if err := inv.Check("gateway member", "member is not a hosted server", !row.McpServerToolsetID.Valid); err != nil {
			logger.WarnContext(ctx, "refuse hosted gateway member", attr.SlogMcpServerID(row.McpServerID.String()), attr.SlogError(err))
			hostedSlugs = append(hostedSlugs, slug)
			continue
		}
		candidates = append(candidates, metaMemberCandidate{
			serverID: row.McpServerID,
			// A stored gateway's members are all in its own project, which the
			// membership query already constrains. Its slugs are unique within
			// that project, so they need no qualifying prefix.
			projectID:                  projectID,
			projectSlug:                "",
			slug:                       slug,
			name:                       conv.PtrValOr(conv.FromPGText[string](row.McpServerName), ""),
			sortOrder:                  row.SortOrder,
			visibility:                 row.McpServerVisibility,
			remoteServerID:             row.McpServerRemoteMcpServerID,
			tunneledServerID:           row.McpServerTunneledMcpServerID,
			upstreamIssuer:             memberUpstreamIssuer(row.McpServerRemoteSessionIssuerID, row.McpServerAssociationProviderIds),
			tunneledResourceIdentifier: row.TunneledResourceIdentifier,
		})
	}

	ctx, members, denied, err := s.admitMetaMembers(ctx, logger, surface, organizationID, candidates)
	return ctx, members, denied, hostedSlugs, err
}

// resolveEndpointMemberSnapshot resolves the members an OAuth flow for a
// gateway endpoint authorizes against: a stored gateway's membership rows, or
// an organization gateway's derived members.
func (s *Service) resolveEndpointMemberSnapshot(ctx context.Context, logger *slog.Logger, surface string, endpoint *ResolvedMcpEndpoint) (context.Context, []metaMember, error) {
	if endpoint.IsOrganizationGateway() {
		ctx, members, _, err := s.resolveOrganizationMemberSnapshot(ctx, logger, surface, endpoint.OrganizationID, endpoint.UserSessionIssuerID, endpoint.ProjectID)
		return ctx, members, err
	}
	ctx, members, _, _, err := s.resolveMetaMemberSnapshot(ctx, logger, surface, endpoint.OrganizationID, endpoint.MetaMcpServerID.UUID, endpoint.ProjectID)
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
		ctx, members, gate.deniedMembers, gate.hostedMemberSlugs, err = s.resolveMetaMemberSnapshot(ctx, logger, mandatorySurfaceGatewayMember, gate.organizationID, metaServerID, projectID)
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
		hashes, otherHashes, hashErr := s.catalogMemberConfigurations(ctx, gate.organizationID, ids)
		if hashErr != nil {
			return ctx, nil, hashErr
		}
		for i := range members {
			members[i].configuration = hashes[members[i].serverID]
			members[i].otherConfiguration = otherHashes[members[i].serverID]
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
// userSessionIssuerID limits legacy sign-in eligibility to the gateway's clients;
// uuid.Nil skips issuer matching for the agent gateway and Platform MCP.
// Enforced typed offers replace this check for members; pending setup retains
// it. A nonzero scopeProjectID restricts candidates before admission and
// credential association for project gateways. The query returns no hosted
// servers, which are never gateway members.
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
			remoteServerID:             row.McpServerRemoteMcpServerID,
			tunneledServerID:           row.McpServerTunneledMcpServerID,
			upstreamIssuer:             memberUpstreamIssuer(row.McpServerRemoteSessionIssuerID, row.McpServerAssociationProviderIds),
			tunneledResourceIdentifier: row.TunneledResourceIdentifier,
		})
	}

	ctx, members, denied, err := s.admitMetaMembers(ctx, logger, authzSurface, organizationID, candidates)
	if err != nil {
		return ctx, nil, nil, err
	}
	// Only the signed-in organization gateway uses typed capability here.
	// Issuerless agent gateways retain their legacy rules.
	var offers map[uuid.UUID]configuredAuthOffer
	var offerMetadata gatewayOfferMetadata
	if userSessionIssuerID != uuid.Nil {
		enforced, err := s.consentAuthChoicesEnforced(ctx, organizationID)
		if err != nil {
			return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "check gateway auth client mode").LogError(ctx, logger)
		}
		if enforced {
			ids := make([]uuid.UUID, 0, len(members))
			for _, member := range members {
				ids = append(ids, member.serverID)
			}
			offers, err = s.configuredAuthOffers(ctx, organizationID, ids)
			if err != nil {
				return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "load gateway auth offers").LogError(ctx, logger)
			}
		}
	}
	if offers != nil {
		offerMetadata, err = s.loadGatewayOfferMetadata(ctx, organizationID, members, offers)
		if err != nil {
			return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "load gateway auth metadata").LogError(ctx, logger)
		}
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
	eligible := make([]metaMember, 0, len(members))
	for _, member := range members {
		offer, known := offers[member.serverID]
		if offers != nil {
			if !known {
				continue
			}
			// Without a live offer, retain legacy transport and issuer checks,
			// including after an auth-client link was deleted (pending=false).
			if len(offer.clients) > 0 {
				supported := gatewayOfferCapability(member, offer, offerMetadata)
				if supported {
					eligible = append(eligible, member)
				}
				continue
			}
		}
		var supported, needsOAuth bool
		if known && !offer.pending {
			// Check detached offers against legacy source headers without decrypting credentials.
			supported, needsOAuth = remoteMemberAuth(offerMetadata.source[member.remoteServerID.UUID], memberNeedsSignIn(member))
		} else {
			supported, needsOAuth, err = s.derivedMemberAuth(ctx, member)
			if err != nil {
				return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "check gateway member authentication").LogError(ctx, logger)
			}
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
	fromAssociations := feature.UpstreamIssuerFromAssociations(ctx, s.features, organizationID)
	members := make([]metaMember, 0, len(candidates))
	var denied []metaMember
	for _, candidate := range candidates {
		// Only remote and tunneled backends have a gateway dispatch path.
		if !candidate.remoteServerID.Valid && !candidate.tunneledServerID.Valid {
			continue
		}

		// Visibility gates exposure and fails closed: public members are open,
		// private members require mcp:connect on the member server (as
		// authorizeProxyBackendAccess) with denied members filtered so
		// unauthorized reads as nonexistent, and any other value (malformed or
		// future) is filtered the same way.
		//
		// Admission records no authorization check: it filters every server
		// the caller could reach, not one it asked for. The check is recorded
		// when a call names the member (findNamedMember), so denied members
		// are kept aside for that.
		connect := memberConnectCheck(candidate.serverID, candidate.projectID)
		refusal, err := s.memberConnectDecision(authz.WithoutChallenges(ctx), shadow, surface, candidate.visibility, connect)
		if err != nil {
			return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "check member authz").LogError(ctx, logger)
		}

		member := metaMember{
			serverID:                       candidate.serverID,
			configuration:                  memberConfiguration{hash: "", typedHash: ""},
			otherConfiguration:             memberConfiguration{hash: "", typedHash: ""},
			authClient:                     nil,
			projectID:                      candidate.projectID,
			projectSlug:                    candidate.projectSlug,
			slug:                           candidate.slug,
			name:                           candidate.name,
			sortOrder:                      candidate.sortOrder,
			remoteServerID:                 candidate.remoteServerID,
			tunneledServerID:               candidate.tunneledServerID,
			visibility:                     candidate.visibility,
			remoteSessionIssuerID:          candidate.upstreamIssuer.authoritative(fromAssociations),
			upstreamIssuer:                 candidate.upstreamIssuer,
			upstreamIssuerFromAssociations: fromAssociations,
			tunneledResourceIdentifier:     candidate.tunneledResourceIdentifier,
			chosenClientID:                 uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			upstreamNoAuth:                 false,
			upstreamUnavailable:            nil,
			typedAuthClient:                false,
			associatedClients:              nil,
			typedRegistrations:             false,
		}
		switch {
		case refusal == nil:
			members = append(members, member)
			s.recordMemberIssuerParity(ctx, logger, organizationID, member)
		case candidate.visibility == mcpservers.VisibilityPrivate:
			// Kept aside so a call that names it records its denied check.
			// Any other visibility (malformed or future) is simply dropped.
			denied = append(denied, member)
		}
	}

	return ctx, members, denied, nil
}

// memberConnectDecision is the one connect decision for a server a caller
// reaches through another surface: a gateway member, a consent candidate, or a
// proxied backend. A public server is open. A
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
	if requireErr != nil && !isAccessDenial(requireErr) {
		return nil, fmt.Errorf("require member connect: %w", requireErr)
	}
	shadow.recordServer(ctx, surface, requireErr == nil, authz.MandatoryServerPrivate, connect)
	return requireErr, nil
}

// memberConnectCheck is the mcp:connect check a member's grants answer,
// keyed on the member's mcp_servers id as the dashboard writes it for proxy
// backends (grantResourceIdForMcpServer).
func memberConnectCheck(serverID uuid.UUID, projectID uuid.UUID) authz.Check {
	return authz.MCPCheck(authz.ScopeMCPConnect, serverID.String(), projectID.String())
}

// findNamedMember resolves the server a call names and records the one
// authorization check the request makes for it: allowed for an admitted or
// hidden private member, denied for a member admission turned away. Admitted
// public members need no grant and record nothing here. A slug that names no
// server records nothing.
func (s *Service) findNamedMember(ctx context.Context, gate *metaGateContext, members []metaMember, slug string) (metaMember, bool) {
	member, ok := findMetaMember(members, slug)
	if ok {
		s.recordNamedPrivateMemberCheck(ctx, member)
		return member, true
	}
	// A turned-away member was refused its own connect, so this records the
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
	_ = s.authz.Require(ctx, memberConnectCheck(member.serverID, member.projectID))
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
