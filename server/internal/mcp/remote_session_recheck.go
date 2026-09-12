// Keepalive re-check: idle grants with no refresh token are presented to their upstream on a slow cadence (AIM-261).

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const (
	// DefaultRemoteSessionRecheckInterval is how long an idle grant goes between re-checks unless configured otherwise.
	DefaultRemoteSessionRecheckInterval = 24 * time.Hour

	// RemoteSessionRecheckProbeBudgetCap bounds one re-check end to end; it must stay under the server's probe drain timeout.
	RemoteSessionRecheckProbeBudgetCap = 18 * time.Second

	// RemoteSessionRecheckLeaseReleaseBudget bounds cleanup after in-flight probes drain.
	// The server drain must cover both budgets, plus bookkeeping headroom.
	RemoteSessionRecheckLeaseReleaseBudget = time.Second

	// remoteSessionRecheckTick is how often one replica looks for due grants; the jitter keeps a fleet from claiming together.
	remoteSessionRecheckTick       = 5 * time.Minute
	remoteSessionRecheckTickJitter = 2 * time.Minute

	// remoteSessionRecheckBatch bounds one claim; a pass claims again while batches come back full and the budget holds.
	remoteSessionRecheckBatch = int32(20)

	// remoteSessionRecheckSlots bounds the probes one replica runs at once.
	remoteSessionRecheckSlots = 2

	// remoteSessionRecheckPassBudget stops a pass claiming more before the next tick would.
	remoteSessionRecheckPassBudget = 3 * time.Minute

	// remoteSessionRecheckPlacementBudget is what one re-check may spend on the re-read and endpoint resolution before the probe's own timeout.
	remoteSessionRecheckPlacementBudget = 3 * time.Second
)

// remoteSessionRecheckHostRate caps probes per issuer host fleet-wide (the limiter is Redis-shared), so one provider is never swept in a burst.
// A Redis outage fails open, bounded by the two slots per replica and the DB claim lease.
var remoteSessionRecheckHostRate = ratelimit.PerMinute(10)

// recheckOutcome is what one claimed row did with its claim.
type recheckOutcome int

const (
	recheckSkipped recheckOutcome = iota
	recheckProbed
	recheckRateLimited
)

// remoteSessionRecheck runs the sweep on the server process: the probe needs endpoint
// routing and the proxy builders, and no worker-to-server credential exists to call it remotely.
type remoteSessionRecheck struct {
	// interval is the re-check cadence; zero or negative disables the sweep.
	interval time.Duration
	batch    int32
	// limiter paces probes per issuer host; nil without Redis.
	limiter      *ratelimit.Limiter
	limiterStore ratelimit.Store
	slots        chan struct{}
	// mu guards closed and every wg.Add, so a probe is never admitted after the drain has started.
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
	stop   chan struct{}
}

func newRemoteSessionRecheck(interval time.Duration, redisClient *redis.Client, meterProvider metric.MeterProvider) *remoteSessionRecheck {
	var limiter *ratelimit.Limiter
	var store ratelimit.Store
	if redisClient != nil {
		store = ratelimit.NewRedisStore(redisClient)
		limiter = ratelimit.New(store, "remote_session_recheck_host", remoteSessionRecheckHostRate, ratelimit.WithMetrics(meterProvider))
	}
	return &remoteSessionRecheck{
		interval:     interval,
		batch:        remoteSessionRecheckBatch,
		limiter:      limiter,
		limiterStore: store,
		slots:        make(chan struct{}, remoteSessionRecheckSlots),
		mu:           sync.Mutex{},
		closed:       false,
		wg:           sync.WaitGroup{},
		stop:         make(chan struct{}),
	}
}

// probeBudget is what one re-check may take end to end, capped under the drain timeout.
func (r *remoteSessionRecheck) probeBudget(validationTimeout time.Duration) time.Duration {
	return min(remoteSessionRecheckPlacementBudget+validationTimeout, RemoteSessionRecheckProbeBudgetCap)
}

// sleepUnlessStopped waits d; false when ctx ended first.
func sleepUnlessStopped(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// admit registers fn under the drain; false once shutdown has started.
func (r *remoteSessionRecheck) admit(fn func()) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.wg.Go(fn)
	return true
}

// shutdown stops the loop and waits for the pass and probes in flight, or ctx.
func (r *remoteSessionRecheck) shutdown(ctx context.Context) error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.stop)
	}
	r.mu.Unlock()
	drained := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the caller names the drain
	}
}

