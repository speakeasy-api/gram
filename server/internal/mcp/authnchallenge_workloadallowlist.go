// Admission for the workload assertion grant: deciding whether an assertion's
// iss is an issuer this endpoint trusts, before anything is fetched. Sits in
// front of workloadIssuerKeySource in authnchallenge_workloadauth.go, which
// turns the row this produces into a key source.

package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

const (
	// workloadIssuerMissTTL is how long a miss is remembered.
	//
	// Deliberately short. This cache exists to absorb a burst of identical
	// rejections, not to be a durable record of what is untrusted, and the
	// cost of a long entry is paid by the operator: an issuer added through
	// the management API stays rejected until the entry lapses, on whichever
	// replicas already hold one. Seconds collapse a flood just as well as
	// minutes would, and keep a configuration change close to immediate.
	workloadIssuerMissTTL = 30 * time.Second

	// workloadIssuerMissEntries bounds how many misses are remembered per
	// replica.
	//
	// The bound is the point, not a detail. Entries are keyed partly by a
	// value that arrives in an unauthenticated request, so an unbounded cache
	// would be a memory amplifier reachable by anyone: vary the iss each time
	// and every rejection costs a permanent allocation. The cap makes the
	// worst case a fixed, small ceiling, and turns a flood of distinct
	// issuers into eviction churn instead of growth. Sized to cover the
	// realistic working set — an organization trusts tens of issuers, not
	// thousands — with headroom.
	workloadIssuerMissEntries = 4096

	// workloadIssuerLookupTimeout bounds one admission lookup, including the
	// wait for a slot below.
	//
	// The lookup runs detached from the caller's context, so nothing else
	// bounds it. Generous for a single indexed read, and deliberately far
	// short of the pool's 60s statement timeout, which is a backstop against
	// a runaway query rather than a delay any caller should wait out.
	workloadIssuerLookupTimeout = 5 * time.Second

	// workloadIssuerLookupSlots bounds how many admission lookups hold a
	// database connection at once.
	//
	// The detached flight is what makes this necessary. singleflight collapses
	// concurrent resolutions of one issuer and does nothing at all for distinct
	// ones, so a flood of *different* spellings — free to produce, since this
	// grant is reachable without credentials — puts one query per spelling in
	// the pool, and detachment holds each there for the timeout above whether
	// or not its caller is still waiting. Unbounded, the cheapest possible
	// request is a pool exhaustion that starves every other caller of the
	// database.
	//
	// Small on purpose. pool_max_conns is not configured anywhere, so the pool
	// is pgx's default of max(4, NumCPU) — single digits on a typical container
	// — and a bound above that would not be a bound. These are single indexed
	// reads, so four concurrent slots still clear thousands per second; the
	// ceiling is here to keep admission from monopolizing the pool, not to
	// ration a scarce resource. Revisit if the pool is ever sized explicitly.
	workloadIssuerLookupSlots = 4
)

// errWorkloadIssuerUntrusted reports an assertion whose iss names no issuer
// this endpoint trusts.
//
// Signature verification proves an assertion is genuine, not that it was
// meant for us: a CI provider's issuer mints valid tokens for every job on
// its platform. This is where "genuinely signed" stops being enough.
var errWorkloadIssuerUntrusted = errors.New("issuer is not trusted by this endpoint")

// workloadIssuerMissReason records why an issuer was rejected, so a repeat
// served from the cache answers with the same taxonomy the original did.
// Without it a caller mapping a malformed iss to 400 and an unknown one to
// 401 would return a different status for the same input depending on whether
// a cache entry happened to be live.
//
// The reason is stored, but the parse error's detail is not: that text is
// derived from an unauthenticated, unbounded value, and keeping it would put
// an attacker-sized string back into an entry the cap is supposed to bound.
// The sentinel survives; the prose does not.
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

// workloadIssuerLookup resolves an assertion's iss to the trusted issuer row
// an endpoint admits it under, reporting false when no tier-visible row
// describes it. Injected so admission can be tested without a database, and
// so the miss path can be shown to consult nothing further.
type workloadIssuerLookup func(ctx context.Context, endpoint *ResolvedMcpEndpoint, issuerURL string) (*remotesessions_repo.RemoteSessionIssuer, bool, error)

// newWorkloadIssuerLookup binds the shared resolver to a database handle.
// Tenancy and tier precedence live in remotesessions.ResolveIssuerByURL, which
// the management API resolves through as well, so admission cannot drift from
// what an operator sees when they look an issuer up.
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

