package identitychaining

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"go.opentelemetry.io/otel/metric"
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
	providers, outcome := c.servingProviders(ctx, logger, req, sel)
	if !outcome.Succeeded() {
		return outcome
	}
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()
	var first Outcome
	for _, provider := range providers {
		var attempt Outcome
		if provider.Kind == remotesessions.EMAProviderSpeakeasy {
			source, ok := c.source(provider).(speakeasySource)
			if !ok || !source.available() {
				attempt = providerUnavailable
			} else {
				current := sel
				current.provider = provider
				attempt = source.checkConsent(ctx, logger.With(attr.SlogIdentityChainingProvider(string(provider.Kind))), req, current, signInID)
				if attempt.Succeeded() {
					attempt = newOutcome(StageAuthorization, ReasonConsentRequired, ConfidenceVerified, false)
				}
			}
		} else {
			_, attempt = c.acquireFrom(ctx, logger.With(attr.SlogIdentityChainingProvider(string(provider.Kind))), req, sel, provider)
		}
		attempt.Provider = provider.Kind
		c.attempts.Add(ctx, 1, metric.WithAttributes(attr.IdentityChainingPath("consent"), attr.IdentityChainingProvider(string(provider.Kind)), attr.Outcome(attempt.Reason), attr.IdentityChainingFallback(false)))
		if attempt.Succeeded() || attempt.Reason == ReasonConsentRequired {
			return attempt
		}
		if first.Reason == "" {
			first = attempt
		}
	}
	return first
}