// StartRemoteSessionRecheck runs the keepalive re-check loop until Shutdown or
// ctx ends. The first pass waits a random fraction of a tick so replicas that
// boot together do not sweep together. A non-positive interval disables it.
func (s *Service) StartRemoteSessionRecheck(ctx context.Context) {
	r := s.remoteSessionRecheck
	logger := s.logger.With(attr.SlogComponent("remote_session_recheck"))
	if r.interval <= 0 {
		logger.InfoContext(ctx, "remote session re-check disabled by configuration")
		return
	}
	admitted := r.admit(func() {
		loopCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-r.stop:
				cancel()
			case <-loopCtx.Done():
			}
		}()
		wait := randomDuration(remoteSessionRecheckTick)
		for sleepUnlessStopped(loopCtx, wait) {
			checked, err := s.SweepRemoteSessionRechecks(loopCtx)
			if err != nil && loopCtx.Err() == nil {
				logger.ErrorContext(loopCtx, "remote session re-check pass failed", attr.SlogError(err))
			}
			if checked > 0 {
				logger.InfoContext(loopCtx, "remote session re-check pass finished", attr.SlogRemoteSessionRecheckCount(checked))
			}
			wait = remoteSessionRecheckTick - remoteSessionRecheckTickJitter + randomDuration(2*remoteSessionRecheckTickJitter)
		}
	})
	if !admitted {
		logger.InfoContext(ctx, "remote session re-check not started: shutting down")
	}
}

func randomDuration(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(limit))) // #nosec G404 -- sweep jitter is not security-sensitive
}

// SweepRemoteSessionRechecks runs one pass: claim due grants in batches and
// probe each under the slot cap, until a batch comes back short, a probe is
// rate limited, the pass budget runs out, or the loop is stopped. Returns how
// many grants were actually probed.
func (s *Service) SweepRemoteSessionRechecks(ctx context.Context) (int, error) {
	r := s.remoteSessionRecheck
	logger := s.logger.With(attr.SlogComponent("remote_session_recheck"))
	startedAt := time.Now()
	var probed, rateLimited atomic.Int32
	for {
		now := time.Now()
		rows, err := remotesessions_repo.New(s.db).ClaimDueRemoteSessionRecheckCandidates(ctx, remotesessions_repo.ClaimDueRemoteSessionRecheckCandidatesParams{
			NowTs:         conv.ToPGTimestamptz(now),
			RecheckCutoff: conv.ToPGTimestamptz(now.Add(-r.interval)),
			// A claimed row that was skipped or left inconclusive comes back well before it is due again.
			AttemptCutoff: conv.ToPGTimestamptz(now.Add(-remotesessions.RecheckLease(r.interval))),
			LimitValue:    r.batch,
		})
		if err != nil {
			return int(probed.Load()), fmt.Errorf("claim due remote session re-check candidates: %w", err)
		}
		var wg sync.WaitGroup
		for i, row := range rows {
			if !r.acquireSlot(ctx) {
				wg.Wait()
				s.releaseRemoteSessionRecheckLeases(ctx, logger, rows[i:])
				return int(probed.Load()), nil
			}
			wg.Go(func() {
				defer func() { <-r.slots }()
				switch s.recheckRemoteSession(ctx, logger, row) {
				case recheckProbed:
					probed.Add(1)
				case recheckRateLimited:
					rateLimited.Add(1)
				case recheckSkipped:
				}
			})
		}
		wg.Wait()
		// A rate-limited host means the rest of its rows would only burn their leases; leave them for the next tick.
		if len(rows) < int(r.batch) || rateLimited.Load() > 0 || time.Since(startedAt) >= remoteSessionRecheckPassBudget || ctx.Err() != nil {
			return int(probed.Load()), nil
		}
	}
}

// acquireSlot never starts a detached probe if cancellation won while waiting.
func (r *remoteSessionRecheck) acquireSlot(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case r.slots <- struct{}{}:
		if ctx.Err() != nil {
			<-r.slots
			return false
		}
		return true
	case <-ctx.Done():
		return false
	}
}