// workloadIssuerAdmission answers whether an endpoint trusts an assertion's
// issuer, remembering recent misses so a repeated unknown issuer costs
// nothing beyond a map read.
//
// Safe for concurrent use; build one at wiring time.
type workloadIssuerAdmission struct {
	lookup workloadIssuerLookup
	misses *workloadIssuerMissCache

	// inflight collapses concurrent resolutions of one key onto a single
	// lookup. The miss cache alone only bounds the *sustained* cost of a
	// repeated unknown issuer: a burst arriving together all passes the
	// cache read before any of them records a miss, so without this each
	// one still costs a query.
	inflight singleflight.Group

	// slots bounds how many of those lookups touch the database at once.
	// singleflight handles repeats of one issuer; this handles distinct ones,
	// which it cannot. See workloadIssuerLookupSlots.
	slots *semaphore.Weighted
}

func newWorkloadIssuerAdmission(lookup workloadIssuerLookup) *workloadIssuerAdmission {
	return &workloadIssuerAdmission{
		lookup:   lookup,
		misses:   newWorkloadIssuerMissCache(workloadIssuerMissEntries, workloadIssuerMissTTL),
		inflight: singleflight.Group{},
		slots:    semaphore.NewWeighted(workloadIssuerLookupSlots),
	}
}

// workloadIssuerResolution is what one admitted lookup produced, carried
// through singleflight so every sharer of a call sees the same row.
type workloadIssuerResolution struct {
	row *remotesessions_repo.RemoteSessionIssuer
}

// admit resolves issuerURL to the trusted issuer row it names, or reports
// errWorkloadIssuerUntrusted.
//
// A rejection here costs no outbound request, and a repeated rejection costs
// no query either. Both matter because this grant is reachable without
// credentials by design: if an unrecognised iss reached discovery, anyone
// could aim Gram's egress at a host of their choosing and spend its fetch
// budget doing it, and if every rejection reached the database, the cheapest
// possible request would still cost a query.
func (a *workloadIssuerAdmission) admit(ctx context.Context, endpoint *ResolvedMcpEndpoint, issuerURL string) (*remotesessions_repo.RemoteSessionIssuer, error) {
	key := workloadIssuerMissKey(endpoint, issuerURL)
	if reason, ok := a.misses.seen(key); ok {
		return nil, reason.err()
	}

	ch := a.inflight.DoChan(key, func() (any, error) {
		// Re-check under the flight. A caller that read the cache before a
		// flight recorded its miss arrives here after that flight has ended,
		// and without this would start a redundant lookup — the same
		// double-check botFrameworkAuthenticator.remoteKeySet carries.
		if reason, ok := a.misses.seen(key); ok {
			return nil, reason.err()
		}

		// Detached from the caller that opened the flight: values carry
		// through, cancellation does not. The flight runs under whichever
		// caller happened to open it, so tying its lifetime to that one would
		// hand context.Canceled to everyone sharing the lookup the moment that
		// caller went away — on a grant reachable without credentials, an
		// abandoned request failing a legitimate one resolving the same issuer.
		//
		// Detachment costs the caller nothing, because cancellation is not
		// taken away from it: the select at the end of admit returns on the
		// caller's own context while the flight carries on for the rest. What
		// bounds the flight is the timeout here and the slot below.
		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workloadIssuerLookupTimeout)
		defer cancel()

		if err := a.slots.Acquire(lookupCtx, 1); err != nil {
			// Never remembered. A queue too long to clear inside the timeout
			// is a statement about load, not about this issuer, and caching it
			// would keep rejecting a legitimate workload after the pressure
			// passed — the same reason a database failure is not remembered
			// below.
			return nil, fmt.Errorf("acquire workload issuer lookup slot: %w", err)
		}
		defer a.slots.Release(1)

		row, found, lookupErr := a.lookup(lookupCtx, endpoint, issuerURL)
		switch {
		case errors.Is(lookupErr, remotesessions.ErrIssuerURLInvalid):
			// Not an issuer identifier at all, so no row could ever describe
			// it. Remembered like any other miss: a malformed iss is the
			// cheapest thing for a flood to carry.
			a.misses.remember(key, workloadIssuerMissMalformed)
			return nil, fmt.Errorf("%w: %w", errWorkloadIssuerUntrusted, lookupErr)
		case lookupErr != nil:
			// A database failure is not evidence about this issuer. Never
			// remembered — caching an outage would keep rejecting a
			// legitimate workload after the store recovered.
			return nil, fmt.Errorf("resolve workload issuer: %w", lookupErr)
		case !found:
			a.misses.remember(key, workloadIssuerMissUnknown)
			return nil, errWorkloadIssuerUntrusted
		}
		return workloadIssuerResolution{row: row}, nil
	})

	select {
	case <-ctx.Done():
		// This caller gave up; the flight it may have opened carries on for
		// whoever else is sharing it, and still records its miss — abandoning a
		// request must not cost the next one a query. Deliberately not
		// errWorkloadIssuerUntrusted: nothing was decided about this issuer,
		// and a caller mapping untrusted to a 401 must not report one here.
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
// The rule this has to satisfy: two calls share a key exactly when they would
// share a lookup result. Narrower and a rejection gets served to a request
// that would have resolved; wider only fragments the cache. So the key is
// precisely the inputs the lookup consumes — the tenancy it resolves under,
// and the spelling it resolves.
//
// Tenancy is the organization and project, NOT the user session issuer. An
// mcp_servers row references its issuer through a single-column foreign key
// with no project pinning (unlike meta_mcp_servers, which is composite), so
// one user session issuer can back endpoints in different projects. Keying on
// the issuer alone would let a miss recorded under one project's tenancy deny
// a project-tier trusted issuer in another.
//
// Keyed on the supplied spelling rather than a canonical form, which is the
// non-obvious half. Lookup matches a closed set of spellings that includes the
// caller's own, so two inputs sharing a canonical form do not necessarily
// share a result: a row stored as https://IDP.example.com is found by a
// request spelling it that way and missed by one spelling it in lowercase.
// Collapsing those onto one key would let the missing spelling's rejection be
// served to the spelling that would have matched.
//
// Hashed, and length-prefixed for the same reason replay.Key is: the issuer
// spelling arrives in an unauthenticated request under no length bound, so
// storing it verbatim would let a flood of long distinct URLs occupy far more
// than the entry count suggests. A digest makes every entry the same size, so
// the entry cap is a true memory bound rather than a count of unbounded
// strings.
func workloadIssuerMissKey(endpoint *ResolvedMcpEndpoint, issuerURL string) string {
	sum := sha256.New()
	for _, part := range []string{endpoint.OrganizationID, endpoint.ProjectID.String(), issuerURL} {
		sum.Write([]byte(strconv.Itoa(len(part))))
		sum.Write([]byte(":"))
		sum.Write([]byte(part))
	}
	return base64.RawURLEncoding.EncodeToString(sum.Sum(nil))
}

// workloadIssuerMissEntry is one remembered rejection: when it lapses, and
// what it was. Both fields are fixed size, so an entry costs the same whatever
// issuer produced it.
type workloadIssuerMissEntry struct {
	expiry time.Time
	reason workloadIssuerMissReason
}

// workloadIssuerMissCache remembers recently rejected issuers, bounded in both
// entries and age.
type workloadIssuerMissCache struct {
	mu sync.Mutex
	// entries maps a key to the rejection it holds.
	entries map[string]workloadIssuerMissEntry
	// order records insertion order so a full cache can evict its oldest
	// entry. Insertion order rather than recency: an entry is worth keeping
	// for as long as the flood that created it lasts, and refreshing on every
	// hit would let a caller pin one indefinitely.
	order      []string
	maxEntries int
	ttl        time.Duration
}

func newWorkloadIssuerMissCache(maxEntries int, ttl time.Duration) *workloadIssuerMissCache {
	return &workloadIssuerMissCache{
		mu:         sync.Mutex{},
		entries:    make(map[string]workloadIssuerMissEntry),
		order:      make([]string, 0, maxEntries),
		maxEntries: maxEntries,
		ttl:        ttl,
	}
}

// seen reports the rejection held for key, if one was recorded recently
// enough for the answer to still stand. A lapsed entry is dropped rather than
// reported, so an issuer added since is looked up again.
func (c *workloadIssuerMissCache) seen(key string) (workloadIssuerMissReason, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return workloadIssuerMissUnknown, false
	}
	if !time.Now().Before(entry.expiry) {
		c.drop(key)
		return workloadIssuerMissUnknown, false
	}
	return entry.reason, true
}

