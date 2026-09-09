// Admission for the workload assertion grant: resolving an assertion's iss to
// the issuer row the rest of the grant is built on. Sits in front of
// workloadIssuerKeySource in authnchallenge_workloadauth.go, which reads that
// row's stored jwks_uri.
//
// Resolving the issuer is not the same as admitting the workload, and this
// stage only does the first. See errWorkloadIssuerUntrusted.

package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

const (
	// workloadIssuerMissTTL is how long a miss is remembered.
	//
	// Deliberately short: this absorbs a burst of identical rejections rather
	// than recording what is untrusted, and a long entry keeps a newly added
	// issuer rejected on replicas already holding one.
	workloadIssuerMissTTL = 30 * time.Second

	// workloadIssuerLookupTimeout bounds one admission lookup. The lookup runs
	// detached from the caller's context, so nothing else bounds it.
	workloadIssuerLookupTimeout = 5 * time.Second
)

// workloadIssuerLookupRate bounds how many admission lookups one endpoint can
// drive into the database. This is the bound; the miss cache is an
// optimization on top of it, since a flood of distinct issuer spellings misses
// the cache every time and singleflight collapses none of them.
//
// Keyed per endpoint rather than per replica: a process-wide budget would let
// one tenant exhaust every other tenant's, turning a mitigation into a
// cross-tenant denial surface. The bucket lives in Redis, so it is fleet-wide
// rather than multiplied by the replica count.
var workloadIssuerLookupRate = ratelimit.PerMinute(120).WithBurst(30)

// errWorkloadIssuerUntrusted reports an assertion whose iss resolves to no
// issuer row visible to the endpoint's tenancy.
//
// "Visible to the tenancy" is deliberately weaker than "trusted by this
// endpoint". This stage establishes only that Gram knows the issuer and holds
// keys for it; a CI provider's issuer mints valid tokens for every job on its
// platform, and nothing here tells ours from anyone else's. The subject-level
// admission that follows is the security boundary of the grant, not this.
var errWorkloadIssuerUntrusted = errors.New("issuer is not trusted by this endpoint")

// errWorkloadIssuerLookupRateLimited reports a lookup refused because the
// endpoint has spent its budget. Distinct from errWorkloadIssuerUntrusted:
// nothing was decided about this issuer, so a caller mapping untrusted onto a
// 401 must not answer one here.
var errWorkloadIssuerLookupRateLimited = errors.New("workload issuer lookups are rate limited for this endpoint")

// errWorkloadIssuerLimiterUnavailable reports that the limiter's store could
// not answer. Fails closed: running the lookup unbounded because the thing
// that bounds it is down would spend exactly the budget the limiter protects.
// Kept separate from a refusal so an outage is not reported as a rate limit an
// operator can wait out.
var errWorkloadIssuerLimiterUnavailable = errors.New("workload issuer lookup limiter unavailable")

// workloadIssuerMissReason records why an issuer was rejected, so a repeat
// served from the cache answers with the same taxonomy the original did.
// Without it, a caller mapping a malformed iss to 400 and an unknown one to
// 401 would return different statuses for the same input depending on whether
// an entry happened to be live.
//
// The reason is stored; the parse error's detail is not. That text derives
// from an unauthenticated, unbounded value, and keeping it would put an
// attacker-sized string into an entry the cap is meant to bound.
type workloadIssuerMissReason uint8

const (
	// workloadIssuerMissUnknown: a well-formed issuer no tier-visible row
	// describes.
	workloadIssuerMissUnknown workloadIssuerMissReason = iota
	// workloadIssuerMissMalformed: not an issuer identifier at all, so no row
	// could ever describe it.
	workloadIssuerMissMalformed
)

// err renders the rejection this reason stands for.
func (r workloadIssuerMissReason) err() error {
	if r == workloadIssuerMissMalformed {
		return fmt.Errorf("%w: %w", errWorkloadIssuerUntrusted, remotesessions.ErrIssuerURLInvalid)
	}
	return errWorkloadIssuerUntrusted
}

// newWorkloadIssuerLookupBudget builds the per-endpoint ceiling, or nil when
// there is no store to hold the buckets.
//
// Nil is not "unlimited": newWorkloadIssuerAdmission treats an absent budget
// as unprotected and refuses. A deployment without the store does not get the
// grant.
func newWorkloadIssuerLookupBudget(redisClient *redis.Client, meterProvider metric.MeterProvider) workloadIssuerBudget {
	if redisClient == nil {
		return nil
	}

	limiter := ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		"workload_issuer_lookup",
		workloadIssuerLookupRate,
		ratelimit.WithMetrics(meterProvider),
	)

	return limiter.Allow
}

