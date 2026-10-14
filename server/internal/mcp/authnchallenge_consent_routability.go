// Consent-time routability of stored grants: a remote_session is shared by
// every endpoint bound to its client, so a live one may never be forwarded
// by this endpoint's backend. Mirrors grantRoutesToUpstream plus the remote
// duplicate refusal, per card.

package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

type consentBackend int

const (
	// consentBackendNone applies no resource rule (hosted, unresolvable).
	consentBackendNone consentBackend = iota
	consentBackendRemote
	consentBackendTunneled
	consentBackendMeta
)

// consentRouting is the endpoint's credential-routing rule, resolved once per
// render so every card is judged from plain values.
type consentRouting struct {
	backend  consentBackend
	upstream string        // the single proxied server's upstream
	issuer   uuid.NullUUID // a tunneled server's derived issuer
	// members claiming each client's grant, keyed by remote session client,
	// meta only.
	members map[uuid.UUID][]metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow
	// active grants per normalized remote upstream; a remote backend refuses
	// when more than one names it.
	grants map[consentGrantKey]int
	// projectScoped counts grants per project, for an organization gateway,
	// whose members each route only their own project's clients.
	projectScoped bool
}

// consentGrantKey counts active grants per upstream within one project.
// Unscoped routing and organization-level clients use uuid.Nil.
type consentGrantKey struct {
	projectID uuid.UUID
	upstream  string
}

// unroutable reports whether the runtime would refuse to forward a card's
// live grant.
func (r consentRouting) unroutable(client remotesessions.Client, resource string) bool {
	switch r.backend {
	case consentBackendRemote:
		return !grantRoutesToUpstream(resource, r.upstream, false) || r.duplicated(uuid.Nil, r.upstream)
	case consentBackendTunneled:
		// A tunneled backend only reads the entry keyed by its own issuer.
		return r.issuer.Valid && client.RemoteSessionIssuerID == r.issuer.UUID && !grantRoutesToUpstream(resource, r.upstream, true)
	case consentBackendMeta:
		members := r.members[client.ID]
		if len(members) == 0 {
			return false
		}
		for _, m := range members {
			if grantRoutesToUpstream(resource, m.UpstreamUrl, m.Tunneled) && (m.Tunneled || !r.duplicated(m.ProjectID, m.UpstreamUrl)) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// canValidate distinguishes an unused connection from a routable probe target.
func (r consentRouting) canValidate(client remotesessions.Client, resource string) bool {
	switch r.backend {
	case consentBackendRemote:
	case consentBackendTunneled:
		if !r.issuer.Valid || r.issuer.UUID != client.RemoteSessionIssuerID {
			return false
		}
	case consentBackendMeta:
		if len(r.members[client.ID]) == 0 {
			return false
		}
	case consentBackendNone:
		return false
	}
	return !r.unroutable(client, resource)
}

// duplicated reports whether more than one active grant names upstream where
// projectID's members route. An organization-level client's grant routes in
// every project, so it counts in each.
func (r consentRouting) duplicated(projectID uuid.UUID, upstream string) bool {
	upstream = strings.TrimRight(upstream, "/")
	if !r.projectScoped {
		projectID = uuid.Nil
	}
	n := r.grants[consentGrantKey{projectID: projectID, upstream: upstream}]
	if projectID != uuid.Nil {
		n += r.grants[consentGrantKey{projectID: uuid.Nil, upstream: upstream}]
	}
	return n > 1
}

// resolveConsentRouting loads the rule for the endpoint being consented to: a
// meta endpoint's members claiming each provider (visibility resolved for the
// consent subject), or a proxied single server's backend kind.
func (s *Service) resolveConsentRouting(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	clients []remotesessions.Client,
	statuses map[uuid.UUID]remotesessions.RemoteSessionState,
) (consentRouting, error) {
	r := consentRouting{backend: consentBackendNone, upstream: endpoint.UpstreamResource, issuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, members: nil, grants: map[consentGrantKey]int{}, projectScoped: endpoint.IsOrganizationGateway()}
	if r.projectScoped {
		// The organization gateway's statuses span every project; only the
		// clients listed for this subject, each in its own project, decide
		// what routes for them.
		for _, c := range clients {
			if st, ok := statuses[c.ID]; ok && st.Status == remotesessions.RemoteSessionActive && st.Resource != "" {
				r.grants[consentGrantKey{projectID: c.ProjectID.UUID, upstream: strings.TrimRight(st.Resource, "/")}]++
			}
		}
	} else {
		for _, st := range statuses {
			if st.Status == remotesessions.RemoteSessionActive && st.Resource != "" {
				r.grants[consentGrantKey{projectID: uuid.Nil, upstream: strings.TrimRight(st.Resource, "/")}]++
			}
		}
	}

	switch {
	case endpoint.MetaMcpServerID.Valid:
		memberCtx, err := s.contextForSessionSubject(ctx, endpoint, *challengeState.Subject, "consent:"+challengeState.ID, challengeState.ClientID)
		if err != nil {
			return r, fmt.Errorf("stamp consent subject context: %w", err)
		}
		r.backend = consentBackendMeta
		r.members = make(map[uuid.UUID][]metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow, len(clients))
		for _, c := range clients {
			if st, ok := statuses[c.ID]; !ok || st.Status != remotesessions.RemoteSessionActive {
				continue
			}
			members, _, err := s.claimingMetaMembers(memberCtx, endpoint, c)
			if err != nil {
				return r, fmt.Errorf("resolve consent routing: %w", err)
			}
			r.members[c.ID] = members
		}

	case endpoint.McpServerID.Valid:
		server, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
			ID:        endpoint.McpServerID.UUID,
			ProjectID: endpoint.ProjectID,
		})
		if err != nil {
			return r, fmt.Errorf("load mcp server for consent routing: %w", err)
		}
		switch {
		case server.RemoteMcpServerID.Valid:
			r.backend = consentBackendRemote
		case server.TunneledMcpServerID.Valid:
			r.backend = consentBackendTunneled
			r.issuer = tunneledBackendIssuer(&server)
		}
	}
	return r, nil
}
