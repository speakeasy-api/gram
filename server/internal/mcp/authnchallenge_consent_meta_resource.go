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

// resolveMetaMemberResource returns the upstream resource (remote server URL
// or tunneled resource identifier) of the meta MCP member that client
// authenticates.
//
// Tri-state: ("", true) means members claimed the client but no single member
// wins, and the caller must not fall back to a weaker derivation; ("", false)
// is a genuine no-match, the only case where the stored per-client derivation
// may answer. A NULL remote_session_issuer_id matches nothing. The error is a
// database or grant-load fault only, and the connect fails closed on it.
func (s *Service) resolveMetaMemberResource(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	client remotesessions.Client,
) (string, bool, error) {
	candidates, claimed, err := s.claimingMetaMembers(ctx, endpoint, client)
	if err != nil || !claimed {
		return "", false, err
	}

	// The resource is sent upstream verbatim: a provider may match it exactly
	// against its RFC 9728 resource, trailing slash included. Trailing slashes
	// are ignored only to decide whether members share a destination.
	resource := ""
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
				resource = upstream
			}
		case resource == "":
			resource = upstream
		default:
			// One client, two member upstreams: a grant records one resource
			// per (subject, client), so nothing routes both. Each member needs
			// its own client.
			logger.WarnContext(ctx, "meta MCP members share a client; credential cannot be qualified to one member",
				attr.SlogMetaMcpServerID(endpoint.MetaMcpServerID.UUID.String()),
				attr.SlogRemoteSessionIssuerID(client.RemoteSessionIssuerID.String()),
				attr.SlogRemoteSessionClientID(client.ID.String()),
				attr.SlogMcpServerID(row.McpServerID.String()),
			)
			return "", true, nil
		}
	}
	return resource, true, nil
}

// claimingMetaMembers lists the proxied members client authenticates,
// filtered to those the subject may reach. Claimed is decided before RBAC and
// client association: an invisible or differently configured member still
// claimed the credential, so callers must not fall back to a weaker derivation.
func (s *Service) claimingMetaMembers(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	client remotesessions.Client,
) ([]metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow, bool, error) {
	if client.RemoteSessionIssuerID == uuid.Nil {
		return nil, false, nil
	}

	rows, err := metamcprepo.New(s.db).ListMetaMCPMembersForRemoteSessionIssuer(ctx, metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerParams{
		RemoteSessionClientID:      client.ID,
		GatewayUserSessionIssuerID: uuid.NullUUID{UUID: endpoint.UserSessionIssuerID, Valid: endpoint.UserSessionIssuerID != uuid.Nil},
		RemoteSessionIssuerID:      client.RemoteSessionIssuerID,
		MetaMcpServerID:            endpoint.MetaMcpServerID.UUID,
		ProjectID:                  endpoint.ProjectID,
	})
	if err != nil {
		return nil, false, fmt.Errorf("list meta MCP members for remote session issuer: %w", err)
	}
	if len(rows) == 0 {
		return nil, false, nil
	}

	candidates, err := s.authorizedMetaMembers(ctx, endpoint, associatedMetaMembers(rows))
	if err != nil {
		return nil, false, err
	}
	return candidates, true, nil
}

// associatedMetaMembers narrows the members claiming a client's authorization
// server to those configured with that exact client. A gateway binding one
// client per authorization server needs no narrowing, so every claiming
// member stays a candidate. With several, a member belongs to the client its
// own user-session issuer binds; members associated with none are dropped, so
// a credential never qualifies to a sibling's upstream.
func associatedMetaMembers(rows []metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow) []metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow {
	if len(rows) == 0 || rows[0].GatewayProviderClients <= 1 {
		return rows
	}
	associated := make([]metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow, 0, len(rows))
	for _, row := range rows {
		if row.MemberBindsClient {
			associated = append(associated, row)
		}
	}
	return associated
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
