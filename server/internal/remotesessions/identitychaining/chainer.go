package identitychaining

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

const (
	// accessExpirySkew is how long before expiry a chained access token stops
	// being reused, so a proxied call never starts with a token that expires
	// in flight.
	accessExpirySkew = time.Minute

	// unknownExpiryLifetime caps reuse of a downstream token whose provider
	// reported no expiry. Short enough that revocation upstream is honored
	// soon; long enough to keep one conversation to one exchange.
	unknownExpiryLifetime = 5 * time.Minute

	// maxAccessLifetime caps reuse of any downstream token, so a token the
	// upstream revoked is replaced within the hour even without a fresh
	// sign-in. An hour keeps typical use under Okta's per-user monthly ID-JAG
	// allowance.
	maxAccessLifetime = time.Hour

	// attemptTimeout bounds one provider's attempt end to end: a delegation
	// refresh, the token exchange and the redemption.
	attemptTimeout = 25 * time.Second

	// acquireTimeout bounds the complete acquisition across every provider
	// in the order, including retries, queueing and token publication.
	acquireTimeout = 30 * time.Second

	// legTimeout bounds each token endpoint request.
	legTimeout = 10 * time.Second

	// lockTTL outlives attemptTimeout so a slow holder keeps single flight
	// until it has published or given up.
	lockTTL = 30 * time.Second

	// waitBudget is how long a request waits for a concurrent holder's
	// credential before reporting a transient failure instead of exchanging
	// again against the provider's per-user assertion quota.
	waitBudget = 10 * time.Second

	// waitInterval is the credential poll cadence while waiting.
	waitInterval = 200 * time.Millisecond

	// attemptResultTTL publishes a holder's failure to the requests waiting
	// on it; it outlives waitBudget so every waiter sees it.
	attemptResultTTL = waitBudget + 5*time.Second

	// failureTTL suppresses repeated exchanges after a provider rejection,
	// which an unconfigured tenant returns on every attempt.
	failureTTL = 30 * time.Second

	// recentKeyWindow is how long after a Speakeasy signing key is published
	// a vendor may still verify against a key set that lacks it: the
	// activation delay plus one vendor key set refresh. A compromise
	// replacement signs at once, so its first assertions can be rejected for
	// an unknown kid until the vendor refetches.
	recentKeyWindow = internalsessionissuers.ActivationDelay + internalsessionissuers.CacheTTL

	// consentAcceptedTTL is how long a consent page reports a vendor's
	// acceptance it could not store, because the subject's one credential
	// slot is held, instead of redeeming again on every reload. Within it,
	// "connected" means a recent vendor acceptance, not a stored token, and
	// can outlive a vendor-side refusal; local sign-in, issuer and provider
	// checks still run on every load.
	consentAcceptedTTL = 5 * time.Minute

	// releaseTimeout bounds the detached single-flight release.
	releaseTimeout = 2 * time.Second

	// lastUsedCutoff throttles last_used_at stamps, so a hot credential does
	// not write on every proxied call. Matches interactive remote sessions.
	lastUsedCutoff = 5 * time.Minute

	// maxClockSkew tolerates drift between Speakeasy and the identity provider when
	// validating an ID-JAG's temporal claims.
	maxClockSkew = time.Minute
)

// meterAttempts counts identity chaining attempts per EMA provider.
const meterAttempts = "identity_chaining.attempt"

// Chainer obtains resource-bound downstream access tokens for a human by
// identity chaining. It is safe for concurrent use.
type Chainer struct {
	Governor
	challenges *remotesessions.ChallengeManager
	keys       *jwks.KeyResolver
	locks      cache.Cache
	observerMu sync.RWMutex
	observer   Observer
	attempts   metric.Int64Counter
}

// New builds a chainer over the challenge manager's egress, tunnel and
// client-assertion configuration. keys verifies identity provider assertions;
// locks serializes acquisition across processes and shares failures.
func New(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, challenges *remotesessions.ChallengeManager, delegation *remotesessions.DelegationService, keys *jwks.KeyResolver, locks cache.Cache) *Chainer {
	c := &Chainer{
		Governor:   *NewGovernor(logger, db, enc, delegation),
		challenges: challenges,
		keys:       keys,
		locks:      locks,
		observerMu: sync.RWMutex{},
		observer:   nil,
		attempts:   nil,
	}
	c.sources[remotesessions.EMAProviderExternal] = func(p remotesessions.EMAProvider) tokenSource {
		return externalSource{g: &c.Governor, c: c, issuer: p.RemoteSessionIssuerID}
	}
	c.recentRefusal = c.recentFailure
	c.SetMeterProvider(noop.NewMeterProvider())
	return c
}