// releaseRemoteSessionRecheckLeases clears the claim on rows a cancelled pass never probed, best effort under its own short budget.
func (s *Service) releaseRemoteSessionRecheckLeases(ctx context.Context, logger *slog.Logger, rows []remotesessions_repo.ClaimDueRemoteSessionRecheckCandidatesRow) {
	detached, cancel := context.WithTimeout(trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx)), RemoteSessionRecheckLeaseReleaseBudget)
	defer cancel()
	for _, row := range rows {
		if _, err := remotesessions_repo.New(s.db).ClearRemoteSessionRecheckLease(detached, remotesessions_repo.ClearRemoteSessionRecheckLeaseParams{
			ID:             row.ID,
			OrganizationID: row.OrganizationID,
		}); err != nil {
			logger.WarnContext(detached, "release unprobed remote session re-check lease", attr.SlogRemoteSessionID(row.ID.String()), attr.SlogError(err))
			return
		}
	}
}

// recheckRemoteSession re-reads one claimed grant, places it on an endpoint it
// still serves, and runs the probe. Every refusal is logged and leaves the
// row for the next attempt window; nothing here writes a rejection.
func (s *Service) recheckRemoteSession(ctx context.Context, logger *slog.Logger, row remotesessions_repo.ClaimDueRemoteSessionRecheckCandidatesRow) (outcome recheckOutcome) {
	logger = logger.With(attr.SlogRemoteSessionID(row.ID.String()), attr.SlogOrganizationID(row.OrganizationID), attr.SlogOAuthIssuer(row.IssuerUrl))
	detached := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	ctx, cancel := context.WithTimeout(detached, s.remoteSessionRecheck.probeBudget(s.metaRuntime.ValidationTimeout))
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome = recheckSkipped
			logger.ErrorContext(ctx, "remote session re-check panicked", attr.SlogError(fmt.Errorf("%v", recovered)))
		}
	}()

	now := time.Now()
	candidate, err := remotesessions_repo.New(s.db).GetDueRemoteSessionRecheckCandidate(ctx, remotesessions_repo.GetDueRemoteSessionRecheckCandidateParams{
		ID:             row.ID,
		NowTs:          conv.ToPGTimestamptz(now),
		RecheckCutoff:  conv.ToPGTimestamptz(now.Add(-s.remoteSessionRecheck.interval)),
		OrganizationID: row.OrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.InfoContext(ctx, "remote session re-check skipped: no longer due")
		return recheckSkipped
	case err != nil:
		logger.ErrorContext(ctx, "reload remote session for re-check", attr.SlogError(err))
		return recheckSkipped
	}
	sess := candidate.RemoteSession
	logger = logger.With(attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()))

	placements, err := remotesessions_repo.New(s.db).GetRemoteSessionRecheckEndpoints(ctx, remotesessions_repo.GetRemoteSessionRecheckEndpointsParams{
		UserSessionIssuerID:   sess.UserSessionIssuerID,
		RemoteSessionClientID: sess.RemoteSessionClientID,
		OrganizationID:        row.OrganizationID,
		SubjectUrn:            sess.SubjectUrn,
		NowTs:                 conv.ToPGTimestamptz(now),
	})
	if err != nil {
		logger.ErrorContext(ctx, "find endpoint for remote session re-check", attr.SlogError(err))
		return recheckSkipped
	}
	for _, placement := range placements {
		ref, endpoint := s.placeRemoteSessionRecheck(ctx, logger, row.OrganizationID, placement)
		if endpoint == nil {
			logger.InfoContext(ctx, "remote session re-check skipped: no endpoint serves this grant")
			continue
		}
		clients, err := s.remoteChallengeMgr.ListClients(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
		if err != nil {
			logger.ErrorContext(ctx, "list clients for remote session re-check", attr.SlogError(err))
			continue
		}
		client := findConsentClient(clients, sess.RemoteSessionClientID)
		if client == nil {
			logger.InfoContext(ctx, "remote session re-check skipped: client is not bound to the endpoint")
			continue
		}
		subject := sess.SubjectUrn
		// A synthetic first-party state: the probe reads only the subject, the id it keys its session on, and the endpoint.
		state := AuthnChallengeState{
			ID:                       "keepalive:" + sess.ID.String(),
			FlowID:                   "",
			UserSessionIssuerID:      endpoint.UserSessionIssuerID,
			AuthorizerUserID:         "",
			AuthorizerImpersonated:   nil,
			AgentAuthorizationTarget: nil,
			Endpoint:                 ref,
			ClientID:                 "",
			RedirectURI:              "",
			State:                    "",
			CodeChallenge:            "",
			CodeChallengeMethod:      "",
			CSRFToken:                "",
			Subject:                  &subject,
			CreatedAt:                now,
			FirstParty:               true,
			AutoConnectDone:          false,
		}
		// The probe logs its own refusals; this only says the verdict did not land.
		err = s.probeRemoteSession(ctx, logger, endpoint, state, *client, nil, remotesessionmetrics.ValidationTriggerKeepalive)
		switch {
		case errors.Is(err, errRemoteSessionUnroutable), errors.Is(err, errRemoteSessionMemberOffline):
			// Nothing was dialled or written; the claim lease paces the retry so a long-offline tunnel is not re-read every tick.
			logger.InfoContext(ctx, "remote session re-check endpoint cannot route this grant", attr.SlogError(err))
			continue
		case errors.Is(err, errRemoteSessionRecheckRateLimited):
			// Stay inside this probe's budget; a detached cleanup would extend the shutdown drain.
			if _, err := remotesessions_repo.New(s.db).ClearRemoteSessionRecheckLease(ctx, remotesessions_repo.ClearRemoteSessionRecheckLeaseParams{
				ID:             row.ID,
				OrganizationID: row.OrganizationID,
			}); err != nil {
				logger.ErrorContext(ctx, "release remote session re-check lease", attr.SlogError(err))
			}
			return recheckRateLimited
		case err != nil:
			logger.InfoContext(ctx, "remote session re-check did not record a verdict", attr.SlogError(err))
		}
		return recheckProbed
	}
	return recheckSkipped
}

