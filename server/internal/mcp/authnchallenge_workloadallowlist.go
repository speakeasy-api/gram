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
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const (
	// workloadIssuerLookupTimeout bounds one admission lookup. The lookup runs
	// detached from the caller's context, so nothing else bounds it.
	workloadIssuerLookupTimeout = 5 * time.Second
)

// workloadIssuerLookupRate bounds how many admission lookups one endpoint can
// drive into the database. This is the only bound: singleflight collapses
// concurrent repeats of one spelling, but a flood of distinct spellings shares
// no flight, so nothing else stands between an anonymous caller and a query.
//
// Keyed per endpoint rather than per replica: a process-wide budget would let
// one tenant exhaust every other tenant's, turning a mitigation into a
// cross-tenant denial surface. The bucket lives in Redis, so it is fleet-wide
// rather than multiplied by the replica count.
var workloadIssuerLookupRate = ratelimit.PerMinute(120).WithBurst(30)

// errWorkloadIssuerUntrusted reports an assertion whose iss resolves to no
// workload issuer row in the addressed endpoint's tenancy — its own project,
// or the organization above it. There is no platform tier to inherit from.
//
// "In the tenancy" is deliberately weaker than "trusted for this workload".
// This stage establishes only that the tenant registered the issuer and Gram
// holds keys for it; a CI provider's issuer mints valid tokens for every job
// on its platform, and nothing here tells ours from anyone else's. The
// subject-level admission that follows is the security boundary of the grant,
// not this.
var errWorkloadIssuerUntrusted = errors.New("no workload issuer in this tenancy describes that issuer url")

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

// errWorkloadIssuerURLInvalid marks a value that is not an issuer identifier
// at all — wrong scheme, no host, or carrying userinfo, a query, or a
// fragment. A lookup implementation wraps this when it rejects an input before
// consulting anything, so admission can tell "could never name a row" apart
// from "names no row we hold" without depending on which store answers.
var errWorkloadIssuerURLInvalid = errors.New("invalid issuer url")

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

// workloadIssuerLookup resolves an assertion's iss to the id of the workload
// issuer row the addressed endpoint's tenant registered for it, reporting
// false when no row in that tenancy describes it. The endpoint is the input
// because it names the tenancy — its project and the organization above it —
// which is exactly the scope the resolution runs against. Injected so
// admission can be tested without a database, and so the miss path can be
// shown to consult nothing further.
//
// An id rather than the row itself: admission only ever needs to name the
// issuer, and everything downstream keys on that id. Returning a row would tie
// this file to whichever table holds it, which is the coupling the workload
// issuer schema decision removed.
//
// An input that is not an issuer identifier is reported as an error wrapping
// errWorkloadIssuerURLInvalid, and must be rejected before the store is
// consulted.
type workloadIssuerLookup func(ctx context.Context, endpoint *ResolvedMcpEndpoint, issuerURL string) (uuid.UUID, bool, error)

// workloadIssuerAdmission resolves an assertion's issuer to the row that
// describes it.
//
// Safe for concurrent use; build one at wiring time.
type workloadIssuerAdmission struct {
	lookup workloadIssuerLookup

	// inflight collapses concurrent resolutions of one key onto a single
	// lookup, so a burst of identical spellings costs one query rather than
	// one each. In-process, which is all it needs to be: it bounds a
	// simultaneous burst, while sustained load is the limiter's job.
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

func newWorkloadIssuerAdmission(lookup workloadIssuerLookup, charge workloadIssuerBudget) *workloadIssuerAdmission {
	return &workloadIssuerAdmission{
		lookup:   lookup,
		inflight: singleflight.Group{},
		charge:   charge,
	}
}

// workloadIssuerLookupScope names the budget an endpoint's lookups are charged
// to: the authorization server's own identifier, so no endpoint can spend
// another's budget.
//
// This is a denial-of-service bound and is deliberately NOT the tenancy the
// lookup resolves against — that is the endpoint's project and organization,
// carried by the miss key. One tenant running several MCP servers gets a
// budget per server, which is the granularity that keeps a flood against one
// from starving the rest.
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
	issuerID uuid.UUID
}