// workloadIssuerBudget charges one lookup against the ceiling for a scope,
// reporting whether it may proceed. A function rather than *ratelimit.Limiter
// so the refusal and outage paths can be exercised without the store behind
// them; production passes (*ratelimit.Limiter).Allow.
//
// !Allowed and an error are different answers: a refusal is a decision the
// budget made, an error is the store failing to make one.
type workloadIssuerBudget func(ctx context.Context, scope string) (ratelimit.Result, error)

// workloadIssuerLookup resolves an assertion's iss to the trusted issuer row
// an endpoint admits it under, reporting false when no tier-visible row
// describes it. Injected so admission can be tested without a database, and so
// the miss path can be shown to consult nothing further.
type workloadIssuerLookup func(ctx context.Context, endpoint *ResolvedMcpEndpoint, issuerURL string) (*remotesessions_repo.RemoteSessionIssuer, bool, error)

// newWorkloadIssuerLookup binds the shared resolver to a database handle, so
// admission cannot drift from what an operator sees when they look an issuer
// up through the management API.
func newWorkloadIssuerLookup(db remotesessions_repo.DBTX) workloadIssuerLookup {
	return func(ctx context.Context, endpoint *ResolvedMcpEndpoint, issuerURL string) (*remotesessions_repo.RemoteSessionIssuer, bool, error) {
		row, found, err := remotesessions.ResolveIssuerByURL(ctx, db, remotesessions.IssuerLookup{
			IssuerURL:      issuerURL,
			ProjectID:      endpoint.ProjectID,
			OrganizationID: endpoint.OrganizationID,
		})
		if err != nil {
			return nil, false, fmt.Errorf("resolve trusted issuer: %w", err)
		}
		if !found {
			return nil, false, nil
		}
		return &row, true, nil
	}
}

// workloadIssuerAdmission resolves an assertion's issuer to the row that
// describes it, remembering recent rejections so a repeated unknown issuer
// costs a cache read rather than a query.
//
// Safe for concurrent use; build one at wiring time.
type workloadIssuerAdmission struct {
	lookup workloadIssuerLookup
	misses *workloadIssuerMissCache

	// inflight collapses concurrent resolutions of one key onto a single
	// lookup. The miss cache alone bounds only the sustained cost: a burst
	// arriving together all passes the cache read before any of them records a
	// miss.
	inflight singleflight.Group

	// charge applies the per-endpoint ceiling on lookups reaching the database.
	// singleflight collapses repeats and the cache absorbs them over time;
	// neither bounds distinct spellings, and this does.
	//
	// Nil means no ceiling was wired, which admission treats as unprotected
	// rather than unlimited — deliberately unlike jwks.NewKeyResolver, whose
	// path is reached behind a registered client. This one is reachable by
	// anyone.
	charge workloadIssuerBudget
}

func newWorkloadIssuerAdmission(logger *slog.Logger, cacheImpl cache.Cache, lookup workloadIssuerLookup, charge workloadIssuerBudget) *workloadIssuerAdmission {
	return &workloadIssuerAdmission{
		lookup:   lookup,
		misses:   newWorkloadIssuerMissCache(logger, cacheImpl),
		inflight: singleflight.Group{},
		charge:   charge,
	}
}

// workloadIssuerLookupScope names the budget an endpoint's lookups are charged
// to: the authorization server's own identifier, which is the tenant boundary,
// so no endpoint can spend another's budget.
//
// Never the issuer URL, which two organizations may legitimately share, and
// never anything derived from the request, which would let a caller mint a
// fresh budget by varying what it sends. The prefix keeps this separate from
// the key fetches charged under workloadFetchScope.
func workloadIssuerLookupScope(endpoint *ResolvedMcpEndpoint) string {
	return "workload-issuer-lookup:" + endpoint.UserSessionIssuerID.String()
}

// workloadIssuerResolution is what one admitted lookup produced, carried
// through singleflight so every sharer of a call sees the same row.
type workloadIssuerResolution struct {
	row *remotesessions_repo.RemoteSessionIssuer
}

