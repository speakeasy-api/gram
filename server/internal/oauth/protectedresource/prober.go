package protectedresource

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

const (
	// probeRecheck is how long a replica trusts its own last check of a
	// server's row, so proxied requests cost no database round trips.
	probeRecheck = time.Hour

	// staleAfter is how long a read of the metadata, successful or not,
	// keeps the server from being probed again on use.
	staleAfter = 24 * time.Hour

	// probeSlots caps the detached on-use probes in flight per replica; a use
	// arriving with every slot taken is dropped, a later one retries.
	probeSlots = 2

	// loginProbeSlots caps the login probes in flight per replica. Logins have
	// their own pool so background on-use probes cannot starve them.
	loginProbeSlots = 4

	// probeBudget caps one detached probe: discovery's own ten-second budget
	// plus the row read and write.
	probeBudget = 20 * time.Second

	// loginProbeBudget caps the synchronous probe a login waits on; a slower
	// resource falls back to the last good read rather than stalling the user.
	loginProbeBudget = 3 * time.Second

	// loginErrorBackoff is how long after a failed read a login stops
	// re-probing a resource that was just seen failing. On-use probes revisit
	// a row whose latest visit failed after the same interval, so a login
	// that ran out of its budget does not hide the resource from the proxy's
	// roomier probe for a day.
	loginErrorBackoff = 15 * time.Minute

	// loginLastGoodWindow bounds how old a cached advertised list may be when
	// a login cannot probe; older lists say nothing about the resource today.
	loginLastGoodWindow = 7 * 24 * time.Hour

	// recordWriteBudget bounds recording a login probe's outcome once the
	// probe's own budget is spent.
	recordWriteBudget = 5 * time.Second
)

// ProbeOutcome labels how a login resolved the resource's metadata, from
// ResolveForLogin or from the login deciding not to consult the resource.
type ProbeOutcome string

const (
	// ProbeOutcomeSkippedRecentError: the last read failed within loginErrorBackoff.
	ProbeOutcomeSkippedRecentError ProbeOutcome = "skipped_recent_error"

	// ProbeOutcomeFetched: the document was read in this login.
	ProbeOutcomeFetched ProbeOutcome = "fetched"

	// ProbeOutcomeTimeout: the probe ran out of its budget.
	ProbeOutcomeTimeout ProbeOutcome = "timeout"

	// ProbeOutcomeError: the probe failed upstream.
	ProbeOutcomeError ProbeOutcome = "error"

	// ProbeOutcomeResourceMismatch: the document named another resource (RFC 9728 §3.3).
	ProbeOutcomeResourceMismatch ProbeOutcome = "resource_mismatch"

	// ProbeOutcomeDBError: a database read the decision needed failed; the
	// login falls back to the issuer's scopes.
	ProbeOutcomeDBError ProbeOutcome = "db_error"

	// ProbeOutcomeDisabled: the organization is not enrolled, or the replica has no prober.
	ProbeOutcomeDisabled ProbeOutcome = "disabled"

	// ProbeOutcomeNotOwned: another client bound to the endpoint holds the resource.
	ProbeOutcomeNotOwned ProbeOutcome = "not_owned"

	// ProbeOutcomeSkippedClientScope: the client's own scope decides, so the
	// resource was not consulted.
	ProbeOutcomeSkippedClientScope ProbeOutcome = "skipped_client_scope"

	// ProbeOutcomeCancelled: the login's own context ended during the probe
	// (the user left); nothing is recorded on the row.
	ProbeOutcomeCancelled ProbeOutcome = "cancelled"

	// ProbeOutcomeNoSlot: every login probe slot on this replica was taken.
	ProbeOutcomeNoSlot ProbeOutcome = "no_slot"

	// ProbeOutcomeNoRow: the URL is not probed (not HTTPS) and the resource has no row.
	ProbeOutcomeNoRow ProbeOutcome = "no_row"

	// ProbeOutcomeNotApplicable: the login has no remote-backed server, or
	// an unprobed URL whose row stands as is.
	ProbeOutcomeNotApplicable ProbeOutcome = "not_applicable"
)

