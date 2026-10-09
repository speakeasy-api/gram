package identitychaining

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// acquire runs one exchange and redemption for an authorized selection.
// Nothing here replays a request after an ambiguous submission. The boolean
// reports whether an ID-JAG passed validation before redemption.
func (c *Chainer) acquire(ctx context.Context, logger *slog.Logger, req Request, sel selection) (Token, Outcome, bool) {
	var none Token
	binding := remotesessions.DelegationBinding{OrganizationID: req.OrganizationID, IssuerID: sel.trustedIssuerID, ClientID: sel.trustedClientID, HumanID: req.UserID}
	assertion, err := c.delegation.Resolve(ctx, binding, authority{chainer: c, req: req})
	switch {
	case errors.Is(err, remotesessions.ErrDelegationReauthentication):
		return none, newOutcome(StageDelegation, ReasonReauthenticationRequired, ConfidenceVerified, false), false
	case errors.Is(err, remotesessions.ErrDelegationConfiguration):
		return none, newOutcome(StageDelegation, ReasonConfigurationRequired, ConfidenceVerified, false), false
	case err != nil:
		return none, newOutcome(StageDelegation, ReasonTransientFailure, ConfidenceVerified, true), false
	}
	// Subject continuity needs the verified upstream subject the assertion
	// was retained with; a retained row without one predates that record.
	if assertion.Subject() == "" || assertion.SessionID() == uuid.Nil {
		return none, newOutcome(StageDelegation, ReasonReauthenticationRequired, ConfidenceVerified, false), false
	}

	idp, err := c.challenges.LoadIdentityProviderEndpoint(ctx, c.keys, req.OrganizationID, sel.trustedIssuerID, sel.trustedClientID)
	switch {
	case errors.Is(err, remotesessions.ErrFederatedConfiguration), errors.Is(err, remotesessions.ErrFederatedSigning):
		return none, newOutcome(StageExchange, ReasonConfigurationRequired, ConfidenceVerified, false), false
	case err != nil:
		return none, newOutcome(StageExchange, ReasonTransientFailure, ConfidenceVerified, true), false
	}
	resourceAS, err := c.challenges.LoadClientTokenEndpoint(ctx, sel.clientID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, newOutcome(StageRedemption, ReasonStaleConfiguration, ConfidenceVerified, true), false
	case errors.Is(err, remotesessions.ErrTokenEndpointConfiguration):
		return none, newOutcome(StageRedemption, ReasonConfigurationRequired, ConfidenceVerified, false), false
	case err != nil:
		logger.ErrorContext(ctx, "load identity chaining resource client", attr.SlogError(err))
		return none, newOutcome(StageRedemption, ReasonTransientFailure, ConfidenceVerified, true), false
	}
	if resourceAS.IssuerID() != sel.remoteIssuerID || resourceAS.ClientID() != sel.externalClientID || resourceAS.Issuer() != sel.issuer {
		return none, newOutcome(StageRedemption, ReasonStaleConfiguration, ConfidenceVerified, true), false
	}

	grant, outcome := exchange(ctx, idp, assertion.Value(), sel)
	if !outcome.Succeeded() {
		return none, outcome, false
	}
	if outcome = c.validateGrant(ctx, logger, idp, idp.Issuer(), grant, assertion.Subject(), sel); !outcome.Succeeded() {
		return none, outcome, false
	}
	cred, outcome := c.redeem(ctx, logger, resourceAS, grant, sel)
	if !outcome.Succeeded() {
		return none, outcome, true
	}
	cred.trustedSessionID = assertion.SessionID()
	cred.trustedObtainedAt = assertion.ObtainedAt()

	switch err := c.publish(ctx, req, sel, cred); {
	case errors.Is(err, errStale):
		return none, newOutcome(StagePersistence, ReasonStaleConfiguration, ConfidenceVerified, true), true
	case err != nil:
		logger.ErrorContext(ctx, "publish identity chaining credential", attr.SlogError(err))
		return none, newOutcome(StagePersistence, ReasonTransientFailure, ConfidenceVerified, true), true
	}
	return Token{value: cred.accessToken, expiresAt: cred.expiresAt.Add(-accessExpirySkew)}, success, true
}

// exchange asks the trusted identity provider to exchange the human's ID token
// for an ID-JAG audience-bound to the resource authorization server. The
// identity provider's client authenticates; the resource client is only named
// in the grant.
func exchange(ctx context.Context, idp tokenPoster, idToken string, sel selection) (string, Outcome) {
	form := url.Values{}
	form.Set(oauthwire.ParamGrantType, oauthwire.GrantTypeTokenExchange)
	form.Set(oauthwire.ParamSubjectToken, idToken)
	form.Set(oauthwire.ParamSubjectTokenType, oauthwire.TokenTypeIDToken)
	form.Set(oauthwire.ParamRequestedTokenType, oauthwire.TokenTypeIDJAG)
	form.Set(oauthwire.ParamAudience, sel.audience)
	form.Set(oauthwire.ParamResource, sel.resource)
	if len(sel.scopes) > 0 {
		form.Set(oauthwire.ParamScope, strings.Join(sel.scopes, " "))
	}
	ctx, cancel := context.WithTimeout(ctx, legTimeout)
	defer cancel()
	tok, err := idp.Post(ctx, form)
	if err != nil {
		return "", classifyEndpointError(StageExchange, err)
	}
	if tok.IssuedTokenType() != oauthwire.TokenTypeIDJAG {
		return "", newOutcome(StageExchange, ReasonMalformedResponse, ConfidenceVerified, false)
	}
	return tok.AccessToken(), success
}

