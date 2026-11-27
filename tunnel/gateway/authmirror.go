package gateway

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/tunnel/route"
)

// authRouteRecheckInterval is how long the mirror trusts a tunnel's
// eligibility before asking again. Binding an OAuth issuer to a tunnel is an
// operator action; a minute until the mirror picks it up is well inside the
// bulk routes' coverage, which every reader falls back to.
const authRouteRecheckInterval = time.Minute

// AuthRouteEligibility decides which tunnels the auth route mirror carries.
type AuthRouteEligibility interface {
	// CarriesOAuth reports whether OAuth back-channel calls (token exchange,
	// refresh, revocation, registration) ride the tunnel.
	CarriesOAuth(ctx context.Context, tunnelID string) (bool, error)
}

// AuthRouteMirror configures the gateway's copy of OAuth tunnel routes on the
// auth role's Redis.
type AuthRouteMirror struct {
	// Routes is the auth role's route store (route.NewAuthMirror).
	Routes route.Store

	// Eligibility picks the tunnels to copy, which keeps the auth instance's
	// share of the routes bounded.
	Eligibility AuthRouteEligibility
}

// eligibilityDecision is the last answer for one tunnel.
type eligibilityDecision struct {
	// carriesOAuth is the answer.
	carriesOAuth bool

	// checkedAt is when it was given.
	checkedAt time.Time
}

// authMirrorResult is one tunnel's outcome in a mirror pass.
type authMirrorResult struct {
	// tunnelID is the tunnel.
	tunnelID string

	// decision is the eligibility the pass used, to cache when checked is set.
	decision eligibilityDecision

	// checked reports that decision came from a fresh lookup.
	checked bool

	// published reports that the route is now in the mirror.
	published bool

	// withdrawn reports that the route is now gone from the mirror.
	withdrawn bool
}

// authRouteMirror writes this gateway's routes for OAuth tunnels to the auth
// role. It is a second writer beside the route reconciler, with its own loop,
// so a reconciler pass slowed by the operations instance (down, full, or held
// during a move) cannot delay it. It works from the registry alone: live
// registrations publish, heartbeats refresh, withdrawals and revocations
// remove.
type authRouteMirror struct {
	routes      route.Store
	eligibility AuthRouteEligibility
	reg         *registry
	address     *atomic.Value
	logger      *slog.Logger
	interval    time.Duration
	now         func() time.Time
	lifetimeCtx context.Context
	cancel      context.CancelFunc

	// mu guards revoked, stopped and now.
	mu sync.Mutex

	// revoked holds tunnels whose mirror entry must be deleted for every
	// gateway on the next pass.
	revoked map[string]struct{}

	// stopped is set once draining starts.
	stopped bool

	// published and decisions belong to the run goroutine.
	published map[string]struct{}

	// decisions caches each tunnel's eligibility.
	decisions map[string]eligibilityDecision

	nudges chan struct{}
	drain  chan drainRequest
	done   chan struct{}
}

func newAuthRouteMirror(
	cfg AuthRouteMirror,
	reg *registry,
	address *atomic.Value,
	logger *slog.Logger,
	interval time.Duration,
) *authRouteMirror {
	lifetimeCtx, cancel := context.WithCancel(context.Background())
	m := &authRouteMirror{
		routes:      cfg.Routes,
		eligibility: cfg.Eligibility,
		reg:         reg,
		address:     address,
		logger:      logger,
		interval:    interval,
		now:         time.Now,
		lifetimeCtx: lifetimeCtx,
		cancel:      cancel,
		mu:          sync.Mutex{},
		revoked:     make(map[string]struct{}),
		stopped:     false,
		published:   make(map[string]struct{}),
		decisions:   make(map[string]eligibilityDecision),
		nudges:      make(chan struct{}, 1),
		drain:       make(chan drainRequest, 1),
		done:        make(chan struct{}),
	}
	go m.run()
	return m
}

// nudge asks for a pass soon. Registrations and withdrawals call it; forwards
// do not, since they do not change which tunnels are live. A nil mirror
// (not configured) ignores it.
func (m *authRouteMirror) nudge() {
	if m == nil {
		return
	}
	select {
	case m.nudges <- struct{}{}:
	default:
	}
}

// revoke deletes the tunnel's mirror entry for every gateway on the next pass.
// A nil mirror ignores it.
func (m *authRouteMirror) revoke(tunnelID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.revoked[tunnelID] = struct{}{}
	m.mu.Unlock()
	m.nudge()
}

func (m *authRouteMirror) requestDrain(ctx context.Context) {
	cleanupTimeout := finalCleanupTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < cleanupTimeout {
			cleanupTimeout = remaining
		}
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
	m.cancel()
	m.drain <- drainRequest{ctx: cleanupCtx, cancel: cancel, tunnelIDs: nil}
}

func (m *authRouteMirror) run() {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	defer close(m.done)

	for {
		select {
		case req := <-m.drain:
			m.finishDrain(req)
			return
		default:
		}

		select {
		case req := <-m.drain:
			m.finishDrain(req)
			return
		case <-m.nudges:
			m.pass(m.lifetimeCtx)
		case <-ticker.C:
			m.pass(m.lifetimeCtx)
		}
	}
}