// admit resolves issuerURL to the issuer row it names, or reports
// errWorkloadIssuerUntrusted.
//
// Nothing on this path fetches: the key source reads a jwks_uri already stored
// on the row, so an unrecognised iss cannot become an outbound request. What a
// miss costs is one indexed SELECT — worth bounding anyway, because the grant
// is reachable without credentials, so the cheapest request anyone can produce
// would otherwise buy a query.
func (a *workloadIssuerAdmission) admit(ctx context.Context, endpoint *ResolvedMcpEndpoint, issuerURL string) (*remotesessions_repo.RemoteSessionIssuer, error) {
	key := workloadIssuerMissKey(endpoint, issuerURL)
	if reason, ok := a.misses.seen(ctx, key); ok {
		return nil, reason.err()
	}

	ch := a.inflight.DoChan(key, func() (any, error) {
		// Detached from the caller that opened the flight: values carry
		// through, cancellation does not. Tying the flight's lifetime to that
		// one caller would hand context.Canceled to everyone sharing the
		// lookup the moment it went away. The caller keeps its own
		// cancellation through the select below; what bounds the flight is
		// this timeout.
		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workloadIssuerLookupTimeout)
		defer cancel()

		// Re-check under the flight: a caller that read the cache before a
		// flight recorded its miss arrives here after that flight ended, and
		// would otherwise start a redundant lookup. Read on lookupCtx for the
		// same reason the lookup is.
		if reason, ok := a.misses.seen(lookupCtx, key); ok {
			return nil, reason.err()
		}

		// Charged before the query, so a refusal costs only the bucket read.
		// Neither outcome below is remembered as a miss: a spent budget and an
		// unreachable bucket are statements about load and about us, not about
		// this issuer.
		if a.charge == nil {
			return nil, errWorkloadIssuerLimiterUnavailable
		}
		charged, chargeErr := a.charge(lookupCtx, workloadIssuerLookupScope(endpoint))
		if chargeErr != nil {
			return nil, fmt.Errorf("%w: %w", errWorkloadIssuerLimiterUnavailable, chargeErr)
		}
		if !charged.Allowed {
			return nil, fmt.Errorf("%w: retry after %s", errWorkloadIssuerLookupRateLimited, charged.RetryAfter)
		}

		row, found, lookupErr := a.lookup(lookupCtx, endpoint, issuerURL)
		switch {
		case errors.Is(lookupErr, remotesessions.ErrIssuerURLInvalid):
			// No row could ever describe it, and a malformed iss is the
			// cheapest thing for a flood to carry.
			a.misses.remember(lookupCtx, key, workloadIssuerMissMalformed)
			return nil, fmt.Errorf("%w: %w", errWorkloadIssuerUntrusted, lookupErr)
		case lookupErr != nil:
			// Not evidence about this issuer, so never remembered: caching an
			// outage would keep rejecting a legitimate workload after the
			// store recovered.
			return nil, fmt.Errorf("resolve workload issuer: %w", lookupErr)
		case !found:
			a.misses.remember(lookupCtx, key, workloadIssuerMissUnknown)
			return nil, errWorkloadIssuerUntrusted
		}
		return workloadIssuerResolution{row: row}, nil
	})

	select {
	case <-ctx.Done():
		// This caller gave up; the flight carries on for whoever else shares
		// it and still records its miss. Deliberately not
		// errWorkloadIssuerUntrusted — nothing was decided about this issuer.
		return nil, fmt.Errorf("await workload issuer admission: %w", ctx.Err())
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		resolution, ok := res.Val.(workloadIssuerResolution)
		if !ok {
			return nil, fmt.Errorf("resolve workload issuer: unexpected resolution %T", res.Val)
		}
		return resolution.row, nil
	}
}

