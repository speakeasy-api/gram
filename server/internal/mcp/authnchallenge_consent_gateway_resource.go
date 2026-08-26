// Per-member credential selection for gateway endpoints. A caller authenticates
// once at a gateway, so every credential hangs off the gateway's
// user_session_issuer and the per-client derivation qualifies nothing. This
// resolves the member a connecting client belongs to at consent time and records
// its URL as the grant's RFC 8707 resource, so routeUpstreamToken routes by it
// unchanged.

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
)

// resolveGatewayMemberResource returns the upstream URL of the gateway member
// whose upstream authenticates against remoteSessionIssuerID, or "" when no
// single member can be identified.
//
// A join, not a discovery call: a remote_session_client names exactly one
// remote_session_issuer and mcp_servers records the issuer its upstream
// authenticates against, so the member falls out of data Gram already holds.
//
// The second return reports whether any member claimed the issuer, a different
// question from "is the resource non-empty". An ambiguous gateway answers
// ("", true): declining to qualify is a decision, and the caller must not then
// reach for a weaker signal. Only a genuine no-match lets the stored per-client
// derivation answer instead.
//
// Fails closed — no match and several members both leave "". A NULL
// remote_session_issuer_id matches nothing, so a server the sync has not reached
// behaves as it does today. The error is a database or grant-load fault only.
func (s *Service) resolveGatewayMemberResource(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	remoteSessionIssuerID uuid.UUID,
) (string, bool, error) {
	if remoteSessionIssuerID == uuid.Nil {
		return "", false, nil
	}

	rows, err := metamcprepo.New(s.db).ListGatewayMembersForRemoteSessionIssuer(ctx, metamcprepo.ListGatewayMembersForRemoteSessionIssuerParams{
		MetaMcpServerID:       endpoint.MetaMcpServerID.UUID,
		ProjectID:             endpoint.ProjectID,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: remoteSessionIssuerID, Valid: true},
	})
	if err != nil {
		return "", false, fmt.Errorf("list gateway members for remote session issuer: %w", err)
	}
	// Claimed before RBAC: a member the caller cannot see still claimed this
	// credential, and letting the fallback qualify it elsewhere would hand it to a
	// member that never claimed it.
	if len(rows) == 0 {
		return "", false, nil
	}

	candidates, err := s.authorizedGatewayMembers(ctx, endpoint, rows)
	if err != nil {
		return "", false, err
	}

	resource := ""
	for _, row := range candidates {
		upstream := strings.TrimRight(row.UpstreamUrl, "/")
		switch {
		// Two members may front one URL — remote_mcp_servers is unique on
		// (project_id, slug), not url — and a token keyed on that URL serves either.
		case upstream == "", upstream == resource:
		case resource == "":
			resource = upstream
		default:
			// One authorization server, two members: a grant records one resource per
			// (subject, client), so nothing routes both.
			logger.WarnContext(ctx, "gateway members share an authorization server; credential cannot be qualified to one member",
				attr.SlogMetaMcpServerID(endpoint.MetaMcpServerID.UUID.String()),
				attr.SlogRemoteSessionIssuerID(remoteSessionIssuerID.String()),
				attr.SlogMcpServerID(row.McpServerID.String()),
			)
			return "", true, nil
		}
	}
	return resource, true, nil
}

// authorizedGatewayMembers drops members the subject holds no mcp:connect on,
// mirroring authorizeProxyBackendAccess. A member the caller cannot reach must
// neither claim their credential — the resolved URL is echoed back to them —
// nor contest the claim of one they can.
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
		// mcp_servers id. Unknown visibility fails closed.
		switch row.McpServerVisibility {
		case mcpservers.VisibilityPublic:
		case mcpservers.VisibilityPrivate:
			// Only a denial drops a member. A fault must not: dropping narrows the
			// candidate set, which is how two members that should read as ambiguous become
			// one confident answer.
			if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, row.McpServerID.String(), endpoint.ProjectID.String())); err != nil {
				if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeForbidden {
					continue
				}
				return nil, fmt.Errorf("authorize gateway member access: %w", err)
			}
		default:
			// Not redundant with the query's visibility filter: that excludes one named
			// value, this excludes every value neither arm can judge.
			continue
		}
		authorized = append(authorized, row)
	}
	return authorized, nil
}
