package idjag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/urn"
	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

// Validator checks an ID-JAG and reserves its identifier only after its
// subject has resolved, so a transient directory failure remains retryable.
type Validator struct {
	keys  assertioncore.VerificationKeys
	guard assertioncore.ReplayGuard
	store Store
}

// NewValidator binds the verification, replay, and subject dependencies.
func NewValidator(keys assertioncore.VerificationKeys, guard assertioncore.ReplayGuard, store Store) (*Validator, error) {
	if keys == nil || guard == nil || store == nil {
		return nil, errors.New("idjag: keys, replay guard, and store are required")
	}
	if guard.MaxHold() < assertioncore.ReplayHoldFor(MaxLifetime) {
		return nil, fmt.Errorf("idjag: replay guard hold %s is shorter than %s", guard.MaxHold(), assertioncore.ReplayHoldFor(MaxLifetime))
	}
	return &Validator{keys: keys, guard: guard, store: store}, nil
}

// Validate authenticates and resolves a presented ID-JAG. It never mints a
// session or decides HTTP error vocabulary.
func (v *Validator) Validate(ctx context.Context, raw string, request Request) (*Result, error) {
	if request.OrganizationID == "" || request.UserSessionIssuerID == uuid.Nil || request.Audience == "" || request.Resource == "" || request.ClientID == "" {
		return nil, reject(ReasonVerifierMisconfigured, errors.New("endpoint identity, resource, and authenticated client are required"))
	}
	token, err := assertioncore.ParseSigned(raw, maxBytes)
	if err != nil {
		return nil, reject(ReasonMalformed, err)
	}
	if len(token.Headers) != 1 {
		return nil, reject(ReasonMalformed, errors.New("assertion must have one signature header"))
	}
	if typ, _ := token.Headers[0].ExtraHeaders[jose.HeaderType].(string); typ != Type {
		return nil, reject(ReasonTypeMismatch, fmt.Errorf("expected typ %q", Type))
	}

	trusted, err := v.store.TrustedIssuer(ctx, request.OrganizationID, request.UserSessionIssuerID)
	if err != nil {
		return nil, reject(ReasonTrustUnavailable, err)
	}
	if trusted.ID == uuid.Nil || trusted.Issuer == "" || trusted.JWKSURI == "" {
		return nil, reject(ReasonVerifierMisconfigured, errors.New("trusted issuer lacks id, issuer, or jwks_uri"))
	}
	var unverified jwt.Claims
	if err := token.UnsafeClaimsWithoutVerification(&unverified); err != nil {
		return nil, reject(ReasonMalformed, err)
	}
	if unverified.Issuer != trusted.Issuer {
		return nil, reject(ReasonIssuerMismatch, errors.New("iss differs from the linked trusted issuer"))
	}
	source, err := jwks.NewRemoteSource(trusted.JWKSURI)
	if err != nil {
		return nil, reject(ReasonVerifierMisconfigured, err)
	}
	source = source.WithFetchScope("idjag:" + request.UserSessionIssuerID.String()).
		WithRefreshNamespace("idjag:" + trusted.ID.String()).
		WithCacheKey(issuerCacheKey(request.OrganizationID, trusted.ID, trusted.Issuer, trusted.JWKSURI))
	var payload json.RawMessage
	if err := assertioncore.VerifiedClaims(ctx, v.keys, source, token, &payload); err != nil {
		if verificationErr, ok := errors.AsType[*assertioncore.VerificationError](err); ok && verificationErr.Stage == assertioncore.VerificationKeyResolution {
			if errors.Is(err, jwks.ErrKeyNotFound) || errors.Is(err, jwks.ErrRefreshRateLimited) || errors.Is(err, jwks.ErrFetchRateLimited) {
				return nil, reject(ReasonKeyUnknown, err)
			}
			return nil, reject(ReasonKeyUnresolvable, err)
		}
		return nil, reject(ReasonSignatureInvalid, err)
	}
	var claims jwt.Claims
	var extra additionalClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, reject(ReasonClaimsMalformed, err)
	}
	if err := json.Unmarshal(payload, &extra); err != nil {
		return nil, reject(ReasonClaimsMalformed, err)
	}
	if claims.Issuer != trusted.Issuer {
		return nil, reject(ReasonIssuerMismatch, errors.New("verified issuer differs from the linked trusted issuer"))
	}
	if claims.Subject == "" {
		return nil, reject(ReasonSubjectMissing, errors.New("sub is required"))
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != request.Audience {
		return nil, reject(ReasonAudienceMismatch, errors.New("aud must be only the authorization server issuer"))
	}
	if extra.Resource == "" || extra.Resource != request.Resource {
		return nil, reject(ReasonResourceMismatch, errors.New("resource does not name this MCP server"))
	}
	if extra.ClientID == "" || extra.ClientID != request.ClientID {
		return nil, reject(ReasonClientMismatch, errors.New("client_id differs from the authenticated client"))
	}
	email := strings.TrimSpace(extra.Email)
	if email == "" {
		return nil, reject(ReasonEmailMissing, errors.New("email is required for subject resolution"))
	}
	if claims.Expiry == nil {
		return nil, reject(ReasonExpiryMissing, errors.New("exp is required"))
	}
	if claims.IssuedAt == nil {
		return nil, reject(ReasonIssuedAtMissing, errors.New("iat is required"))
	}
	now := time.Now()
	if err := claims.ValidateWithLeeway(jwt.Expected{Issuer: "", Subject: "", AnyAudience: nil, ID: "", Time: now}, assertioncore.MaxSkew); err != nil {
		switch {
		case errors.Is(err, jwt.ErrExpired):
			return nil, reject(ReasonExpired, err)
		case errors.Is(err, jwt.ErrNotValidYet), errors.Is(err, jwt.ErrIssuedInTheFuture):
			return nil, reject(ReasonNotYetValid, err)
		default:
			return nil, reject(ReasonClaimsMalformed, err)
		}
	}
	expiresAt := claims.Expiry.Time()
	if expiresAt.After(now.Add(MaxLifetime + assertioncore.MaxSkew)) {
		return nil, reject(ReasonLifetimeTooLong, errors.New("exp exceeds the ID-JAG lifetime bound"))
	}
	if expiresAt.Sub(claims.IssuedAt.Time()) > MaxLifetime+assertioncore.MaxSkew {
		return nil, reject(ReasonLifetimeTooLong, errors.New("exp exceeds the permitted lifetime from iat"))
	}
	if claims.ID == "" {
		return nil, reject(ReasonIDMissing, errors.New("jti is required"))
	}
	userID, err := v.store.ResolveUser(ctx, request.OrganizationID, email)
	if err != nil {
		if errors.Is(err, ErrNotProvisioned) {
			return nil, reject(ReasonNotProvisioned, err)
		}
		return nil, reject(ReasonSubjectUnavailable, err)
	}
	if userID == "" {
		return nil, reject(ReasonNotProvisioned, ErrNotProvisioned)
	}
	claimed, err := assertioncore.Reserve(ctx, v.guard, replay.Key{
		Issuer: request.UserSessionIssuerID.String(), Party: trusted.ID.String(), Subject: "", ID: claims.ID,
	}, expiresAt)
	if err != nil {
		return nil, reject(ReasonReplayStoreUnavailable, err)
	}
	if !claimed {
		return nil, reject(ReasonReplayed, errors.New("jti has already been presented"))
	}
	return &Result{
		Subject: urn.NewUserSubject(userID), TrustedIssuerID: trusted.ID,
		Claims: Claims{Issuer: claims.Issuer, ExternalSubject: claims.Subject, Email: email, Resource: extra.Resource, ClientID: extra.ClientID, JTI: claims.ID, Scope: extra.Scope, ExpiresAt: expiresAt},
	}, nil
}
