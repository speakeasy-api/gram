package identitychaining

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
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

	// attemptTimeout bounds one acquisition end to end: a delegation refresh,
	// the token exchange and the redemption.
	attemptTimeout = 25 * time.Second

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

	// releaseTimeout bounds the detached single-flight release.
	releaseTimeout = 2 * time.Second

	// lastUsedCutoff throttles last_used_at stamps, so a hot credential does
	// not write on every proxied call. Matches interactive remote sessions.
	lastUsedCutoff = 5 * time.Minute

	// maxClockSkew tolerates drift between Gram and the identity provider when
	// validating an ID-JAG's temporal claims.
	maxClockSkew = time.Minute
)

// Chainer obtains resource-bound downstream access tokens for a human by
// identity chaining. It is safe for concurrent use.
type Chainer struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	enc        *encryption.Client
	challenges *remotesessions.ChallengeManager
	delegation *remotesessions.DelegationService
	keys       *jwks.KeyResolver
	locks      cache.Cache
	now        func() time.Time
}

// New builds a chainer over the challenge manager's egress, tunnel and
// client-assertion configuration. keys verifies identity provider assertions;
// locks serializes acquisition across processes and shares failures.
func New(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, challenges *remotesessions.ChallengeManager, delegation *remotesessions.DelegationService, keys *jwks.KeyResolver, locks cache.Cache) *Chainer {
	return &Chainer{
		logger:     logger.With(attr.SlogComponent("identity_chaining")),
		db:         db,
		enc:        enc,
		challenges: challenges,
		delegation: delegation,
		keys:       keys,
		locks:      locks,
		now:        time.Now,
	}
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
	if outcome = c.authorize(ctx, logger, req, &sel); !outcome.Succeeded() {
		return none, outcome
	}

	if token, ok := c.cachedToken(ctx, logger, req, sel); ok {
		return token, success
	}
	failureKey := cacheKey("identityChainingFailure", req, sel)
	var cached Outcome
	if err := c.locks.Get(ctx, failureKey, &cached); err == nil && cached.Reason != "" {
		cached.Cached = true
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
		return c.awaitConcurrentAcquisition(ctx, logger, req, sel)
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
			return token, success
		}
	}

	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	token, outcome := c.acquire(attemptCtx, logger, req, sel)
	if !outcome.Succeeded() {
		logger.WarnContext(ctx, "identity chaining failed",
			attr.SlogOutcome(string(outcome.Reason)),
			attr.SlogReason(string(outcome.Stage)),
		)
		// Requests waiting on this attempt adopt its failure instead of timing out.
		attemptKey := cacheKey("identityChainingAttempt", req, sel)
		if err := c.locks.Set(context.WithoutCancel(ctx), attemptKey, attemptResult{Outcome: outcome, FinishedAt: c.now()}, attemptResultTTL); err != nil {
			logger.WarnContext(ctx, "record identity chaining attempt", attr.SlogCacheKey(attemptKey), attr.SlogError(err))
		}
		if outcome.cacheable() {
			if err := c.locks.Set(context.WithoutCancel(ctx), failureKey, outcome, failureTTL); err != nil {
				logger.WarnContext(ctx, "record identity chaining failure", attr.SlogCacheKey(failureKey), attr.SlogError(err))
			}
		}
	}
	return token, outcome
}

// Governs reports whether identity chaining owns the request's upstream: a
// ready binding names it. It reads configuration only and never contacts a
// provider.
func (c *Chainer) Governs(ctx context.Context, req Request) bool {
	if !req.complete() {
		return false
	}
	_, outcome := c.selectBinding(ctx, c.logger, req)
	return outcome.Applicable() && !outcome.Retryable
}

// awaitConcurrentAcquisition adopts a concurrent holder's credential, or its
// failure, instead of spending another assertion on the same identity. A
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
			var result attemptResult
			if err := c.locks.Get(ctx, attemptKey, &result); err == nil && result.Outcome.Reason != "" && !result.FinishedAt.Before(startedAt) {
				return none, result.Outcome
			}
		}
	}
}

// cacheKey hashes the chained identity so resources of any length and content
// make bounded, delimiter-safe keys. The binding generation and the trusted
// identity provider registration are part of the identity, so a rebind or a
// changed trust never inherits an earlier failure or lock.
func cacheKey(prefix string, req Request, sel selection) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		req.OrganizationID, req.ProjectID.String(), req.UserSessionIssuerID.String(), req.UserID,
		sel.bindingID.String(), strconv.FormatInt(sel.generation, 10),
		sel.trustedIssuerID.String(), sel.trustedClientID.String(),
		sel.clientID.String(), sel.resource, strings.Join(sel.scopes, " "),
	}, "\n")))
	return prefix + ":" + hex.EncodeToString(digest[:])
}
