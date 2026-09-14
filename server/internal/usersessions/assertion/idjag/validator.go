// Package idjag validates identity assertion JWT authorization grants and
// resolves their enterprise identities to provisioned Gram users.
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

const (
	// Type is the protected JWT header type for an ID-JAG.
	Type = "oauth-id-jag+jwt"

	// MaxLifetime bounds how far an ID-JAG expiration can be in the future.
	MaxLifetime = 10 * time.Minute

	maxBytes = 32 * 1024
)

// Reason identifies a rejected assertion or an unavailable validation input.
type Reason string

const (
	ReasonMalformed              Reason = "assertion_malformed"
	ReasonTypeMismatch           Reason = "assertion_type_mismatch"
	ReasonTrustUnavailable       Reason = "trusted_issuer_unavailable"
	ReasonIssuerMismatch         Reason = "assertion_issuer_mismatch"
	ReasonKeyUnknown             Reason = "assertion_key_unknown"
	ReasonKeyUnresolvable        Reason = "assertion_key_unresolvable"
	ReasonSignatureInvalid       Reason = "assertion_signature_invalid"
	ReasonClaimsMalformed        Reason = "assertion_claims_malformed"
	ReasonAudienceMismatch       Reason = "assertion_audience_mismatch"
	ReasonResourceMismatch       Reason = "assertion_resource_mismatch"
	ReasonClientMismatch         Reason = "assertion_client_mismatch"
	ReasonSubjectMissing         Reason = "assertion_subject_missing"
	ReasonEmailMissing           Reason = "assertion_email_missing"
	ReasonExpiryMissing          Reason = "assertion_expiry_missing"
	ReasonExpired                Reason = "assertion_expired"
	ReasonNotYetValid            Reason = "assertion_not_yet_valid"
	ReasonLifetimeTooLong        Reason = "assertion_lifetime_too_long"
	ReasonIDMissing              Reason = "assertion_id_missing"
	ReasonNotProvisioned         Reason = "subject_not_provisioned"
	ReasonSubjectUnavailable     Reason = "subject_resolution_unavailable"
	ReasonReplayed               Reason = "assertion_replayed"
	ReasonReplayStoreUnavailable Reason = "assertion_replay_store_unavailable"
	ReasonVerifierMisconfigured  Reason = "assertion_verifier_misconfigured"
)

// Error is a typed rejection. Callers may log Err but must not echo it to an
// OAuth client: it can contain key-set URLs or storage details.
type Error struct {
	Reason Reason
	Err    error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Reason, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// ReasonOf returns the rejection reason, or an empty value for another error.
func ReasonOf(err error) Reason {
	if typed, ok := errors.AsType[*Error](err); ok {
		return typed.Reason
	}
	return ""
}

func reject(reason Reason, err error) error { return &Error{Reason: reason, Err: err} }

// ErrNotProvisioned is returned by a subject store when no active directory
// identity maps to an active Gram member in the same organization.
var ErrNotProvisioned = errors.New("enterprise identity is not provisioned")

// ErrNoTrustedIssuer is returned when the user session issuer has no active,
// organization-accessible trusted issuer.
var ErrNoTrustedIssuer = errors.New("user session issuer has no trusted remote issuer")

// TrustedIssuer is the issuer reached through the user session issuer's
// configured trust link, never through a search on an assertion's iss claim.
type TrustedIssuer struct {
	ID      uuid.UUID
	Issuer  string
	JWKSURI string
}

// Store supplies the trusted-issuer link and active directory membership.
type Store interface {
	TrustedIssuer(ctx context.Context, organizationID string, userSessionIssuerID uuid.UUID) (TrustedIssuer, error)
	ResolveUser(ctx context.Context, organizationID, email string) (string, error)
}

// Request contains trusted endpoint and authenticated-client values supplied
// by the grant handler; none is taken from the unverified JWT.
type Request struct {
	OrganizationID      string
	UserSessionIssuerID uuid.UUID
	Audience            string
	Resource            string
	ClientID            string
}

// Claims are the verified ID-JAG values needed by a grant handler.
type Claims struct {
	Issuer          string
	ExternalSubject string
	Email           string
	Resource        string
	ClientID        string
	JTI             string
	Scope           string
	ExpiresAt       time.Time
}

// Result is an accepted Gram user subject and its verified grant claims.
type Result struct {
	Subject         urn.SessionSubject
	TrustedIssuerID uuid.UUID
	Claims          Claims
}

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

type additionalClaims struct {
	Resource string `json:"resource"`
	ClientID string `json:"client_id"`
	Email    string `json:"email"`
	Scope    string `json:"scope"`
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
		WithRefreshNamespace("idjag").WithCacheKey(issuerCacheKey(request.OrganizationID, trusted.ID, trusted.Issuer, trusted.JWKSURI))
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
	if claims.IssuedAt != nil && expiresAt.Sub(claims.IssuedAt.Time()) > MaxLifetime+assertioncore.MaxSkew {
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
