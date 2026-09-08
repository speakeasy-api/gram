package clientauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

const (
	// DefaultMaxLifetime bounds how far an assertion's exp may sit in the
	// future when an Expectation names no bound of its own.
	//
	// One hour matches what real implementations emit and what the strictest
	// mainstream profile permits: FAPI 2.0 caps client assertion lifetime at
	// 60 minutes, Google service account assertions and Okta both top out
	// there, and common client libraries default well inside it. A tighter
	// ceiling would reject stock clients with an invalid_client they could
	// not diagnose.
	//
	// It is the *client assertion* bound. A profile whose tokens are minted
	// by someone else's platform carries its own on Expectation.MaxLifetime,
	// which is also where the reason not to widen this constant lives.
	//
	// The bound is not doing replay work — the jti guard covers the whole
	// validity window — it bounds how long that guard must remember each
	// identifier, so the ceiling is a keyspace decision.
	DefaultMaxLifetime = time.Hour

	// MaxSkew is the clock difference tolerated on every temporal claim, in
	// both directions. Client and server clocks genuinely drift; a window
	// wider than this starts extending the life of an expired assertion for
	// no interoperability gain.
	MaxSkew = time.Minute

	// DefaultMaxReplayHold is the hold a guard must provide to serve
	// assertions bounded at DefaultMaxLifetime. It is the floor NewVerifier
	// enforces, because every verifier can be handed a default-bounded
	// expectation.
	DefaultMaxReplayHold = DefaultMaxLifetime + 2*MaxSkew
)

// ReplayHoldFor is how long a spent identifier must be remembered for
// assertions bounded at lifetime: the full window in which an accepted
// assertion can still verify.
//
// The skew counts twice. A party whose clock runs ahead may present an exp up
// to MaxSkew beyond the bound and still be accepted, and an assertion is then
// honoured until MaxSkew after that exp. Releasing an identifier anywhere
// inside that window is exactly what a replay needs.
//
// Size a guard with the longest lifetime it will be asked to serve. Verify
// refuses an expectation whose hold would exceed the guard's cap rather than
// letting it through, because Guard.Reserve clamps a longer hold silently and
// nothing at request time would otherwise notice the reservation lapsing
// while the assertion still verifies.
func ReplayHoldFor(lifetime time.Duration) time.Duration {
	return lifetime + 2*MaxSkew
}

// Verifier authenticates clients presenting assertions. Safe for concurrent
// use; construct one at wiring time so its dependencies are created once.
type Verifier struct {
	// keys resolves the key an assertion's kid names from the client's
	// published key set, refreshing on an unknown kid within the fleet-wide
	// rate limit.
	keys *jwks.KeyResolver

	// guard remembers spent assertion identifiers for at least
	// MaxReplayHold.
	guard *replay.Guard
}

// NewVerifier binds the key resolver and replay guard a verification needs.
// Both are required: a verifier without key resolution cannot check a
// signature, and one without replay memory would accept every assertion for
// as long as it stays valid, which is the property the jti exists to remove.
//
// The guard's cap must cover DefaultMaxReplayHold. A shorter cap would
// release identifiers while their assertions still verify, and nothing at
// request time would notice: the guard would report success and the replay
// would be accepted. Checked here so the mismatch is a wiring error, not a
// silent weakening.
//
// The check is a floor. An Expectation carrying a longer MaxLifetime needs a
// proportionally longer hold, which cannot be known here because it varies
// per request, so Verify re-checks the guard against the expectation it is
// actually given.
func NewVerifier(keys *jwks.KeyResolver, guard *replay.Guard) (*Verifier, error) {
	if keys == nil {
		return nil, errors.New("clientauth: Verifier requires a key resolver")
	}
	if guard == nil {
		return nil, errors.New("clientauth: Verifier requires a replay guard")
	}
	if guard.MaxHold() < DefaultMaxReplayHold {
		return nil, fmt.Errorf("clientauth: replay guard holds identifiers for %s, but assertions stay acceptable for up to %s", guard.MaxHold(), DefaultMaxReplayHold)
	}
	return &Verifier{keys: keys, guard: guard}, nil
}

