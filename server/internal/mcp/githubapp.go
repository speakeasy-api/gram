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
		return mode, err
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
		return res, err
	}
	res.githubCredential = &token
	res.githubRenew = func(ctx context.Context) (string, bool, error) {
		forgotten, err := token.Forget(ctx)
		if err != nil || !forgotten {
			return "", false, err
		}
		next, err := s.githubApps.Credential(ctx, organizationID, res.clientID, serverID)
		if err != nil {
			return "", false, err
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
	own = append(own, proxy.ConfiguredHeader{Name: "Authorization", IsRequired: true, StaticValue: "Bearer " + token.Value()})
	return applyAuthClientHeaders(res, own), nil
}