// validateGrant verifies the ID-JAG against the trusted identity provider's
// published keys before it leaves Speakeasy. No claim selects keys or trust.
func (c *Chainer) validateGrant(ctx context.Context, logger *slog.Logger, verifier assertionVerifier, idpIssuer, raw, upstreamSubject string, sel selection) Outcome {
	invalid := newOutcome(StageValidation, ReasonMalformedAssertion, ConfidenceVerified, false)
	var claims grantClaims
	header, err := verifier.VerifyAssertion(ctx, raw, &claims)
	if err != nil {
		if errors.Is(err, remotesessions.ErrJWTKeySetUnavailable) {
			return newOutcome(StageValidation, ReasonTransientFailure, ConfidenceVerified, true)
		}
		logger.WarnContext(ctx, "identity chaining assertion grant failed verification", attr.SlogError(err))
		return invalid
	}
	if err := claims.check(header, idpIssuer, upstreamSubject, sel, c.now()); err != nil {
		logger.WarnContext(ctx, "identity chaining assertion grant rejected", attr.SlogError(err))
		if scopeErr, ok := errors.AsType[scopeError](err); ok {
			return newOutcome(StageValidation, scopeErr.reason, ConfidenceVerified, false)
		}
		return invalid
	}
	return success
}

// redeem presents the ID-JAG to the resource authorization server as an RFC
// 7523 JWT bearer grant. Any downstream refresh token is discarded; expiry
// renewal re-exchanges instead.
func (c *Chainer) redeem(ctx context.Context, logger *slog.Logger, resourceAS tokenPoster, grant string, sel selection) (credential, Outcome) {
	var none credential
	form := url.Values{}
	form.Set(oauthwire.ParamGrantType, oauthwire.GrantTypeJWTBearer)
	form.Set(oauthwire.ParamAssertion, grant)
	postCtx, cancel := context.WithTimeout(ctx, legTimeout)
	defer cancel()
	tok, err := resourceAS.Post(postCtx, form)
	if err != nil {
		return none, classifyEndpointError(StageRedemption, err)
	}
	malformed := newOutcome(StageRedemption, ReasonMalformedResponse, ConfidenceVerified, false)
	if tok.AccessToken() == "" || (tok.TokenType() != "" && !strings.EqualFold(tok.TokenType(), "bearer")) {
		return none, malformed
	}
	granted := sel.scopes
	if tok.ScopeReported() {
		granted = tok.Scopes()
		for _, scope := range sel.scopes {
			if !slices.Contains(granted, scope) {
				logger.WarnContext(ctx, "identity chaining token lacks requested scope",
					attr.SlogOAuthScope(strings.Join(sel.scopes, " ")), attr.SlogOAuthScopeGranted(granted), attr.SlogOAuthResource(sel.resource))
				return none, newOutcome(StageRedemption, ReasonInsufficientScope, ConfidenceVerified, false)
			}
		}
	}
	now := c.now()
	expiresAt := now.Add(unknownExpiryLifetime)
	if reported := tok.AccessExpiresAt(now); reported != nil {
		expiresAt = *reported
	}
	// A short-lived token serves this request and is simply not reused.
	if !expiresAt.After(now) {
		return none, malformed
	}
	if capped := now.Add(maxAccessLifetime); expiresAt.After(capped) {
		expiresAt = capped
	}
	return credential{
		accessToken:       tok.AccessToken(),
		expiresAt:         expiresAt,
		grantedScopes:     granted,
		refreshObserved:   tok.RefreshTokenReturned(),
		trustedSessionID:  uuid.Nil,
		trustedObtainedAt: time.Time{},
	}, success
}

// classifyEndpointError maps a token endpoint failure to an outcome.
// invalid_grant stays ambiguous: it does not prove expiry or missing trust.
func classifyEndpointError(stage Stage, err error) Outcome {
	failure, ok := errors.AsType[*remotesessions.TokenEndpointError](err)
	if !ok {
		return newOutcome(stage, ReasonMalformedResponse, ConfidenceVerified, false)
	}
	outcome := classifyEndpointFailure(stage, failure)
	outcome.providerDescription = failure.Description
	return outcome
}

func classifyEndpointFailure(stage Stage, failure *remotesessions.TokenEndpointError) Outcome {
	switch {
	case failure.Transport, failure.Signing:
		return newOutcome(stage, ReasonTransientFailure, ConfidenceUnknown, true)
	case failure.StatusCode >= http.StatusInternalServerError, failure.StatusCode == http.StatusTooManyRequests, failure.StatusCode == http.StatusRequestTimeout,
		failure.Code == oautherr.CodeServerError, failure.Code == oautherr.CodeTemporarilyUnavailable:
		return newOutcome(stage, ReasonTransientFailure, ConfidenceInferred, true)
	}
	switch failure.Code {
	case oautherr.CodeInvalidTarget:
		return newOutcome(stage, ReasonInvalidTarget, ConfidenceInferred, false)
	case oautherr.CodeUnauthorizedClient, oautherr.CodeInvalidScope:
		return newOutcome(stage, ReasonScopePolicyDenied, ConfidenceInferred, false)
	case oautherr.CodeAccessDenied:
		return newOutcome(stage, ReasonAccessDenied, ConfidenceInferred, false)
	case oautherr.CodeInvalidClient:
		return newOutcome(stage, ReasonInvalidClient, ConfidenceInferred, false)
	case oautherr.CodeInvalidGrant, oautherr.CodeInvalidToken:
		return newOutcome(stage, ReasonInvalidGrant, ConfidenceUnknown, false)
	}
	return newOutcome(stage, ReasonUnknownRejection, ConfidenceUnknown, false)
}