// admit resolves issuerURL to the issuer row it names, or reports
// errWorkloadIssuerUntrusted.
//
// Nothing on this path fetches: the key source reads a jwks_uri already stored
// on the row, so an unrecognised iss cannot become an outbound request. What a
// miss costs is one indexed SELECT against a tenant-scoped table — worth
// bounding anyway, because the grant is reachable without credentials, so the
// cheapest request anyone can produce would otherwise buy a query.
func (a *workloadIssuerAdmission) admit(ctx context.Context, endpoint *ResolvedMcpEndpoint, issuerURL string) (uuid.UUID, error) {
	ch := a.inflight.DoChan(workloadIssuerFlightKey(endpoint, issuerURL), func() (any, error) {
		// Detached from the caller that opened the flight: values carry
		// through, cancellation does not. Tying the flight's lifetime to that
		// one caller would hand context.Canceled to everyone sharing the
		// lookup the moment it went away. The caller keeps its own
		// cancellation through the select below; what bounds the flight is
		// this timeout.
		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workloadIssuerLookupTimeout)
		defer cancel()

		// Charged before the query, so a refusal costs only the bucket read.
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

		issuerID, found, lookupErr := a.lookup(lookupCtx, endpoint, issuerURL)
		switch {
		case errors.Is(lookupErr, errWorkloadIssuerURLInvalid):
			// No row could ever describe it. Reported as untrusted with the
			// parse failure wrapped, so a caller can tell a malformed iss from
			// an unknown one without the two answering differently on the wire.
			return nil, fmt.Errorf("%w: %w", errWorkloadIssuerUntrusted, lookupErr)
		case lookupErr != nil:
			return nil, fmt.Errorf("resolve workload issuer: %w", lookupErr)
		case !found:
			return nil, errWorkloadIssuerUntrusted
		}
		return workloadIssuerResolution{issuerID: issuerID}, nil
	})

	select {
	case <-ctx.Done():
		// This caller gave up; the flight carries on for whoever else shares
		// it. Deliberately not errWorkloadIssuerUntrusted — nothing was
		// decided about this issuer.
		return uuid.Nil, fmt.Errorf("await workload issuer admission: %w", ctx.Err())
	case res := <-ch:
		if res.Err != nil {
			return uuid.Nil, res.Err
		}
		resolution, ok := res.Val.(workloadIssuerResolution)
		if !ok {
			return uuid.Nil, fmt.Errorf("resolve workload issuer: unexpected resolution %T", res.Val)
		}
		return resolution.issuerID, nil
	}
}

// workloadIssuerFlightKey identifies one in-flight lookup.
//
// Two callers share a key exactly when they would share a result: narrower and
// one caller's answer gets served to a request that would have resolved
// differently, wider only splits a flight that could have been one. So the key
// is the inputs the lookup consumes — the tenancy it resolves under, and the
// spelling it resolves.
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
// collapsing those onto one key would serve one spelling's answer to another
// that would have matched.
//
// Hashed and length-prefixed, as replay.Key is: the spelling arrives
// unauthenticated under no length bound, and a digest keeps one caller from
// holding an arbitrarily large key in the flight map.
func workloadIssuerFlightKey(endpoint *ResolvedMcpEndpoint, issuerURL string) string {
	sum := sha256.New()
	for _, part := range []string{endpoint.OrganizationID, endpoint.ProjectID.String(), issuerURL} {
		sum.Write([]byte(strconv.Itoa(len(part))))
		sum.Write([]byte(":"))
		sum.Write([]byte(part))
	}
	return base64.RawURLEncoding.EncodeToString(sum.Sum(nil))
}