// placeRemoteSessionRecheck resolves one endpoint the runtime still serves, trying the private surface when the public one is policy-refused.
func (s *Service) placeRemoteSessionRecheck(ctx context.Context, logger *slog.Logger, organizationID string, placement remotesessions_repo.GetRemoteSessionRecheckEndpointsRow) (EndpointRef, *ResolvedMcpEndpoint) {
	// The runtime resolver re-applies visibility, network access and the issuer gate from the ref, as the callback does.
	var publicAuthority networkingress.Authority
	ref := EndpointRef{
		CustomDomainID:  placement.CustomDomainID,
		Authority:       publicAuthority,
		BaseURL:         "",
		McpServerID:     placement.McpServerID,
		MetaMcpServerID: placement.MetaMcpServerID,
		IsPublic:        nil,
		ToolsetID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpSlug:         placement.Slug,
		RouteBase:       "mcp",
	}
	endpoint, err := s.loadResolvedMcpEndpointByRef(ctx, ref)
	if errors.Is(err, mcpendpoints.ErrPolicyDenied) {
		// A private-only endpoint refuses the public surface; present it through the organization's own.
		ref.Authority = networkingress.Authority{
			Surface:          requestorigin.SurfacePrivateNetwork,
			BaseURL:          "",
			OrganizationID:   organizationID,
			NetworkIngressID: uuid.Nil,
			NamespaceKind:    "",
			CustomDomainID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		}
		endpoint, err = s.loadResolvedMcpEndpointByRef(ctx, ref)
	}
	if err != nil {
		logger.InfoContext(ctx, "remote session re-check endpoint not resolved", attr.SlogError(err))
		return EndpointRef{}, nil //nolint:exhaustruct // no placement
	}
	return ref, endpoint
}

// errRemoteSessionRecheckRateLimited is a pre-probe refusal: the caller releases the lease.
var errRemoteSessionRecheckRateLimited = errors.New("remote session re-check issuer host rate limited")

func (s *Service) admitRemoteSessionRecheck(ctx context.Context, logger *slog.Logger, issuerURL string) error {
	if s.remoteSessionRecheck.limiter == nil {
		return nil
	}
	res, err := s.remoteSessionRecheck.limiter.Allow(ctx, issuerHost(issuerURL))
	if err != nil {
		logger.WarnContext(ctx, "remote session re-check limiter unavailable; allowing", attr.SlogError(err))
		return nil
	}
	if !res.Allowed {
		return errRemoteSessionRecheckRateLimited
	}
	return nil
}

// issuerHost keys the per-host limiter; an unparseable issuer shares one bucket.
func issuerHost(issuerURL string) string {
	parsed, err := url.Parse(issuerURL)
	if err != nil || parsed.Host == "" {
		return "unknown"
	}
	return strings.ToLower(parsed.Host)
}