type check struct {
	at time.Time
}

// Prober keeps remote_protected_resources rows fresh. One instance per
// replica is shared by every caller so the per-replica debounce and the
// probe slots bound the work as a whole.
type Prober struct {
	db     *pgxpool.Pool
	policy *guardian.Policy

	// checked holds *check per project+URL so an entry is only ever removed by identity.
	checked sync.Map

	// slots bounds detached on-use probes.
	slots chan struct{}

	// loginSlots bounds synchronous login probes, apart from slots.
	loginSlots chan struct{}

	// loginBudget is how long one login probe may take; loginProbeBudget
	// outside tests.
	loginBudget time.Duration

	// now is the clock the debounce, freshness rules, and probe timing read.
	now func() time.Time

	// record persists a document a login probe read; RecordIfChanged outside tests.
	record func(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL string, doc wellknown.OAuthProtectedResourceMetadata) error

	// beforeDetached runs synchronously before detached work starts; tests only.
	beforeDetached func()

	// afterDetached runs when a detached on-use probe finishes; tests only.
	afterDetached func()
}

func NewProber(db *pgxpool.Pool, policy *guardian.Policy) *Prober {
	return &Prober{
		db:             db,
		policy:         policy,
		checked:        sync.Map{},
		slots:          make(chan struct{}, probeSlots),
		loginSlots:     make(chan struct{}, loginProbeSlots),
		loginBudget:    loginProbeBudget,
		now:            time.Now,
		record:         RecordIfChanged,
		beforeDetached: nil,
		afterDetached:  nil,
	}
}

// SetClock replaces the clock the debounce, freshness rules, and probe
// timing read; tests only.
func (p *Prober) SetClock(now func() time.Time) { p.now = now }

// SetBeforeDetached runs fn synchronously when detached work is scheduled; tests only.
func (p *Prober) SetBeforeDetached(fn func()) { p.beforeDetached = fn }

// SetAfterDetached runs fn after a detached on-use probe finishes; tests only.
func (p *Prober) SetAfterDetached(fn func()) { p.afterDetached = fn }

// sweep drops checks past their recheck window so the map stays bounded by
// the servers used recently, not ever.
func (p *Prober) sweep(now time.Time) {
	p.checked.Range(func(key, v any) bool {
		if c, ok := v.(*check); ok && now.Sub(c.at) >= probeRecheck {
			p.checked.CompareAndDelete(key, v)
		}
		return true
	})
}

// ProbeOnUse keeps the protected resource row of a server in use fresh, off
// the request path. It never fails the proxied response.
func (p *Prober) ProbeOnUse(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, organizationID string, resourceURL string) {
	// Metadata persists as-is, so it is only read over TLS.
	if !urls.IsAbsoluteHTTPSOrLoopback(resourceURL) {
		return
	}
	key := projectID.String() + " " + resourceURL
	now := p.now()
	c := &check{at: now}
	if v, loaded := p.checked.LoadOrStore(key, c); loaded {
		if prev, ok := v.(*check); ok && now.Sub(prev.at) < probeRecheck {
			return
		}
		if !p.checked.CompareAndSwap(key, v, c) {
			return
		}
	}
	select {
	case p.slots <- struct{}{}:
	default:
		p.checked.CompareAndDelete(key, c)
		return
	}

	if p.beforeDetached != nil {
		p.beforeDetached()
	}

	// Only the trace carries over: the probe outlives the request and must not inherit its values.
	detached := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	go func() {
		defer func() { <-p.slots }()
		if p.afterDetached != nil {
			defer p.afterDetached()
		}
		ctx, cancel := context.WithTimeout(detached, probeBudget)
		defer cancel()
		defer func() {
			if rec := recover(); rec != nil {
				logger.ErrorContext(ctx, "protected resource probe panicked", attr.SlogError(fmt.Errorf("%v", rec)))
			}
		}()
		current, err := p.refresh(ctx, projectID, organizationID, resourceURL, now)
		if err != nil {
			logger.ErrorContext(ctx, "refresh protected resource on use", attr.SlogError(err))
		}
		// A database fault leaves no check so the next use retries. A row
		// left failing is held off until loginErrorBackoff passes, not for
		// the full probeRecheck, and not re-read on every use either.
		switch {
		case err != nil:
			p.checked.CompareAndDelete(key, c)
		case !current:
			p.checked.CompareAndSwap(key, c, &check{at: now.Add(loginErrorBackoff - probeRecheck)})
		}
		p.sweep(p.now())
	}()
}

