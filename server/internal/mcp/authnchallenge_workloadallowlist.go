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

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	workloadidentity_repo "github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

const (
	// workloadIssuerLookupTimeout bounds one admission lookup. The lookup runs
	// detached from the caller's context, so nothing else bounds it.
	workloadIssuerLookupTimeout = 5 * time.Second
)

// workloadIssuerLookupRate bounds how many lookups one endpoint drives into
// the database, and is the only bound: singleflight collapses repeats of one
// spelling, but a flood of distinct spellings shares no flight.
//
// Per endpoint rather than per replica, so one tenant cannot exhaust another's
// budget. The bucket lives in Redis, so it is fleet-wide.
var workloadIssuerLookupRate = ratelimit.PerMinute(120).WithBurst(30)

// errWorkloadIssuerUntrusted reports an iss resolving to no workload issuer
// row in the endpoint's tenancy — its own project, or the organization above.
//
// Weaker than "trusted for this workload": this stage establishes only that the
// tenant registered the issuer. A CI provider signs every job on its platform,
// so the subject admission that follows is the security boundary, not this.
var errWorkloadIssuerUntrusted = errors.New("no workload issuer in this tenancy describes that issuer url")

// errWorkloadIssuerLookupRateLimited reports a lookup refused for budget.
// Nothing was decided about the issuer, so a caller mapping untrusted onto a
// 401 must not answer one here.
var errWorkloadIssuerLookupRateLimited = errors.New("workload issuer lookups are rate limited for this endpoint")

// errWorkloadIssuerLimiterUnavailable reports that the limiter's store could
// not answer. Fails closed, and stays distinct from a refusal so an outage is
// not reported as a rate limit an operator can wait out.
var errWorkloadIssuerLimiterUnavailable = errors.New("workload issuer lookup limiter unavailable")

// errWorkloadIssuerURLInvalid marks a value that is not an issuer identifier
// at all. Wrapped by a lookup before it consults anything, so admission can
// tell "could never name a row" from "names no row we hold".
var errWorkloadIssuerURLInvalid = errors.New("invalid issuer url")

// newWorkloadIssuerLookupBudget builds the per-endpoint ceiling, or nil when
// there is no store. Nil is not "unlimited" — an absent budget refuses, so a
// deployment without the store does not get the grant.
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

// workloadIssuerBudget charges one lookup against a scope's ceiling. A
// function rather than *ratelimit.Limiter so refusal and outage are testable
// without the store; production passes (*ratelimit.Limiter).Allow.
//
// !Allowed is a decision the budget made; an error is it failing to make one.
type workloadIssuerBudget func(ctx context.Context, scope string) (ratelimit.Result, error)

// workloadIssuerLookup resolves an iss to the workload issuer row the
// endpoint's tenant registered for it, false when none does. The endpoint is
// the input because it names the tenancy the resolution runs against.
//
// The whole row rather than its id, because workloadIssuerKeySource reads
// jwks_uri off it. A value that is not an issuer identifier is reported as an
// error wrapping errWorkloadIssuerURLInvalid, before the store is consulted.
type workloadIssuerLookup func(ctx context.Context, endpoint *ResolvedMcpEndpoint, issuerURL string) (workloadidentity_repo.WorkloadIssuer, bool, error)

// workloadIssuerAdmission resolves an assertion's issuer to the row that
// describes it.
//
// Safe for concurrent use; build one at wiring time.
type workloadIssuerAdmission struct {
	lookup workloadIssuerLookup

	// inflight collapses concurrent resolutions of one key onto a single
	// lookup. In-process is all it needs to be: it bounds a simultaneous
	// burst, sustained load is the limiter's job.
	inflight singleflight.Group

	// charge bounds lookups reaching the database. Nil means no ceiling was
	// wired, which admission treats as unprotected rather than unlimited.
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
// to: the authorization server's identifier, so no endpoint spends another's.
//
// A denial-of-service bound, deliberately not the tenancy the lookup resolves
// against, so a flood at one of a tenant's servers cannot starve the rest.
//
// Never the issuer URL, which two organizations may share, and never anything
// derived from the request, which would let a caller mint a fresh budget by
// varying what it sends.
func workloadIssuerLookupScope(endpoint *ResolvedMcpEndpoint) string {
	return "workload-issuer-lookup:" + endpoint.UserSessionIssuerID.String()
}

// workloadIssuerResolution carries a lookup's result through singleflight, so
// every sharer of a call sees the same row.
type workloadIssuerResolution struct {
	issuer workloadidentity_repo.WorkloadIssuer
}

// admit resolves issuerURL to the issuer row it names, or reports
// errWorkloadIssuerUntrusted.
//
// Nothing here fetches, so an unrecognised iss cannot become an outbound
// request. A rejection costs one indexed SELECT, bounded anyway because this
// grant is reachable without credentials.
func (a *workloadIssuerAdmission) admit(ctx context.Context, endpoint *ResolvedMcpEndpoint, issuerURL string) (workloadidentity_repo.WorkloadIssuer, error) {
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

		issuer, found, lookupErr := a.lookup(lookupCtx, endpoint, issuerURL)
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
		return workloadIssuerResolution{issuer: issuer}, nil
	})

	select {
	case <-ctx.Done():
		// This caller gave up; the flight carries on for whoever else shares
		// it. Deliberately not errWorkloadIssuerUntrusted — nothing was
		// decided about this issuer.
		return workloadidentity_repo.WorkloadIssuer{}, fmt.Errorf("await workload issuer admission: %w", ctx.Err())
	case res := <-ch:
		if res.Err != nil {
			return workloadidentity_repo.WorkloadIssuer{}, res.Err
		}
		resolution, ok := res.Val.(workloadIssuerResolution)
		if !ok {
			return workloadidentity_repo.WorkloadIssuer{}, fmt.Errorf("resolve workload issuer: unexpected resolution %T", res.Val)
		}
		return resolution.issuer, nil
	}
}

// workloadIssuerFlightKey identifies one in-flight lookup.
//
// Two callers share a key exactly when they would share a result, so the key is
// what the lookup consumes: the tenancy it resolves under and the spelling it
// resolves. Tenancy is the organization and project, not the user session
// issuer — one issuer can back endpoints in different projects.
//
// Keyed on the supplied spelling rather than a canonical form: lookup matches a
// closed set of spellings, so a row stored as https://IDP.example.com is not
// found by a request spelling it in lowercase.
//
// Hashed and length-prefixed, as replay.Key is, because the spelling arrives
// unauthenticated under no length bound.
func workloadIssuerFlightKey(endpoint *ResolvedMcpEndpoint, issuerURL string) string {
	sum := sha256.New()
	for _, part := range []string{endpoint.OrganizationID, endpoint.ProjectID.String(), issuerURL} {
		sum.Write([]byte(strconv.Itoa(len(part))))
		sum.Write([]byte(":"))
		sum.Write([]byte(part))
	}
	return base64.RawURLEncoding.EncodeToString(sum.Sum(nil))
}
