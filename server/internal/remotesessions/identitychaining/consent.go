package identitychaining

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// CheckConsent tries the vendor's providers in order, as Acquire does: an
// external provider runs its live exchange, and the Speakeasy provider checks
// the sign-in before its MCP grant exists. The latter never signs or
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
	order, outcome := c.providerOrder(ctx, logger, req, sel)
	if !outcome.Succeeded() {
		return outcome
	}
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()
	_, attempt := c.tryProviders(ctx, logger, "consent", order, func(ctx context.Context, logger *slog.Logger, p remotesessions.EMAProvider) (Token, Outcome) {
		var none Token
		if p.Kind != remotesessions.EMAProviderSpeakeasy {
			return c.acquireFrom(ctx, logger, req, sel, p)
		}
		source, ok := c.source(p).(speakeasySource)
		if !ok || !source.available() {
			return none, unavailableBecause(remotesessions.EMAProviderSigningUnavailable)
		}
		current := sel
		current.provider = p
		outcome := source.checkConsent(ctx, logger, req, current, signInID)
		if outcome.Succeeded() {
			outcome = newOutcome(StageAuthorization, ReasonConsentRequired, ConfidenceVerified, false)
		}
		return none, outcome
	})
	return attempt
}