// remember records key as rejected for reason, evicting to stay within the
// entry bound.
func (c *workloadIssuerMissCache) remember(key string, reason workloadIssuerMissReason) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, exists := c.entries[key]; exists && time.Now().Before(existing.expiry) {
		// Held and still live: refreshing the expiry would let a caller keep
		// one entry alive forever by repeating it, which is the pinning the
		// insertion-ordered eviction avoids. A lapsed entry is not held in
		// this sense — it is a fresh miss that happens to collide with a
		// corpse, and falls through to be recorded as one.
		return
	}

	c.evictExpiredLocked()
	for len(c.entries) >= c.maxEntries && len(c.order) > 0 {
		c.drop(c.order[0])
	}

	c.entries[key] = workloadIssuerMissEntry{expiry: time.Now().Add(c.ttl), reason: reason}
	c.order = append(c.order, key)
}

// evictExpiredLocked drops every lapsed entry. Called before making room, so
// a cache full of stale entries evicts those rather than live ones.
func (c *workloadIssuerMissCache) evictExpiredLocked() {
	now := time.Now()
	live := c.order[:0]
	for _, key := range c.order {
		if entry, ok := c.entries[key]; ok && now.Before(entry.expiry) {
			live = append(live, key)
			continue
		}
		delete(c.entries, key)
	}
	c.order = live
}

// drop removes one key from both the map and the insertion order.
func (c *workloadIssuerMissCache) drop(key string) {
	delete(c.entries, key)
	for i, held := range c.order {
		if held == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}