// recentFailure reads the definitive provider refusal acquisition shares for
// sel, which outlives the attempt by failureTTL.
func (c *Chainer) recentFailure(ctx context.Context, req Request, sel selection) (Outcome, bool) {
	var cached Outcome
	if c.locks == nil || c.locks.Get(ctx, cacheKey("identityChainingFailure", req, sel), &cached) != nil || cached.Reason == "" {
		return cached, false
	}
	cached.Cached = true
	return cached, true
}

// SetMeterProvider records attempts on meterProvider. Call it during wiring,
// before the chainer serves requests.
func (c *Chainer) SetMeterProvider(meterProvider metric.MeterProvider) {
	counter, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining").Int64Counter(
		meterAttempts,
		metric.WithDescription("Identity chaining attempts by EMA provider, outcome, whether an earlier provider in the order failed, retries and, when the provider could not serve, why."),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		c.logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterAttempts), attr.SlogError(err))
		counter, _ = noop.NewMeterProvider().Meter("").Int64Counter(meterAttempts)
	}
	c.attempts = counter
}

// Acquire returns a usable downstream token for the request's upstream, from
// the credential store when a still-valid one matches current provenance, or
// by a fresh exchange. Outcomes that are not Applicable leave the caller's
// interactive behavior unchanged.
func (c *Chainer) Acquire(ctx context.Context, req Request) (Token, Outcome) {
	var none Token
	if !req.complete() || req.UserID == "" {
		return none, notApplicable
	}
	logger := c.logger.With(
		attr.SlogOrganizationID(req.OrganizationID),
		attr.SlogProjectID(req.ProjectID.String()),
		attr.SlogUserSessionIssuerID(req.UserSessionIssuerID.String()),
		attr.SlogUserID(req.UserID),
	)

	sel, outcome := c.selectBinding(ctx, logger, req)
	if !outcome.Succeeded() {
		return none, outcome
	}
	logger = logger.With(
		attr.SlogRemoteSessionIssuerID(sel.remoteIssuerID.String()),
		attr.SlogRemoteSessionClientID(sel.clientID.String()),
		attr.SlogOAuthResource(sel.resource),
	)
	order, outcome := c.providerOrder(ctx, logger, req, sel)
	if !outcome.Succeeded() {
		return none, outcome
	}
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()
	if token, outcome, ok := c.reuseStored(ctx, logger, req, sel, order); ok {
		return token, outcome
	}
	return c.tryProviders(ctx, logger, "acquire", order, func(ctx context.Context, logger *slog.Logger, p remotesessions.EMAProvider) (Token, Outcome) {
		return c.acquireFrom(ctx, logger, req, sel, p)
	})
}

