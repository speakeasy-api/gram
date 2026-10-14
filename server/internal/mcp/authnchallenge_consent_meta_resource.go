// Per-member credential selection for meta MCP endpoints: resolves the member a
// connecting client belongs to at consent time and records its upstream
// resource (remote server URL or tunneled resource identifier) as the grant's
// RFC 8707 resource, which routeUpstreamToken routes by unchanged.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// chosenMetaMember is the member whose spelling of an upstream URL a
// credential is qualified to.
type chosenMetaMember struct {
	// serverID is the member's mcp_servers id, invalid when no member was
	// chosen.
	serverID uuid.NullUUID

	// projectID is the member's own project.
	projectID uuid.UUID
}

// resolveMetaMemberResource returns the upstream resource (remote server URL
// or tunneled resource identifier) of the meta MCP member whose upstream
// authenticates against the client's authorization server, and the member
// whose spelling of that URL was chosen.
//
// Tri-state: ("", true) means members claimed the issuer but no single member
// wins, and the caller must not fall back to a weaker derivation; ("", false)
// is a genuine no-match, the only case where the stored per-client derivation
// may answer. A NULL remote_session_issuer_id matches nothing. The error is a
// database or grant-load fault only, and the connect fails closed on it.
func (s *Service) resolveMetaMemberResource(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	client remotesessions.Client,
) (string, chosenMetaMember, bool, error) {
	none := chosenMetaMember{serverID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, projectID: uuid.Nil}
	candidates, claimed, err := s.claimingMetaMembers(ctx, endpoint, client)
	if err != nil || !claimed {
		return "", none, false, err
	}

	// The resource is sent upstream verbatim: a provider may match it exactly
	// against its RFC 9728 resource, trailing slash included. Trailing slashes
	// are ignored only to decide whether members share a destination.
	resource := ""
	member := none
	for _, row := range candidates {
		upstream := row.UpstreamUrl
		trimmed := strings.TrimRight(upstream, "/")
		switch {
		case trimmed == "":
		// Two members may front one URL — remote_mcp_servers is unique on
		// (project_id, slug), not url — and a token keyed on that URL serves
		// either. Spellings differing only in trailing slashes resolve to the
		// shortest, so the result does not depend on member order.
		case resource != "" && trimmed == strings.TrimRight(resource, "/"):
			if len(upstream) < len(resource) {
				resource, member = upstream, chosenMetaMember{serverID: uuid.NullUUID{UUID: row.McpServerID, Valid: true}, projectID: row.ProjectID}
			}
		case resource == "":
			resource, member = upstream, chosenMetaMember{serverID: uuid.NullUUID{UUID: row.McpServerID, Valid: true}, projectID: row.ProjectID}
		default:
			// One authorization server, two members: a grant records one resource per
			// (subject, client), so nothing routes both.
			logger.WarnContext(ctx, "meta MCP members share an authorization server; credential cannot be qualified to one member",
				attr.SlogMetaMcpServerID(endpoint.MetaMcpServerID.UUID.String()),
				attr.SlogRemoteSessionIssuerID(client.RemoteSessionIssuerID.String()),
				attr.SlogMcpServerID(row.McpServerID.String()),
			)
			return "", none, true, nil
		}
	}
	return resource, member, true, nil
}

// claimingMetaMembers lists the proxied members authenticating against the
// client's authorization server, filtered to those the subject may reach.
// Claimed is decided before RBAC: an invisible member still claimed the
// credential.
//
// An organization gateway's members span projects, so a client that a
// project owns is claimed only by that project's members, the ones whose
// dispatch routes through it.
func (s *Service) claimingMetaMembers(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	client remotesessions.Client,
) ([]metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow, bool, error) {
	if client.RemoteSessionIssuerID == uuid.Nil {
		return nil, false, nil
	}

	var rows []metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow
	if endpoint.IsOrganizationGateway() {
		organizationRows, err := metamcprepo.New(s.db).ListOrganizationMCPServersForRemoteSessionIssuer(ctx, metamcprepo.ListOrganizationMCPServersForRemoteSessionIssuerParams{
			OrganizationID:        endpoint.OrganizationID,
			ProjectID:             client.ProjectID,
			RemoteSessionIssuerID: uuid.NullUUID{UUID: client.RemoteSessionIssuerID, Valid: true},
		})
		if err != nil {
			return nil, false, fmt.Errorf("list organization MCP servers for remote session issuer: %w", err)
		}
		// Only servers the gateway serves on this surface claim, as only they
		// are members of it here.
		surface := requestNetworkSurface(ctx)
		for _, row := range organizationRows {
			if servableOnSurface(row.McpServerNetworkAccessMode, surface) {
				rows = append(rows, metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow(row))
			}
		}
	} else {
		storedRows, err := metamcprepo.New(s.db).ListMetaMCPMembersForRemoteSessionIssuer(ctx, metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerParams{
			MetaMcpServerID:       endpoint.MetaMcpServerID.UUID,
			ProjectID:             endpoint.ProjectID,
			RemoteSessionIssuerID: uuid.NullUUID{UUID: client.RemoteSessionIssuerID, Valid: true},
		})
		if err != nil {
			return nil, false, fmt.Errorf("list meta MCP members for remote session issuer: %w", err)
		}
		rows = storedRows
	}
	if len(rows) == 0 {
		return nil, false, nil
	}

	candidates, err := s.authorizedMetaMembers(ctx, endpoint, rows)
	if err != nil {
		return nil, false, err
	}
	return candidates, true, nil
}