// Verify authenticates a client from its assertion, returning what was
// observed about an accepted one. Every failure is an *Error carrying a
// Reason; callers must answer all of them identically on the wire.
//
// An accepted assertion has its jti spent here, before the caller goes on to
// evaluate the grant. A request that then fails on its grant (a bad code, a
// PKCE mismatch) has consumed the assertion, and a retry with the same one is
// a replay. That is the standard RFC 7523 posture, and clients mint a fresh
// assertion per request for exactly this reason.
func (v *Verifier) Verify(ctx context.Context, assertion Assertion, expect Expectation) (*Result, error) {
	if err := expect.validate(); err != nil {
		return nil, err
	}
	// Before anything is parsed: an expectation whose assertions outlive
	// what the guard remembers cannot be verified safely at all, and
	// Reserve would clamp the hold rather than complain. A wiring fault,
	// labelled as one.
	hold := expect.replayHold()
	if v.guard.MaxHold() < hold {
		return nil, reject(ReasonVerifierMisconfigured, "replay guard holds identifiers for %s, but this assertion stays acceptable for up to %s", v.guard.MaxHold(), hold)
	}
	switch {
	case assertion.Value == "":
		return nil, reject(ReasonAssertionMissing, "client_assertion is required")
	case assertion.Type == "":
		return nil, reject(ReasonTypeUnsupported, "client_assertion_type is required alongside client_assertion")
	case assertion.Type != AssertionType:
		return nil, reject(ReasonTypeUnsupported, "client_assertion_type %q is not supported", assertion.Type)
	}

	token, err := parseAssertion(assertion.Value)
	if err != nil {
		return nil, err
	}

	// A compact JWS carries exactly one signature, so Headers[0] is the one
	// that names the key.
	key, err := v.keys.VerificationKey(ctx, expect.KeySource, token.Headers[0].KeyID)
	switch {
	case err == nil:
	case errors.Is(err, jwks.ErrKeyNotFound), errors.Is(err, jwks.ErrRefreshRateLimited), errors.Is(err, jwks.ErrFetchRateLimited):
		// A rate-limited refresh or fetch is reported to the client exactly
		// like an unknown key: the distinction is operational, not
		// something the client can act on, and the wrapped cause names it
		// for the log.
		return nil, rejectWith(ReasonKeyUnknown, err)
	default:
		return nil, rejectWith(ReasonKeyUnresolvable, err)
	}

	var claims jwt.Claims
	if err := token.Claims(key, &claims); err != nil {
		return nil, rejectWith(ReasonSignatureInvalid, err)
	}

	// exp is checked for presence before the library validation, which
	// treats an absent exp as "never expires": RFC 7523 §3 requires it, and
	// without one the replay hold has no end.
	if claims.Expiry == nil {
		return nil, reject(ReasonExpiryMissing, "exp is required")
	}
	now := time.Now()
	if err := claims.ValidateWithLeeway(jwt.Expected{
		Issuer:      expect.Issuer,
		Subject:     expect.Subject,
		AnyAudience: expect.Audiences.accepted(),
		Time:        now,
		ID:          "",
	}, MaxSkew); err != nil {
		return nil, rejectWith(reasonForClaimError(err), err)
	}
	expiresAt := claims.Expiry.Time()
	lifetime := expect.lifetime()
	if expiresAt.After(now.Add(lifetime + MaxSkew)) {
		return nil, reject(ReasonLifetimeTooLong, "exp is more than %s in the future", lifetime)
	}

	// The library check proved aud intersects the accepted set; this
	// reports which member, for the log line.
	audience, _ := expect.Audiences.Match(claims.Audience)

	if claims.ID == "" {
		return nil, reject(ReasonIDMissing, "jti is required")
	}
	// Last, so an assertion rejected for any other reason does not spend an
	// identifier and grow the keyspace.
	claimed, err := v.guard.Reserve(ctx, replay.Key{
		Issuer:  expect.ReplayIssuer,
		Party:   expect.ReplayParty,
		Subject: expect.ReplaySubject,
		ID:      claims.ID,
	}, expiresAt.Add(MaxSkew))
	if err != nil {
		return nil, rejectWith(ReasonReplayStoreUnavailable, err)
	}
	if !claimed {
		return nil, reject(ReasonReplayed, "jti has already been presented")
	}

	return &Result{Audience: audience, ExpiresAt: expiresAt}, nil
}

// reasonForClaimError maps the library's claim validation sentinels onto this
// package's rejection labels. Every sentinel the Expected passed to
// ValidateWithLeeway can produce is covered; the fallback exists so a future
// library sentinel is still a rejection rather than a panic or an acceptance.
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