// workloadIssuerMissKey identifies one remembered miss.
//
// Two calls share a key exactly when they would share a lookup result:
// narrower and a rejection gets served to a request that would have resolved,
// wider only fragments the cache. So the key is the inputs the lookup consumes
// — the tenancy it resolves under, and the spelling it resolves.
//
// Tenancy is the organization and project, NOT the user session issuer: an
// mcp_servers row references its issuer without project pinning, so one issuer
// can back endpoints in different projects, and keying on it alone would let a
// miss in one project deny a project-tier issuer in another.
//
// Keyed on the supplied spelling rather than a canonical form, which is the
// non-obvious half. Lookup matches a closed set of spellings, so two inputs
// sharing a canonical form do not necessarily share a result — a row stored as
// https://IDP.example.com is missed by a request spelling it in lowercase, and
// collapsing those onto one key would serve the miss to the spelling that
// would have matched.
//
// Hashed and length-prefixed, as replay.Key is: the spelling arrives
// unauthenticated under no length bound, so a digest makes every entry the
// same size and the entry cap a true memory bound.
func workloadIssuerMissKey(endpoint *ResolvedMcpEndpoint, issuerURL string) string {
	sum := sha256.New()
	for _, part := range []string{endpoint.OrganizationID, endpoint.ProjectID.String(), issuerURL} {
		sum.Write([]byte(strconv.Itoa(len(part))))
		sum.Write([]byte(":"))
		sum.Write([]byte(part))
	}
	return base64.RawURLEncoding.EncodeToString(sum.Sum(nil))
}

// workloadIssuerMiss is one remembered rejection.
type workloadIssuerMiss struct {
	// Key addresses the entry: the tenancy and spelling digest from
	// workloadIssuerMissKey. Not serialized — it is where the entry lives, not
	// part of what it says.
	Key string `json:"-"`

	// Reason is the rejection this entry answers with.
	Reason workloadIssuerMissReason `json:"reason"`
}

// workloadIssuerMissCacheKey namespaces a miss digest so it cannot collide
// with anything else sharing the store.
func workloadIssuerMissCacheKey(key string) string {
	return "workload_issuer_miss:" + key
}

// CacheKey implements [cache.CacheableObject].
func (m workloadIssuerMiss) CacheKey() string {
	return workloadIssuerMissCacheKey(m.Key)
}

// TTL is how long the rejection stands. See workloadIssuerMissTTL.
func (m workloadIssuerMiss) TTL() time.Duration {
	return workloadIssuerMissTTL
}

// workloadIssuerMissCache remembers recently rejected issuers in the shared
// cache. Shared rather than per-replica, matching the limiter next door: a
// per-replica cache multiplies the cost of an unknown issuer by the replica
// count, reintroducing the multiplier a fleet-wide budget exists to avoid.
//
// What bounds it is upstream. A miss is only recorded after a lookup a budget
// charge admitted, so the limiter bounding queries bounds writes here by the
// same amount — a small resident set per endpoint, of fixed-size entries.
type workloadIssuerMissCache struct {
	entries cache.TypedCacheObject[workloadIssuerMiss]
	logger  *slog.Logger
}

func newWorkloadIssuerMissCache(logger *slog.Logger, cacheImpl cache.Cache) *workloadIssuerMissCache {
	logger = logger.With(attr.SlogCacheNamespace("workload_issuer_miss"))
	return &workloadIssuerMissCache{
		entries: cache.NewTypedObjectCache[workloadIssuerMiss](logger, cacheImpl, cache.SuffixNone),
		logger:  logger,
	}
}

// seen reports the rejection held for key, if one is still live.
//
// A store that cannot answer is reported as "not seen", so the lookup runs.
// That costs a query; answering a rejection from a failed read would deny a
// legitimate workload, which is worse, and the limiter still bounds the
// fallthrough.
func (c *workloadIssuerMissCache) seen(ctx context.Context, key string) (workloadIssuerMissReason, bool) {
	miss, err := c.entries.Get(ctx, workloadIssuerMissCacheKey(key))
	switch {
	case err == nil:
		return miss.Reason, true
	case errors.Is(err, redisCache.ErrCacheMiss):
		return workloadIssuerMissUnknown, false
	default:
		c.logger.WarnContext(ctx, "workload issuer miss cache read failed", attr.SlogError(err))
		return workloadIssuerMissUnknown, false
	}
}

// remember records key as rejected for reason.
//
// Set-if-absent rather than set, so repeating a miss cannot extend its expiry
// and hold an issuer rejected past the configuration change that added it.
// Redis applies the condition and the TTL in one command.
//
// A failed write is not an admission failure: the rejection is returned
// regardless, and losing the entry costs the next repeat a query.
func (c *workloadIssuerMissCache) remember(ctx context.Context, key string, reason workloadIssuerMissReason) {
	if _, err := c.entries.StoreIfAbsent(ctx, workloadIssuerMiss{Key: key, Reason: reason}); err != nil {
		c.logger.WarnContext(ctx, "workload issuer miss cache write failed", attr.SlogError(err))
	}
}
