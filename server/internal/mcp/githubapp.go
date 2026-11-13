package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// GitHub providers have no legacy OAuth or static-header equivalent. Their
// server always uses typed selection, independent of rollout flags. Historical
// attachments count until an explicit replacement is attached: deleting or
// disabling a client alone must not restore a legacy credential.
func (s *Service) serverRequiresGitHubApp(ctx context.Context, organizationID string, serverID uuid.UUID) (bool, error) {
	servers, err := authclientsrepo.New(s.db).ListGitHubAppServers(ctx, authclientsrepo.ListGitHubAppServersParams{OrganizationID: organizationID, McpServerIds: []uuid.UUID{serverID}})
	if err != nil {
		return false, fmt.Errorf("check GitHub App requirement: %w", err)
	}
	return len(servers) != 0, nil
}

func (s *Service) prepareGitHubApp(ctx context.Context, permit authClientPermitRequest, res authClientResolution, headers []proxy.ConfiguredHeader) (authClientResolution, error) {
	if s.githubApps == nil {
		return res, fmt.Errorf("GitHub App provider is unavailable")
	}
	// Both serving paths have already checked this client's permission. Renewal
	// below checks it again because grants may change after the initial request.
	token, err := s.githubApps.Credential(ctx, permit.OrganizationID, res.clientID, permit.ServerID)
	if err != nil {
		return res, fmt.Errorf("resolve GitHub App credential: %w", err)
	}
	res.githubCredential = &token
	res.githubRenew = func(ctx context.Context) (string, bool, error) {
		if err := s.authorizeGitHubApp(ctx, permit, res); err != nil {
			return "", false, err
		}
		forgotten, err := token.Forget(ctx)
		if err != nil {
			return "", false, fmt.Errorf("invalidate GitHub App credential: %w", err)
		}
		if !forgotten {
			return "", false, nil
		}
		next, err := s.githubApps.Credential(ctx, permit.OrganizationID, res.clientID, permit.ServerID)
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
	own = append(own, proxy.ConfiguredHeader{ValueFromRequestHeader: "", Name: "Authorization", IsRequired: true, IsSecret: true, StaticValue: "Bearer " + token.Value()})
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

// authorizeGitHubApp repeats the mandatory decision on renewal: the caller's
// grants may have changed since the original upstream request was prepared.
func (s *Service) authorizeGitHubApp(ctx context.Context, permit authClientPermitRequest, res authClientResolution) error {
	checked, err := s.checkAuthClientPermission(ctx, permit, res, nil)
	if err != nil {
		return fmt.Errorf("authorize GitHub App credential: %w", err)
	}
	if checked.outcome != authClientResolved {
		return fmt.Errorf("GitHub App credential is no longer authorized")
	}
	return nil
}
