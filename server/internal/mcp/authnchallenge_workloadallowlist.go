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
)

// errWorkloadIssuerUntrusted reports an assertion whose iss names no issuer
// this endpoint trusts.
//
// Signature verification proves an assertion is genuine, not that it was
// meant for us: a CI provider's issuer mints valid tokens for every job on
// its platform. This is where "genuinely signed" stops being enough.
var errWorkloadIssuerUntrusted = errors.New("issuer is not trusted by this endpoint")

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
}

func newWorkloadIssuerAdmission(lookup workloadIssuerLookup) *workloadIssuerAdmission {
	return &workloadIssuerAdmission{
		lookup:   lookup,
		misses:   newWorkloadIssuerMissCache(workloadIssuerMissEntries, workloadIssuerMissTTL),
		inflight: singleflight.Group{},
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
	if a.misses.seen(key) {
		return nil, errWorkloadIssuerUntrusted
	}

	resolved, err, _ := a.inflight.Do(key, func() (any, error) {
		row, found, lookupErr := a.lookup(ctx, endpoint, issuerURL)
		switch {
		case errors.Is(lookupErr, remotesessions.ErrIssuerURLInvalid):
			// Not an issuer identifier at all, so no row could ever describe
			// it. Remembered like any other miss: a malformed iss is the
			// cheapest thing for a flood to carry.
			a.misses.remember(key)
			return nil, fmt.Errorf("%w: %w", errWorkloadIssuerUntrusted, lookupErr)
		case lookupErr != nil:
			// A database failure is not evidence about this issuer. Never
			// remembered — caching an outage would keep rejecting a
			// legitimate workload after the store recovered.
			return nil, fmt.Errorf("resolve workload issuer: %w", lookupErr)
		case !found:
			a.misses.remember(key)
			return nil, errWorkloadIssuerUntrusted
		}
		return workloadIssuerResolution{row: row}, nil
	})
	if err != nil {
		return nil, err
	}

	resolution, ok := resolved.(workloadIssuerResolution)
	if !ok {
		return nil, fmt.Errorf("resolve workload issuer: unexpected resolution %T", resolved)
	}
	return resolution.row, nil
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

// workloadIssuerMissCache remembers recently rejected issuers, bounded in both
// entries and age.
type workloadIssuerMissCache struct {
	mu sync.Mutex
	// expiries maps a key to the instant its entry lapses.
	expiries map[string]time.Time
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
		expiries:   make(map[string]time.Time),
		order:      make([]string, 0, maxEntries),
		maxEntries: maxEntries,
		ttl:        ttl,
	}
}

// seen reports whether key was rejected recently enough for the answer to
// still stand. A lapsed entry is dropped rather than reported, so an issuer
// added since is looked up again.
func (c *workloadIssuerMissCache) seen(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	expiry, ok := c.expiries[key]
	if !ok {
		return false
	}
	if !time.Now().Before(expiry) {
		c.drop(key)
		return false
	}
	return true
}

// remember records key as rejected, evicting to stay within the entry bound.
func (c *workloadIssuerMissCache) remember(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.expiries[key]; exists {
		// Already held: refreshing the expiry would let a caller keep one
		// entry alive forever by repeating it, which is the pinning the
		// insertion-ordered eviction avoids.
		return
	}

	c.evictExpiredLocked()
	for len(c.expiries) >= c.maxEntries && len(c.order) > 0 {
		c.drop(c.order[0])
	}

	c.expiries[key] = time.Now().Add(c.ttl)
	c.order = append(c.order, key)
}

// evictExpiredLocked drops every lapsed entry. Called before making room, so
// a cache full of stale entries evicts those rather than live ones.
func (c *workloadIssuerMissCache) evictExpiredLocked() {
	now := time.Now()
	live := c.order[:0]
	for _, key := range c.order {
		if expiry, ok := c.expiries[key]; ok && now.Before(expiry) {
			live = append(live, key)
			continue
		}
		delete(c.expiries, key)
	}
	c.order = live
}

// drop removes one key from both the map and the insertion order.
func (c *workloadIssuerMissCache) drop(key string) {
	delete(c.expiries, key)
	for i, held := range c.order {
		if held == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}