func (m *authRouteMirror) takeRevoked() []string {
	m.mu.Lock()
	revoked := m.revoked
	m.revoked = make(map[string]struct{})
	m.mu.Unlock()

	tunnelIDs := make([]string, 0, len(revoked))
	for tunnelID := range revoked {
		tunnelIDs = append(tunnelIDs, tunnelID)
	}
	return tunnelIDs
}

// pass brings the mirror in line with the registry.
func (m *authRouteMirror) pass(ctx context.Context) {
	for _, tunnelID := range m.takeRevoked() {
		opCtx, cancel := routeOperationContext(ctx)
		err := m.routes.Delete(opCtx, tunnelID)
		cancel()
		if err != nil {
			m.logger.WarnContext(ctx, "tunnel auth route delete failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			m.mu.Lock()
			m.revoked[tunnelID] = struct{}{}
			m.mu.Unlock()
			continue
		}
		delete(m.published, tunnelID)
	}

	live := make(map[string]struct{})
	for _, tunnelID := range m.reg.liveTunnelIDs() {
		live[tunnelID] = struct{}{}
	}
	tunnelIDs := make([]string, 0, len(live)+len(m.published))
	for tunnelID := range live {
		tunnelIDs = append(tunnelIDs, tunnelID)
	}
	for tunnelID := range m.published {
		if _, ok := live[tunnelID]; !ok {
			tunnelIDs = append(tunnelIDs, tunnelID)
		}
	}

	m.mu.Lock()
	now := m.now()
	m.mu.Unlock()
	address := m.address.Load().(string)
	results := make([]authMirrorResult, len(tunnelIDs))
	group := errgroup.Group{}
	group.SetLimit(reconcileConcurrency)
	for i, tunnelID := range tunnelIDs {
		_, isLive := live[tunnelID]
		_, wasPublished := m.published[tunnelID]
		cached, hasCached := m.decisions[tunnelID]
		group.Go(func() error {
			results[i] = m.reconcileTunnel(ctx, tunnelID, address, now, isLive, wasPublished, cached, hasCached)
			return nil
		})
	}
	_ = group.Wait()

	for _, result := range results {
		if result.checked {
			m.decisions[result.tunnelID] = result.decision
		}
		if result.published {
			m.published[result.tunnelID] = struct{}{}
		}
		if result.withdrawn {
			delete(m.published, result.tunnelID)
		}
	}
	for tunnelID := range m.decisions {
		_, isLive := live[tunnelID]
		_, isPublished := m.published[tunnelID]
		if !isLive && !isPublished {
			delete(m.decisions, tunnelID)
		}
	}
}

func (m *authRouteMirror) reconcileTunnel(
	ctx context.Context,
	tunnelID, address string,
	now time.Time,
	isLive, wasPublished bool,
	cached eligibilityDecision,
	hasCached bool,
) authMirrorResult {
	result := authMirrorResult{tunnelID: tunnelID, decision: cached, checked: false, published: false, withdrawn: false}

	carriesOAuth := false
	if isLive {
		if hasCached && now.Sub(cached.checkedAt) < authRouteRecheckInterval {
			carriesOAuth = cached.carriesOAuth
		} else {
			opCtx, cancel := routeOperationContext(ctx)
			answer, err := m.eligibility.CarriesOAuth(opCtx, tunnelID)
			cancel()
			switch {
			case err == nil:
				carriesOAuth = answer
				result.decision = eligibilityDecision{carriesOAuth: answer, checkedAt: now}
				result.checked = true
			case hasCached:
				// Keep the last answer through a database blip; the lookup is
				// retried next pass.
				carriesOAuth = cached.carriesOAuth
				m.logger.WarnContext(ctx, "tunnel auth route eligibility check failed",
					slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			default:
				m.logger.WarnContext(ctx, "tunnel auth route eligibility check failed",
					slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			}
		}
	}

	opCtx, cancel := routeOperationContext(ctx)
	defer cancel()
	if isLive && carriesOAuth {
		if err := m.routes.Publish(opCtx, tunnelID, address, routeTTL); err != nil {
			m.logger.WarnContext(opCtx, "tunnel auth route publish failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			return result
		}
		result.published = true
		return result
	}
	if !wasPublished {
		return result
	}
	if err := m.routes.Unpublish(opCtx, tunnelID, address); err != nil {
		m.logger.WarnContext(opCtx, "tunnel auth route unpublish failed",
			slog.String("tunnel_id", tunnelID), slog.Any("error", err))
		return result
	}
	result.withdrawn = true
	return result
}

// finishDrain removes every route this gateway put in the mirror.
func (m *authRouteMirror) finishDrain(req drainRequest) {
	defer req.cancel()

	group := errgroup.Group{}
	group.SetLimit(reconcileConcurrency)
	address := m.address.Load().(string)
	for tunnelID := range m.published {
		group.Go(func() error {
			opCtx, cancel := routeOperationContext(req.ctx)
			defer cancel()
			if err := m.routes.Unpublish(opCtx, tunnelID, address); err != nil {
				m.logger.WarnContext(opCtx, "tunnel auth route unpublish failed",
					slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			}
			return nil
		})
	}
	_ = group.Wait()
}