// acquireFrom obtains a token for sel from one source: a still-valid stored
// credential, or a fresh issue and redemption under single flight.
func (c *Chainer) acquireFrom(ctx context.Context, logger *slog.Logger, req Request, sel selection, p remotesessions.EMAProvider) (Token, Outcome) {
	var none Token
	src := c.source(p)
	sel.provider = p
	// release logs a credential's source only once release lets it out.
	release := func(token Token, source credentialSource) (Token, Outcome) {
		token, outcome := c.release(ctx, logger, req, sel, src, token)
		if outcome.Succeeded() && source != credentialNone {
			logCredentialSource(ctx, logger, sel, source)
		}
		return token, outcome
	}
	if outcome := src.authorize(ctx, logger, req, &sel); !outcome.Succeeded() {
		return none, outcome
	}
	// The local source's authorization already checks every reuse predicate.
	// Other sources retain their independent delegation/reuse gate.
	if _, local := src.(speakeasySource); !local {
		if outcome := src.checkReuse(ctx, req, sel); !outcome.Succeeded() {
			return none, outcome
		}
	}

	// Stored credentials are checked against their issuing provider and exact
	// provenance.
	if token, ok := c.cachedToken(ctx, logger, req, sel); ok {
		return release(token, credentialCached)
	}
	if token, source, ok := c.adoptConsentCredential(ctx, logger, req, sel); ok {
		return release(token, source)
	}
	if c.consentAccepted(ctx, req, sel) {
		return release(none, credentialNone)
	}
	failureKey := cacheKey("identityChainingFailure", req, sel)
	if cached, ok := c.recentFailure(ctx, req, sel); ok {
		return none, cached
	}

	lockKey := cacheKey("identityChainingLock", req, sel)
	held, err := c.locks.Add(ctx, lockKey, lockTTL)
	switch {
	case err != nil:
		// Exchanging unserialized beats refusing to exchange; the publish
		// statement still rejects stale provenance.
		logger.WarnContext(ctx, "identity chaining lock unavailable; exchanging without single flight", attr.SlogCacheKey(lockKey), attr.SlogError(err))
	case !held:
		token, outcome := c.awaitConcurrentAcquisition(ctx, logger, req, sel)
		if !outcome.Succeeded() {
			return none, outcome
		}
		return release(token, credentialNone)
	default:
		lockedAt := c.now()
		defer o11y.LogDefer(ctx, logger, "release identity chaining lock", func() error {
			// Past TTL the key may be a new holder's lock; TTL reaps ours.
			if c.now().Sub(lockedAt) >= lockTTL {
				return nil
			}
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
			defer cancel()
			return c.locks.Delete(releaseCtx, lockKey)
		})
		// A holder that finished while this request waited for the lock
		// already published its credential.
		if token, ok := c.cachedToken(ctx, logger, req, sel); ok {
			return release(token, credentialCached)
		}
		if c.consentAccepted(ctx, req, sel) {
			return release(none, credentialNone)
		}
	}

	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	startedAt := c.now()
	token, outcome, grantValidated := c.acquire(attemptCtx, logger, req, sel, src)
	outcome.Provider = src.kind()
	if !outcome.Succeeded() {
		// A consent page has no later retry, so a vendor that may not have
		// fetched a new key yet reads as unknown there, never as a refusal.
		if req.consent() && c.signedWithRecentKey(sel, outcome, grantValidated) {
			outcome.Retryable = true
		}
		logAcquisitionFailure(ctx, logger, outcome, sel)
		outcome.providerDescription = ""
		// Requests waiting on this attempt adopt its failure instead of timing out.
		attemptKey := cacheKey("identityChainingAttempt", req, sel)
		if err := c.locks.Set(context.WithoutCancel(ctx), attemptKey, attemptResult{Outcome: outcome, FinishedAt: c.now()}, attemptResultTTL); err != nil {
			logger.WarnContext(ctx, "record identity chaining attempt", attr.SlogCacheKey(attemptKey), attr.SlogError(err))
		}
		if outcome.cacheable() && !c.signedWithRecentKey(sel, outcome, grantValidated) {
			if err := c.locks.Set(context.WithoutCancel(ctx), failureKey, outcome, failureTTL); err != nil {
				logger.WarnContext(ctx, "record identity chaining failure", attr.SlogCacheKey(failureKey), attr.SlogError(err))
			}
		}
	} else if !req.consent() {
		// The consent page only records the outcome; nothing is released there.
		logCredentialSource(ctx, logger, sel, credentialMinted)
	}
	// After publishing to waiters; the caller waits at most observeTimeout.
	c.observe(ctx, logger, Observation{
		OrganizationID:  req.OrganizationID,
		Provider:        src.kind(),
		TrustedIssuerID: sel.trustedIssuerID,
		RemoteIssuerID:  sel.remoteIssuerID,
		RemoteIssuer:    sel.issuer,
		Audience:        sel.audience,
		Resource:        sel.resource,
		Outcome:         outcome,
		GrantValidated:  grantValidated,
		StartedAt:       startedAt,
	})
	return token, outcome
}

// awaitConcurrentAcquisition adopts a concurrent holder's credential, its
// consent acceptance, or its failure, instead of spending another assertion on the same identity. A
// holder that finishes neither within the wait budget is reported transient.
func (c *Chainer) awaitConcurrentAcquisition(ctx context.Context, logger *slog.Logger, req Request, sel selection) (Token, Outcome) {
	var none Token
	timedOut := newOutcome(StagePersistence, ReasonTransientFailure, ConfidenceVerified, true)
	attemptKey := cacheKey("identityChainingAttempt", req, sel)
	startedAt := c.now()
	deadline := time.NewTimer(waitBudget)
	defer deadline.Stop()
	tick := time.NewTicker(waitInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return none, timedOut
		case <-deadline.C:
			return none, timedOut
		case <-tick.C:
			if token, ok := c.cachedToken(ctx, logger, req, sel); ok {
				return token, success
			}
			if c.consentAccepted(ctx, req, sel) {
				return none, success
			}
			var result attemptResult
			if err := c.locks.Get(ctx, attemptKey, &result); err == nil && result.Outcome.Reason != "" && !result.FinishedAt.Before(startedAt) {
				return none, result.Outcome
			}
		}
	}
}

