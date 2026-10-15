package identitychaining

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// CheckConsent tries the vendor's providers in order, as Acquire does, and
// each runs its live exchange. The Speakeasy provider vouches for the consent
// page's sign-in; its credential has no MCP grant yet, and a grant backed by
// that sign-in adopts it on first use (adoptConsentCredential).
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
	order, outcome := c.providerOrder(ctx, logger, req, sel)
	if !outcome.Succeeded() {
		return outcome
	}
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()
	_, attempt := c.tryProviders(ctx, logger, "consent", order, func(ctx context.Context, logger *slog.Logger, p remotesessions.EMAProvider) (Token, Outcome) {
		if p.Kind != remotesessions.EMAProviderSpeakeasy {
			return c.acquireFrom(ctx, logger, req, sel, p)
		}
		var none Token
		source, ok := c.source(p).(speakeasySource)
		if !ok || !source.available() {
			return none, unavailableBecause(remotesessions.EMAProviderSigningUnavailable)
		}
		if signInID == uuid.Nil {
			return none, newOutcome(StageAuthorization, ReasonReauthenticationRequired, ConfidenceVerified, false)
		}
		source.consentSignInID = signInID
		return c.acquireWith(ctx, logger, req, sel, p, source)
	})
	return attempt
}
