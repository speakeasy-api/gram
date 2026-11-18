package identitychaining

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// CheckConsent tries the vendor's providers in order, as Acquire does, and
// each runs its live exchange. The Speakeasy provider vouches for the consent
// page's sign-in; its credential has no MCP grant yet, and any grant backed by
// that sign-in adopts it on first use (adoptConsentCredential).
func (c *Chainer) CheckConsent(ctx context.Context, req Request, signInID uuid.UUID) Outcome {
	req.consentNeedsToken = false
	_, outcome := c.consent(ctx, req, signInID)
	return outcome
}

// AcquireConsent returns the exchanged credential for consent-time discovery.
// Success always carries a token: an acceptance an earlier check recorded
// without storing its credential is exchanged again, never reported as a
// credential the caller could call the upstream with.
func (c *Chainer) AcquireConsent(ctx context.Context, req Request, signInID uuid.UUID) (Token, Outcome) {
	var none Token
	req.consentNeedsToken = true
	token, outcome := c.consent(ctx, req, signInID)
	if outcome.Succeeded() && token.value == "" {
		return none, newOutcome(StagePersistence, ReasonTransientFailure, ConfidenceVerified, true)
	}
	return token, outcome
}

func (c *Chainer) consent(ctx context.Context, req Request, signInID uuid.UUID) (Token, Outcome) {
	var none Token
	if !req.complete() || req.UserID == "" {
		return none, notApplicable
	}
	req.UserSessionJTI = ""
	req.consentSignInID = signInID
	logger := c.logger.With(attr.SlogOrganizationID(req.OrganizationID), attr.SlogProjectID(req.ProjectID.String()), attr.SlogUserSessionIssuerID(req.UserSessionIssuerID.String()), attr.SlogUserID(req.UserID))
	sel, outcome := c.selectBinding(ctx, logger, req)
	if !outcome.Succeeded() {
		return none, outcome
	}
	logger = logger.With(attr.SlogRemoteSessionIssuerID(sel.remoteIssuerID.String()), attr.SlogRemoteSessionClientID(sel.clientID.String()), attr.SlogOAuthResource(sel.resource))
	order, outcome := c.providerOrder(ctx, logger, req, sel)
	if !outcome.Succeeded() {
		return none, outcome
	}
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()
	token, attempt := c.tryProviders(ctx, logger, "consent", order, func(ctx context.Context, logger *slog.Logger, p remotesessions.EMAProvider) (Token, Outcome) {
		if p.Kind == remotesessions.EMAProviderSpeakeasy && !req.consent() {
			var none Token
			return none, newOutcome(StageAuthorization, ReasonReauthenticationRequired, ConfidenceVerified, false)
		}
		return c.acquireFrom(ctx, logger, req, sel, p)
	})
	return token, attempt
}

// RetireConsentCredentials erases the credentials the sign-in's consent pages
// stored for the issuer's endpoints that no grant has adopted. The consent
// flow calls it when the user denies access. It is a point-in-time cleanup: an
// exchange already in flight can still store a credential afterwards, which
// only another eligible grant of the same sign-in could adopt.
func (c *Chainer) RetireConsentCredentials(ctx context.Context, organizationID string, userSessionIssuerID uuid.UUID, userID string, signInID uuid.UUID) error {
	if signInID == uuid.Nil || userID == "" {
		return nil
	}
	if _, err := repo.New(c.db).RetireSpeakeasySignInConsentEMACredentials(ctx, repo.RetireSpeakeasySignInConsentEMACredentialsParams{
		OrganizationID: organizationID, UserSessionIssuerID: userSessionIssuerID,
		SubjectUrn: urn.NewUserSubject(userID).String(), SignInID: signInID,
	}); err != nil {
		return fmt.Errorf("retire consent EMA credentials: %w", err)
	}
	return nil
}
