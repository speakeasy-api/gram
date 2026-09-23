package mcp

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// stampConsentDiscovery is called after resolving and authorizing a challenge's
// endpoint and reconstructing its current tenant context. Only the human
// resolved by the IDP callback is evidence; a selected agent, anonymous subject,
// pending federation, or synthetic background probe is not an acting user.
func (s *Service) stampConsentDiscovery(ctx context.Context, state AuthnChallengeState) context.Context {
	ctx = mcpidentity.WithoutIdentity(ctx)
	if state.Subject == nil || state.Subject.Kind != urn.SessionSubjectKindUser ||
		state.AuthorizerImpersonated == nil || *state.AuthorizerImpersonated ||
		state.AuthorizerUserID == "" || state.Subject.ID != state.AuthorizerUserID ||
		state.Federation != nil || state.CreatedAt.IsZero() || state.CreatedAt.After(time.Now().Add(time.Minute)) {
		return ctx
	}
	return s.identityValidator.StampConsentDiscovery(ctx, state.Subject.ID, state.CreatedAt.Add(state.TTL()))
}
