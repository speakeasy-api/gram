package identitychaining

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// CheckConsent preserves external providers' live exchange while checking a
// Speakeasy sign-in before its MCP grant exists. The latter never signs or
// publishes a token: Acquire still requires the exact authenticated grant JTI.
func (c *Chainer) CheckConsent(ctx context.Context, req Request, signInID uuid.UUID) Outcome {
	if !req.complete() || req.UserID == "" {
		return notApplicable
	}
	logger := c.logger.With(attr.SlogOrganizationID(req.OrganizationID), attr.SlogProjectID(req.ProjectID.String()), attr.SlogUserSessionIssuerID(req.UserSessionIssuerID.String()), attr.SlogUserID(req.UserID))
	sel, outcome := c.selectBinding(ctx, logger, req)
	if !outcome.Succeeded() {
		return outcome
	}
	logger = logger.With(attr.SlogRemoteSessionIssuerID(sel.remoteIssuerID.String()), attr.SlogRemoteSessionClientID(sel.clientID.String()), attr.SlogOAuthResource(sel.resource))
	provider, outcome := c.servingProvider(ctx, logger, req, sel)
	if !outcome.Succeeded() {
		if outcome.Reason == ReasonProviderUnavailable {
			outcome.Provider = provider.Kind
			c.recordAttempt(ctx, "consent", outcome)
		}
		return outcome
	}
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()
	logger = logger.With(attr.SlogIdentityChainingProvider(string(provider.Kind)))
	var attempt Outcome
	if provider.Kind == remotesessions.EMAProviderSpeakeasy {
		source, ok := c.source(provider).(speakeasySource)
		if !ok || !source.available() {
			attempt = unavailableBecause(remotesessions.EMAProviderSigningUnavailable)
		} else {
			current := sel
			current.provider = provider
			attempt = source.checkConsent(ctx, logger, req, current, signInID)
			if attempt.Succeeded() {
				attempt = newOutcome(StageAuthorization, ReasonConsentRequired, ConfidenceVerified, false)
			}
		}
	} else {
		_, attempt = c.acquireFrom(ctx, logger, req, sel, provider)
	}
	attempt.Provider = provider.Kind
	c.recordAttempt(ctx, "consent", attempt)
	return attempt
}