// refresh probes resourceURL unless its row is still current. current
// reports that the row stands as a successful read (it was fresh, or was
// just recorded from a valid document); a failed probe is recorded on the
// row and reported as not current. Only database failures are returned.
func (p *Prober) refresh(ctx context.Context, projectID uuid.UUID, organizationID string, resourceURL string, now time.Time) (current bool, err error) {
	existing, err := repo.New(p.db).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: projectID, ResourceIdentifier: resourceURL})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return false, fmt.Errorf("get remote protected resource: %w", err)
	default:
		if !refreshDue(&existing, now) {
			return !existing.MetadataLastErrorAt.Time.After(existing.MetadataFetchedAt.Time), nil
		}
	}

	doc, _, err := wellknown.DiscoverProtectedResourceMetadata(ctx, p.policy, resourceURL)
	if err != nil {
		if typed, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](err); ok {
			return false, RecordFetchError(ctx, p.db, projectID, organizationID, resourceURL, typed)
		}
		return false, nil
	}
	if err := Record(ctx, p.db, projectID, organizationID, resourceURL, doc); err != nil {
		return false, err
	}
	return doc.ValidForResource(resourceURL), nil
}

// refreshDue reports whether an on-use probe should read the row's resource
// again: a successful read stands for staleAfter, while a row whose latest
// visit is a failure is revisited after loginErrorBackoff.
func refreshDue(row *repo.RemoteProtectedResource, now time.Time) bool {
	fetched := row.MetadataFetchedAt.Time
	failed := row.MetadataLastErrorAt.Time
	if failed.After(fetched) {
		return now.Sub(failed) >= loginErrorBackoff
	}
	return now.Sub(fetched) >= staleAfter
}

// LoginResolution is what a login learns about its resource's metadata.
type LoginResolution struct {
	// Row is the stored record; nil when the resource has none.
	Row *repo.RemoteProtectedResource

	// ScopesSupported is the advertised list this login may rely on: the
	// document just read, or the last good read within loginLastGoodWindow.
	// Nil when neither exists; empty when the resource advertises none.
	ScopesSupported []string

	// Live reports that ScopesSupported came from a probe in this call.
	Live bool

	// Outcome labels how the resolution went.
	Outcome ProbeOutcome

	// ProbeDuration is how long the probe took; zero when none ran.
	ProbeDuration time.Duration
}

