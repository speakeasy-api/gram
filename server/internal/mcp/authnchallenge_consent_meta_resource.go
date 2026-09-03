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
)

// resolveMetaMemberResource returns the upstream resource (remote server URL
// or tunneled resource identifier) of the meta MCP member whose upstream
// authenticates against remoteSessionIssuerID.
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
	remoteSessionIssuerID uuid.UUID,
) (string, bool, error) {
	candidates, claimed, err := s.claimingMetaMembers(ctx, endpoint, remoteSessionIssuerID)
	if err != nil || !claimed {
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
			logger.WarnContext(ctx, "meta MCP members share an authorization server; credential cannot be qualified to one member",
				attr.SlogMetaMcpServerID(endpoint.MetaMcpServerID.UUID.String()),
				attr.SlogRemoteSessionIssuerID(remoteSessionIssuerID.String()),
				attr.SlogMcpServerID(row.McpServerID.String()),
			)
			return "", true, nil
		}
	}
	return resource, true, nil
}

// claimingMetaMembers lists the proxied members authenticating against
// remoteSessionIssuerID, filtered to those the subject may reach. Claimed is
// decided before RBAC: an invisible member still claimed the credential.
func (s *Service) claimingMetaMembers(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	remoteSessionIssuerID uuid.UUID,
) ([]metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow, bool, error) {
	if remoteSessionIssuerID == uuid.Nil {
		return nil, false, nil
	}

	rows, err := metamcprepo.New(s.db).ListMetaMCPMembersForRemoteSessionIssuer(ctx, metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerParams{
		MetaMcpServerID:       endpoint.MetaMcpServerID.UUID,
		ProjectID:             endpoint.ProjectID,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: remoteSessionIssuerID, Valid: true},
	})
	if err != nil {
		return nil, false, fmt.Errorf("list meta MCP members for remote session issuer: %w", err)
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
			if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, row.McpServerID.String(), endpoint.ProjectID.String())); err != nil {
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