// authorizedMetaMembers drops members the subject holds no mcp:connect on,
// mirroring authorizeProxyBackendAccess.
func (s *Service) authorizedMetaMembers(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	rows []metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow,
) ([]metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	ctx, err := s.authz.PrepareContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("load access grants: %w", err)
	}

	authorized := make([]metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow, 0, len(rows))
	for _, row := range rows {
		// Only proxy backends reach here, so mcp:connect is keyed on the
		// mcp_servers id. Unknown visibility fails closed.
		switch row.McpServerVisibility {
		case mcpservers.VisibilityPublic:
		case mcpservers.VisibilityPrivate:
			// Only a denial drops a member (Unauthorized is an anonymous
			// caller, as the runtime snapshot treats it); dropping on a fault
			// narrows the candidate set and turns ambiguous into a confident
			// wrong answer.
			if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, row.McpServerID.String(), row.ProjectID.String())); err != nil {
				if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && (shareable.Code == oops.CodeForbidden || shareable.Code == oops.CodeUnauthorized) {
					continue
				}
				return nil, fmt.Errorf("authorize meta MCP member access: %w", err)
			}
		default:
			// Not redundant with the query's one-named-value visibility filter.
			continue
		}
		authorized = append(authorized, row)
	}
	return authorized, nil
}

var quietLogger = slog.New(slog.DiscardHandler)

// gatewayMemberResourceScopes reads, for each card on a gateway, the cached
// resource row of the member a login through that client would read, so the
// card requests what the login will. A client no single member claims has no
// entry; a lookup fault logs and leaves the card on its issuer's scopes.
func (s *Service) gatewayMemberResourceScopes(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	clients []remotesessions.Client,
	discover bool,
) map[uuid.UUID]remotesessions.ResourceScopes {
	if !discover || !endpoint.MetaMcpServerID.Valid || challengeState.Subject == nil || len(clients) == 0 {
		return nil
	}
	memberCtx, err := s.contextForSessionSubject(ctx, endpoint, *challengeState.Subject, "consent:"+challengeState.ID, challengeState.ClientID)
	if err != nil {
		s.logger.WarnContext(ctx, "stamp consent subject for gateway member scopes", attr.SlogError(err))
		return nil
	}
	// Clients on one authorization server, owned by the same project, resolve
	// to the same member, so each pair is resolved once per render. The
	// project matters on an organization gateway, where a project's client is
	// claimed only by that project's members.
	type claimKey struct {
		remoteSessionIssuerID uuid.UUID
		projectID             uuid.UUID
	}
	byClaim := make(map[claimKey]memberScopes, len(clients))
	out := make(map[uuid.UUID]remotesessions.ResourceScopes, len(clients))
	for _, c := range clients {
		key := claimKey{remoteSessionIssuerID: c.RemoteSessionIssuerID, projectID: c.ProjectID.UUID}
		resolved, seen := byClaim[key]
		if !seen {
			resolved = s.gatewayMemberScopes(ctx, memberCtx, endpoint, c)
			byClaim[key] = resolved
		}
		if resolved.ok {
			out[c.ID] = resolved.scopes
		}
	}
	return out
}

// memberScopes is one authorization server's gateway member row, if any.
type memberScopes struct {
	scopes remotesessions.ResourceScopes
	ok     bool
}

// gatewayMemberScopes reads the cached resource row of the member a login
// through c would read. A lookup fault logs and reports none.
func (s *Service) gatewayMemberScopes(ctx, memberCtx context.Context, endpoint *ResolvedMcpEndpoint, c remotesessions.Client) memberScopes {
	none := memberScopes{scopes: remotesessions.ResourceScopes{Pin: nil, ChallengeScopes: nil, ScopesSupported: nil, Live: false, UseDiscovered: false}, ok: false}
	// The connect logs a shared authorization server; every render need not.
	resource, member, _, err := s.resolveMetaMemberResource(memberCtx, quietLogger, endpoint, c)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve gateway member for consent card", attr.SlogError(err), attr.SlogRemoteSessionClientID(c.ID.String()))
		return none
	}
	if !member.serverID.Valid {
		return none
	}
	scopes, resourceURL, ok := s.remoteChallengeMgr.CachedResourceScopesForServer(ctx, member.projectID, member.serverID, true)
	if !ok || strings.TrimRight(resourceURL, "/") != strings.TrimRight(resource, "/") {
		return none
	}
	return memberScopes{scopes: scopes, ok: true}
}