// ResolveForLogin reads the resource's row and, unless the row failed to
// read moments ago, probes the resource within loginProbeBudget and records
// the result when it changed. Every login probes: a resource may change what
// it advertises at any time, so a cached list is only a fallback for a failed
// probe. It never fails the login: a nil prober, a missing row, or a failed
// probe degrade to what is cached.
func (p *Prober) ResolveForLogin(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, organizationID string, resourceURL string) LoginResolution {
	none := LoginResolution{Row: nil, ScopesSupported: nil, Live: false, Outcome: ProbeOutcomeNotApplicable, ProbeDuration: 0}
	if p == nil || resourceURL == "" {
		return none
	}
	now := p.now()
	row, err := repo.New(p.db).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: projectID, ResourceIdentifier: resourceURL})
	var existing *repo.RemoteProtectedResource
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case errors.Is(err, context.Canceled):
		none.Outcome = ProbeOutcomeCancelled
		return none
	case err != nil:
		logger.ErrorContext(ctx, "get remote protected resource for login", attr.SlogError(err))
		none.Outcome = ProbeOutcomeDBError
		return none
	default:
		existing = &row
	}
	cached := LoginResolution{Row: existing, ScopesSupported: LastGoodScopes(existing, now), Live: false, Outcome: ProbeOutcomeNotApplicable, ProbeDuration: 0}

	// Metadata persists as-is, so it is only read over TLS.
	if !urls.IsAbsoluteHTTPSOrLoopback(resourceURL) {
		if existing == nil {
			cached.Outcome = ProbeOutcomeNoRow
		}
		return cached
	}
	if outcome, skip := loginSkip(existing, now); skip {
		cached.Outcome = outcome
		return cached
	}
	select {
	case p.loginSlots <- struct{}{}:
		defer func() { <-p.loginSlots }()
	default:
		cached.Outcome = ProbeOutcomeNoSlot
		return cached
	}

	probeCtx, cancel := context.WithTimeout(ctx, p.loginBudget)
	defer cancel()
	started := p.now()
	doc, _, err := wellknown.DiscoverProtectedResourceMetadata(probeCtx, p.policy, resourceURL)
	cached.ProbeDuration = p.now().Sub(started)
	// The write outlives the probe's budget, not the login's.
	writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), recordWriteBudget)
	defer cancelWrite()
	if err != nil {
		// The login's own context ended (the user left, or its deadline
		// passed): nothing was learned about the resource, so nothing is recorded.
		if ctx.Err() != nil {
			cached.Outcome = ProbeOutcomeCancelled
			return cached
		}
		cached.Outcome = ProbeOutcomeError
		if typed, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](err); ok {
			if typed.Code() == wellknown.DiscoveryCodeTimeout {
				cached.Outcome = ProbeOutcomeTimeout
			}
			if recordErr := RecordFetchError(writeCtx, p.db, projectID, organizationID, resourceURL, typed); recordErr != nil {
				logger.ErrorContext(ctx, "record protected resource fetch error", attr.SlogError(recordErr))
			}
		}
		return cached
	}
	recordErr := p.record(writeCtx, p.db, projectID, organizationID, resourceURL, doc)
	if recordErr != nil {
		logger.ErrorContext(ctx, "record protected resource", attr.SlogError(recordErr))
	}
	if !doc.ValidForResource(resourceURL) {
		cached.Outcome = ProbeOutcomeResourceMismatch
		return cached
	}
	// Once the row holds this read, the proxy's next use of this server
	// needs no row read for an hour. A write that failed leaves no check, so
	// the proxy's next use reads the row and probes again. The login's check
	// replaces any in-flight on-use check; logins probe regardless.
	if recordErr == nil {
		key := projectID.String() + " " + resourceURL
		c := &check{at: now}
		if v, loaded := p.checked.LoadOrStore(key, c); loaded {
			p.checked.CompareAndSwap(key, v, c)
		}
	}
	p.sweep(now)
	return LoginResolution{Row: existing, ScopesSupported: doc.ScopesSupported, Live: true, Outcome: ProbeOutcomeFetched, ProbeDuration: cached.ProbeDuration}
}

// loginSkip reports whether a login should trust the row as is, and why.
func loginSkip(row *repo.RemoteProtectedResource, now time.Time) (ProbeOutcome, bool) {
	if row == nil {
		return "", false
	}
	if row.MetadataLastErrorAt.Valid && now.Sub(row.MetadataLastErrorAt.Time) < loginErrorBackoff {
		return ProbeOutcomeSkippedRecentError, true
	}
	return "", false
}

// LastGoodScopes is the row's advertised list when a read captured one
// within loginLastGoodWindow; nil otherwise.
func LastGoodScopes(row *repo.RemoteProtectedResource, now time.Time) []string {
	if row == nil {
		return nil
	}
	return LastGoodAdvertisedScopes(row.ScopesSupported, row.MetadataFetchedAt, now)
}

// LastGoodAdvertisedScopes is LastGoodScopes over a row's advertised list
// and the time it was read, for rows read into another shape.
func LastGoodAdvertisedScopes(scopesSupported []string, fetchedAt pgtype.Timestamptz, now time.Time) []string {
	if scopesSupported == nil || !fetchedAt.Valid || now.Sub(fetchedAt.Time) > loginLastGoodWindow {
		return nil
	}
	return scopesSupported
}
