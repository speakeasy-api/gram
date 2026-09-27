package workload

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

const maxAssertionBytes = 8 * 1024

// nonBearerTypes are JOSE typ values, normalized by normalizeMediaType, of
// credentials that must never be exchanged as a bearer grant:
//   - wit+jwt: a WIMSE Workload Identity Token, valid only together with proof
//     of possession of its bound key (draft-ietf-wimse-workload-creds).
//   - at+jwt: an access token issued by another authorization server
//     (RFC 9068).
var nonBearerTypes = map[string]struct{}{
	"wit+jwt": {},
	"at+jwt":  {},
}

// Result describes a verified workload assertion. Admission of the subject
// and issuance of credentials remain separate caller decisions.
type Result struct {
	// ExpiresAt is the assertion's verified expiration.
	ExpiresAt time.Time

	// ReusedAssertion is true when the exact same identifier-less platform
	// token was presented again within its validity window.
	ReusedAssertion bool
}

// Verifier checks workload assertions against a pre-admitted issuer and
// subject. It is safe for concurrent use.
type Verifier struct {
	keys  assertioncore.VerificationKeys
	guard assertioncore.ReplayGuard
}

// NewVerifier binds key resolution and authoritative replay memory.
func NewVerifier(keys assertioncore.VerificationKeys, guard assertioncore.ReplayGuard) (*Verifier, error) {
	if keys == nil || guard == nil {
		return nil, errors.New("workload: key resolver and replay guard are required")
	}
	return &Verifier{keys: keys, guard: guard}, nil
}

// Verify checks a signed platform assertion after its issuer and subject
// have been selected through tenant admission. Its jti or uti is single-use;
// a platform token with neither can be reused only as identical JWT bytes.
func (v *Verifier) Verify(ctx context.Context, raw string, expect Expectation) (*Result, error) {
	if err := expect.validate(); err != nil {
		return nil, err
	}
	if v.guard.MaxHold() < assertioncore.ReplayHoldFor(expect.MaxLifetime) {
		return nil, reject(ReasonVerifierMisconfigured, "replay guard hold is shorter than the assertion acceptance window")
	}
	if raw == "" {
		return nil, reject(ReasonAssertionMissing, "assertion is required")
	}
	token, err := assertioncore.ParseSigned(raw, maxAssertionBytes)
	if err != nil {
		return nil, rejectWith(ReasonMalformed, err)
	}
	// typ is a header, so refusing it before signature verification keeps the
	// signature-before-claims order and spends no key fetch on a token that
	// can never be accepted.
	if err := checkBearerType(token); err != nil {
		return nil, err
	}
	var claims jwt.Claims
	var extra replayIDClaims
	if err := assertioncore.VerifiedClaims(ctx, v.keys, expect.KeySource, token, &claims, &extra); err != nil {
		if verifiedErr, ok := errors.AsType[*assertioncore.VerificationError](err); ok && verifiedErr.Stage == assertioncore.VerificationKeyResolution {
			if errors.Is(err, jwks.ErrKeyNotFound) || errors.Is(err, jwks.ErrRefreshRateLimited) || errors.Is(err, jwks.ErrFetchRateLimited) {
				return nil, rejectWith(ReasonKeyUnknown, err)
			}
			return nil, rejectWith(ReasonKeyUnresolvable, err)
		}
		return nil, rejectWith(ReasonSignatureInvalid, err)
	}
	if claims.Expiry == nil {
		return nil, reject(ReasonExpiryMissing, "exp is required")
	}
	now := time.Now()
	if err := claims.ValidateWithLeeway(jwt.Expected{
		Issuer: expect.Issuer, Subject: expect.Subject, AnyAudience: expect.Audiences,
		Time: now, ID: "",
	}, assertioncore.MaxSkew); err != nil {
		return nil, rejectWith(reasonForClaimError(err), err)
	}
	expiresAt := claims.Expiry.Time()
	if expiresAt.After(now.Add(expect.MaxLifetime + assertioncore.MaxSkew)) {
		return nil, reject(ReasonLifetimeTooLong, "exp exceeds the configured platform lifetime")
	}
	id, exact := resolveReplayID(claims.ID, extra, raw)
	claimed, err := assertioncore.Reserve(ctx, v.guard, replay.Key{
		Issuer: expect.ReplayIssuer, Party: expect.ReplayParty, Subject: expect.Subject, ID: id,
	}, expiresAt)
	if err != nil {
		return nil, rejectWith(ReasonReplayStoreUnavailable, err)
	}
	if !claimed && exact {
		return nil, reject(ReasonReplayed, "assertion identifier has already been presented")
	}
	return &Result{ExpiresAt: expiresAt, ReusedAssertion: !claimed}, nil
}

// checkBearerType refuses a denylisted typ. A missing typ and every other
// value pass: platforms do not send a consistent one, so none is required.
func checkBearerType(token *jwt.JSONWebToken) error {
	if len(token.Headers) == 0 {
		return nil
	}
	value, present := token.Headers[0].ExtraHeaders[jose.HeaderType]
	if !present {
		return nil
	}
	typ, ok := value.(string)
	if !ok {
		return reject(ReasonMalformed, "typ header must be a string")
	}
	if _, denied := nonBearerTypes[normalizeMediaType(typ)]; denied {
		return reject(ReasonTypeNotBearer, "typ %q is not a bearer assertion", typ)
	}
	return nil
}

// normalizeMediaType compares typ values per RFC 7515 section 4.1.9: media
// types are case-insensitive, the "application/" prefix is optional, and
// parameters do not change the type.
func normalizeMediaType(typ string) string {
	base, _, _ := strings.Cut(typ, ";")
	base = strings.ToLower(strings.TrimSpace(base))
	return strings.TrimPrefix(base, "application/")
}

func reasonForClaimError(err error) Reason {
	switch {
	case errors.Is(err, jwt.ErrInvalidIssuer), errors.Is(err, jwt.ErrInvalidSubject):
		return ReasonSubjectMismatch
	case errors.Is(err, jwt.ErrInvalidAudience):
		return ReasonAudienceMismatch
	case errors.Is(err, jwt.ErrExpired):
		return ReasonExpired
	case errors.Is(err, jwt.ErrNotValidYet), errors.Is(err, jwt.ErrIssuedInTheFuture):
		return ReasonNotYetValid
	default:
		return ReasonMalformed
	}
}
