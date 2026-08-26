// Per-member credential selection for gateway endpoints. A caller
// authenticates once at a gateway, so every upstream credential they hold
// hangs off the gateway's user_session_issuer and the per-client derivation
// that qualifies a normal endpoint's grants derives nothing. This resolves the
// member a connecting client belongs to at consent time and records that
// member's URL as the grant's RFC 8707 resource, so routeUpstreamToken can
// route by it unchanged.

package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
)

// resolveGatewayMemberResource returns the upstream URL of the gateway member
// whose upstream authenticates against remoteSessionIssuerID, or "" when no
// single member can be identified.
//
// The lookup is a join, not a discovery call: a remote_session_client names
// exactly one remote_session_issuer, and mcp_servers records the issuer its
// upstream authenticates against, so the member falls out of the data Gram
// already holds. Probing each member's RFC 9728 metadata would answer the same
// question over the network, but that document is optional, its contents can
// change without Gram knowing, and a mismatch would surface as a token-routing
// failure rather than a configuration one.
//
// Fails closed: no match and several members leave "", which the connect arm
// treats exactly as an underivable resource. A NULL remote_session_issuer_id
// simply matches nothing, so a server the sync has not reached yet resolves to
// "" and behaves as it does today.
//
// The returned error is a database or grant-load fault only; the connect arm
// turns it into a failed consent rather than minting an unqualified grant.
func (s *Service) resolveGatewayMemberResource(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	remoteSessionIssuerID uuid.UUID,
) (string, error) {
	if remoteSessionIssuerID == uuid.Nil {
		return "", nil
	}

	rows, err := metamcprepo.New(s.db).ListGatewayMembersForRemoteSessionIssuer(ctx, metamcprepo.ListGatewayMembersForRemoteSessionIssuerParams{
		MetaMcpServerID:       endpoint.MetaMcpServerID.UUID,
		ProjectID:             endpoint.ProjectID,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: remoteSessionIssuerID, Valid: true},
	})
	if err != nil {
		return "", fmt.Errorf("list gateway members for remote session issuer: %w", err)
	}

	candidates, err := s.authorizedGatewayMembers(ctx, endpoint, rows)
	if err != nil {
		return "", err
	}

	resource := ""
	for _, row := range candidates {
		upstream := strings.TrimRight(row.UpstreamUrl, "/")
		switch {
		// Two members may front one URL: remote_mcp_servers is unique on
		// (project_id, slug), not on url. routeUpstreamToken keys on the
		// destination URL, so a token minted for it serves either member.
		case upstream == "", upstream == resource:
		case resource == "":
			resource = upstream
		default:
			// One authorization server fronting two members of the same
			// gateway. A grant records one resource per (subject, client), so
			// there is nothing to record that would route both correctly.
			logger.WarnContext(ctx, "gateway members share an authorization server; credential cannot be qualified to one member",
				attr.SlogMetaMcpServerID(endpoint.MetaMcpServerID.UUID.String()),
				attr.SlogOAuthIssuer(remoteSessionIssuerID.String()),
			)
			return "", nil
		}
	}
	return resource, nil
}

// authorizedGatewayMembers drops the members the connecting subject holds no
// mcp:connect on, as resolveMetaMemberSnapshot does for the serving path. A
// member the caller cannot reach must neither claim their credential — the
// resolved URL is echoed to them and sent to the authorization server — nor
// contest the claim of a member they can.
func (s *Service) authorizedGatewayMembers(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	rows []metamcprepo.ListGatewayMembersForRemoteSessionIssuerRow,
) ([]metamcprepo.ListGatewayMembersForRemoteSessionIssuerRow, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	ctx, err := s.authz.PrepareContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("load access grants: %w", err)
	}

	authorized := make([]metamcprepo.ListGatewayMembersForRemoteSessionIssuerRow, 0, len(rows))
	for _, row := range rows {
		// Only proxy backends reach here, so mcp:connect is keyed on the
		// mcp_servers id (see grantResourceIdForMcpServer). Any visibility
		// other than the two known values fails closed.
		switch row.McpServerVisibility {
		case mcpservers.VisibilityPublic:
		case mcpservers.VisibilityPrivate:
			if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, row.McpServerID.String(), endpoint.ProjectID.String())); err != nil {
				continue
			}
		default:
			continue
		}
		authorized = append(authorized, row)
	}
	return authorized, nil
}
