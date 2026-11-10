// Consent-time routability of stored grants: a remote_session is shared by
// every endpoint bound to its client, so a live one may never be forwarded
// by this endpoint's backend. Mirrors grantRoutesToUpstream, per card. A
// direct proxied server lists only its associated clients, and a gateway card
// routes only through the members associated with its client. Serving
// resolves only the client a credential chose, so another client's grant for
// the same upstream is never a competing candidate. Endpoints without a
// proxied backend apply no resource rule.

package mcp

import (
	"context"
	"fmt"

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
	upstream string // the single proxied server's upstream
	// members claiming each client's grant, keyed by remote session client,
	// meta only.
	members map[uuid.UUID][]metamcprepo.ListMetaMCPMembersForRemoteSessionClientRow
}

// unroutable reports whether the runtime would refuse to forward a card's
// live grant.
func (r consentRouting) unroutable(client remotesessions.Client, resource string) bool {
	switch r.backend {
	case consentBackendRemote:
		return !grantRoutesToUpstream(resource, r.upstream, false)
	case consentBackendTunneled:
		// A tunneled backend routes the chosen client's grant by that
		// client's own issuer.
		return !grantRoutesToUpstream(resource, r.upstream, true)
	case consentBackendMeta:
		members := r.members[client.ID]
		if len(members) == 0 {
			return false
		}
		for _, m := range members {
			if grantRoutesToUpstream(resource, m.UpstreamUrl, m.Tunneled) {
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
	case consentBackendRemote, consentBackendTunneled:
	case consentBackendMeta:
		if len(r.members[client.ID]) == 0 {
			return false
		}
	case consentBackendNone:
		return false
	}
	return !r.unroutable(client, resource)
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
	r := consentRouting{backend: consentBackendNone, upstream: endpoint.UpstreamResource, members: nil}
	switch {
	case endpoint.MetaMcpServerID.Valid:
		memberCtx, err := s.contextForSessionSubject(ctx, endpoint, *challengeState.Subject, "consent:"+challengeState.ID, challengeState.ClientID)
		if err != nil {
			return r, fmt.Errorf("stamp consent subject context: %w", err)
		}
		r.backend = consentBackendMeta
		r.members = make(map[uuid.UUID][]metamcprepo.ListMetaMCPMembersForRemoteSessionClientRow, len(clients))
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
		}
	}
	return r, nil
}
