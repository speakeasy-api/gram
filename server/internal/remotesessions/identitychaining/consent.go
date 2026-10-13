package identitychaining

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// CheckConsent preserves external providers' live exchange while checking a
// Speakeasy sign-in before its MCP grant exists. The latter never signs or
// publishes a token: Acquire still requires the exact authenticated grant JTI.
func (c *Chainer) CheckConsent(ctx context.Context, req Request, signInID uuid.UUID) Outcome {
	if !req.complete() || req.UserID == "" {
		return notApplicable
	}
	sel, outcome := c.selectBinding(ctx, c.logger, req)
	if !outcome.Succeeded() {
		return outcome
	}
	providers, outcome := c.servingProviders(ctx, c.logger, req, sel)
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
				attempt = source.checkConsent(ctx, req, current, signInID)
				if attempt.Succeeded() {
					attempt = newOutcome(StageAuthorization, ReasonConsentRequired, ConfidenceVerified, false)
				}
			}
		} else {
			_, attempt = c.acquireFrom(ctx, c.logger, req, sel, provider)
		}
		attempt.Provider = provider.Kind
		if attempt.Succeeded() || attempt.Reason == ReasonConsentRequired {
			return attempt
		}
		if first.Reason == "" {
			first = attempt
		}
	}
	return first
}
