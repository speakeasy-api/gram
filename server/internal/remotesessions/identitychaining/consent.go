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
	if !req.complete() || req.UserID == "" {
		return notApplicable
	}
	req.UserSessionJTI = ""
	req.consentSignInID = signInID
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
		if p.Kind == remotesessions.EMAProviderSpeakeasy && !req.consent() {
			var none Token
			return none, newOutcome(StageAuthorization, ReasonReauthenticationRequired, ConfidenceVerified, false)
		}
		return c.acquireFrom(ctx, logger, req, sel, p)
	})
	return attempt
}

// RetireConsentCredentials erases the credentials the sign-in's consent pages
// stored for the issuer's endpoints that no grant has adopted. The consent
// flow calls it when the user denies access. It is a point-in-time cleanup: an
// exchange already in flight can still store a credential afterwards, which
// only another eligible grant of the same sign-in could adopt.
func (c *Chainer) RetireConsentCredentials(ctx context.Context, organizationID string, projectID, userSessionIssuerID uuid.UUID, userID string, signInID uuid.UUID) error {
	if signInID == uuid.Nil || userID == "" {
		return nil
	}
	if _, err := repo.New(c.db).RetireSpeakeasySignInConsentEMACredentials(ctx, repo.RetireSpeakeasySignInConsentEMACredentialsParams{
		OrganizationID: organizationID, ProjectID: projectID, UserSessionIssuerID: userSessionIssuerID,
		SubjectUrn: urn.NewUserSubject(userID).String(), SignInID: signInID,
	}); err != nil {
		return fmt.Errorf("retire consent EMA credentials: %w", err)
	}
	return nil
}
