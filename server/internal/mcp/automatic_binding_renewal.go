package mcp

import (
	"context"
	"errors"
	"fmt"
	redisCache "github.com/go-redis/cache/v9"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Callback state pins the human and source. The still-live parent must also
// prove this is an ordinary human sign-in, never impersonation or an agent.
func (s *Service) authorizeAutomaticBindingRenewal(ctx context.Context, state remotesessions.RemoteLoginState) (bool, error) {
	parent, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+state.ParentChallengeID)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load reauthentication parent: %w", err)
	}
	if state.Subject == nil || state.Subject.Kind != urn.SessionSubjectKindUser || parent.Subject == nil || *parent.Subject != *state.Subject || parent.AuthorizerUserID != state.Subject.ID || parent.AuthorizerImpersonated == nil || *parent.AuthorizerImpersonated || parent.UserSessionIssuerID != state.UserSessionIssuerID {
		return false, nil
	}
	endpoint, err := s.loadResolvedMcpEndpointByRef(ctx, parent.Endpoint)
	if err != nil {
		return false, fmt.Errorf("resolve reauthentication endpoint: %w", err)
	}
	if endpoint.OrganizationID != state.OrganizationID || endpoint.ProjectID != state.ProjectID {
		return false, nil
	}
	if err := endpoint.ValidateChallenge(ctx, parent.Endpoint, state.UserSessionIssuerID); err != nil {
		return false, fmt.Errorf("validate reauthentication authority: %w", err)
	}
	return true, nil
}
