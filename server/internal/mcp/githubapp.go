package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// GitHub providers have no legacy OAuth or static-header equivalent. Their
// server always uses typed selection, independent of rollout flags. Historical
// attachments count too: deleting or disabling a client must not restore a
// legacy credential behind an existing connection.
func (s *Service) githubAppMode(ctx context.Context, organizationID string, serverID uuid.UUID, mode feature.AuthClientResolverMode) (feature.AuthClientResolverMode, error) {
	present, err := authclientsrepo.New(s.db).ServerHasGitHubAppAuthClient(ctx, authclientsrepo.ServerHasGitHubAppAuthClientParams{OrganizationID: organizationID, McpServerID: serverID})
	if err != nil {
		return mode, fmt.Errorf("check GitHub App requirement: %w", err)
	}
	if present {
		return feature.AuthClientResolverEnforced, nil
	}
	return mode, nil
}

func (s *Service) prepareGitHubApp(ctx context.Context, organizationID string, serverID uuid.UUID, res authClientResolution, headers []proxy.ConfiguredHeader) (authClientResolution, error) {
	if s.githubApps == nil {
		return res, fmt.Errorf("GitHub App provider is unavailable")
	}
	token, err := s.githubApps.Credential(ctx, organizationID, res.clientID, serverID)
	if err != nil {
		return res, fmt.Errorf("resolve GitHub App credential: %w", err)
	}
	res.githubCredential = &token
	res.githubRenew = func(ctx context.Context) (string, bool, error) {
		forgotten, err := token.Forget(ctx)
		if err != nil {
			return "", false, fmt.Errorf("invalidate GitHub App credential: %w", err)
		}
		if !forgotten {
			return "", false, nil
		}
		next, err := s.githubApps.Credential(ctx, organizationID, res.clientID, serverID)
		if err != nil {
			return "", false, fmt.Errorf("renew GitHub App credential: %w", err)
		}
		if next.Origin != token.Origin {
			return "", false, fmt.Errorf("GitHub App credential origin changed; reconnect")
		}
		return next.Value(), true, nil
	}
	own := make([]proxy.ConfiguredHeader, 0, len(headers)+1)
	for _, header := range headers {
		if !strings.EqualFold(header.Name, "Authorization") {
			own = append(own, header)
		}
	}
	own = append(own, proxy.ConfiguredHeader{ValueFromRequestHeader: "", Name: "Authorization", IsRequired: true, StaticValue: "Bearer " + token.Value()})
	return applyAuthClientHeaders(res, own), nil
}

// rejectLegacyGitHubMembers closes legacy and non-session paths as well as
// typed selection. Historical provider attachments must never revive legacy auth.
func (s *Service) rejectLegacyGitHubMembers(ctx context.Context, organizationID string, members []metaMember) error {
	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		if member.backend == metaMemberBackendProxied {
			ids = append(ids, member.serverID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	servers, err := authclientsrepo.New(s.db).ListGitHubAppServers(ctx, authclientsrepo.ListGitHubAppServersParams{OrganizationID: organizationID, McpServerIds: ids})
	if err != nil {
		return fmt.Errorf("check gateway GitHub App requirements: %w", err)
	}
	required := make(map[uuid.UUID]bool, len(servers))
	for _, id := range servers {
		required[id] = true
	}
	for i := range members {
		member := &members[i]
		if required[member.serverID] && githubSelectionMissing(member.authClient) {
			member.upstreamUnavailable = memberCredentialError{&metaMemberError{message: "This server requires a new sign-in to choose its upstream identity."}}
		}
	}
	return nil
}

func githubSelectionMissing(res *authClientResolution) bool {
	return res == nil || (res.outcome == authClientLegacy && res.clientID == uuid.Nil)
}