// cacheKey hashes the chained identity so resources of any length and content
// make bounded, delimiter-safe keys. The binding generation, the trusted
// identity provider registration and the audience are part of the identity, so
// a rebind, changed trust or newly confirmed audience never inherits an
// earlier failure or lock. External sources keep the key format that predates
// EMA providers, so old and new processes share single flight during a
// rolling deploy; other providers are qualified by kind.
func cacheKey(prefix string, req Request, sel selection) string {
	parts := []string{req.OrganizationID, req.ProjectID.String(), req.UserSessionIssuerID.String(), req.UserID}
	if sel.provider.Kind != remotesessions.EMAProviderExternal {
		parts = append(parts, string(sel.provider.Kind), req.UserSessionJTI, sel.signInID.String(), sel.provider.InternalSessionIssuerID.String(), sel.internalKeyID.String(), sel.internalIssuerVersion.UTC().Format(time.RFC3339Nano))
	}
	parts = append(parts,
		sel.bindingID.String(), strconv.FormatInt(sel.generation, 10),
		sel.trustedIssuerID.String(), sel.trustedClientID.String(), sel.audience,
		sel.clientID.String(), sel.resource, strings.Join(sel.scopes, " "),
	)
	digest := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return prefix + ":" + hex.EncodeToString(digest[:])
}

// consentAccepted reports a vendor acceptance a consent check recorded
// because it could not store the credential. No token comes with it: callers
// still run release's authority checks.
func (c *Chainer) consentAccepted(ctx context.Context, req Request, sel selection) bool {
	if !req.consent() {
		return false
	}
	var accepted bool
	err := c.locks.Get(ctx, cacheKey("identityChainingConsentAccepted", req, sel), &accepted)
	return err == nil && accepted
}

// signedWithRecentKey reports a Speakeasy redemption rejection of an assertion
// signed by a key the vendor may not have fetched yet. Only the ambiguous
// rejections an unknown signing key draws qualify, and only once an assertion
// was signed; local configuration, client and scope refusals stay definitive.
// It is not cached, so the next request retries as soon as the vendor sees
// the key.
func (c *Chainer) signedWithRecentKey(sel selection, outcome Outcome, signed bool) bool {
	if !signed || sel.provider.Kind != remotesessions.EMAProviderSpeakeasy || outcome.Stage != StageRedemption {
		return false
	}
	if outcome.Reason != ReasonInvalidGrant && outcome.Reason != ReasonUnknownRejection {
		return false
	}
	return !sel.internalKeyPublishedAt.IsZero() && c.now().Sub(sel.internalKeyPublishedAt) < recentKeyWindow
}

// logCredentialSource records where a Speakeasy credential released to a tool
// call came from. Reuse is logged at debug: it happens on every proxied call.
func logCredentialSource(ctx context.Context, logger *slog.Logger, sel selection, source credentialSource) {
	if sel.provider.Kind != remotesessions.EMAProviderSpeakeasy {
		return
	}
	level := slog.LevelInfo
	if source == credentialCached {
		level = slog.LevelDebug
	}
	logger.Log(ctx, level, "identity chaining credential released", attr.SlogIdentityChainingCredentialSource(source))
}

// logAcquisitionFailure logs a failed attempt with the provider's error text
// and the scopes requested, which always come from the binding.
func logAcquisitionFailure(ctx context.Context, logger *slog.Logger, outcome Outcome, sel selection) {
	logger.WarnContext(ctx, "identity chaining failed",
		attr.SlogOutcome(string(outcome.Reason)),
		attr.SlogReason(string(outcome.Stage)),
		attr.SlogOAuthErrorDescription(outcome.providerDescription),
		attr.SlogOAuthScope(strings.Join(sel.scopes, " ")),
		attr.SlogOAuthScopeSource(remotesessionmetrics.ScopeSourceEMABinding),
	)
}
