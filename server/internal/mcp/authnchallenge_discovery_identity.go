package mcp

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// stampConsentDiscovery uses the human identity from the IDP callback. Call it
// after resolving and authorizing the challenge's endpoint and rebuilding its
// tenant context. Selected agents, anonymous subjects, pending federation and
// synthetic background probes cannot establish a human identity.
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
