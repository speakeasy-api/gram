// Consent-time routability of stored grants: a remote_session is shared by
// every endpoint bound to its client, so a live one may never be forwarded
// by this endpoint's backend. Mirrors grantRoutesToUpstream plus the remote
// duplicate refusal, per card.

package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// grantRoutability reports whether the runtime would refuse to forward a
// card's live grant. Cards no resource rule applies to are never unroutable.
type grantRoutability func(client remotesessions.Client, resource string) (unroutable bool, err error)

func alwaysRoutable(remotesessions.Client, string) (bool, error) {
	return false, nil
}

// grantRoutabilityForEndpoint judges a meta endpoint's cards against the
// members claiming their authorization server (visibility resolved for the
// consent subject), and a proxied single server against its one upstream.
func (s *Service) grantRoutabilityForEndpoint(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	statuses map[uuid.UUID]remotesessions.RemoteSessionState,
) (grantRoutability, error) {
	duplicated := func(upstream string) bool {
		n := 0
		for _, st := range statuses {
			if st.Status == remotesessions.RemoteSessionActive && grantRoutesToUpstream(st.Resource, upstream, false) {
				n++
			}
		}
		return n > 1
	}

	switch {
	case endpoint.MetaMcpServerID.Valid:
		memberCtx, err := s.contextForSessionSubject(ctx, endpoint, *challengeState.Subject, "consent:"+challengeState.ID, challengeState.ClientID)
		if err != nil {
			return nil, fmt.Errorf("stamp consent subject context: %w", err)
		}
		return func(client remotesessions.Client, resource string) (bool, error) {
			members, _, err := s.claimingMetaMembers(memberCtx, endpoint, client.RemoteSessionIssuerID)
			if err != nil || len(members) == 0 {
				return false, err
			}
			for _, m := range members {
				if grantRoutesToUpstream(resource, m.UpstreamUrl, m.Tunneled) && (m.Tunneled || !duplicated(m.UpstreamUrl)) {
					return false, nil
				}
			}
			return true, nil
		}, nil

	case endpoint.McpServerID.Valid:
		server, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
			ID:        endpoint.McpServerID.UUID,
			ProjectID: endpoint.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("load mcp server for consent routing: %w", err)
		}
		switch {
		case server.RemoteMcpServerID.Valid:
			return func(_ remotesessions.Client, resource string) (bool, error) {
				return !grantRoutesToUpstream(resource, endpoint.UpstreamResource, false) || duplicated(endpoint.UpstreamResource), nil
			}, nil
		case server.TunneledMcpServerID.Valid:
			// A tunneled backend only reads the entry keyed by its own issuer.
			issuer := tunneledBackendIssuer(&server)
			return func(client remotesessions.Client, resource string) (bool, error) {
				if !issuer.Valid || client.RemoteSessionIssuerID != issuer.UUID {
					return false, nil
				}
				return !grantRoutesToUpstream(resource, endpoint.UpstreamResource, true), nil
			}, nil
		}
	}
	return alwaysRoutable, nil
}
