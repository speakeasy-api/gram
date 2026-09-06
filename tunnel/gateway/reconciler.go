package gateway

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/speakeasy-api/gram/tunnel/route"
)

const (
	reconcileConcurrency = 16
	finalCleanupTimeout  = 20 * time.Second
)

type drainRequest struct {
	ctx       context.Context
	cancel    context.CancelFunc
	tunnelIDs []string
}

type revokeRequest struct {
	ctx      context.Context
	tunnelID string
	done     chan struct{}
}

type revokeState struct {
	globalDeleteComplete bool
}

type reconcileJob struct {
	tunnelID        string
	suppressPublish bool
}

type reconcileResult struct {
	tunnelID    string
	owned       bool
	cleanupRan  bool
	cleaned     bool
	reactivated bool
}

// routeReconciler is the sole writer of this gateway's route and connection
// projection. Passes never overlap; bounded workers shorten each pass while
// preserving ordering between nudges, revokes, refreshes, and final cleanup.
type routeReconciler struct {
	lifetimeCtx     context.Context
	cancel          context.CancelFunc
	reg             *registry
	keys            KeyResolver
	routes          route.Store
	logger          *slog.Logger
	refreshInterval time.Duration
	address         atomic.Value

	dirtyMu sync.Mutex
	dirty   map[string]struct{}
	stopped bool

	nudges chan struct{}
	revoke chan revokeRequest
	drain  chan drainRequest
	done   chan struct{}
}

func newRouteReconciler(
	reg *registry,
	keys KeyResolver,
	routes route.Store,
	address string,
	logger *slog.Logger,
	refreshInterval time.Duration,
) *routeReconciler {
	if refreshInterval <= 0 {
		refreshInterval = routeTTL / 2
	}
	lifetimeCtx, cancel := context.WithCancel(context.Background())
	r := &routeReconciler{
		lifetimeCtx:     lifetimeCtx,
		cancel:          cancel,
		reg:             reg,
		keys:            keys,
		routes:          routes,
		logger:          logger,
		refreshInterval: refreshInterval,
		address:         atomic.Value{},
		dirtyMu:         sync.Mutex{},
		dirty:           make(map[string]struct{}),
		stopped:         false,
		nudges:          make(chan struct{}, 1),
		revoke:          make(chan revokeRequest),
		drain:           make(chan drainRequest, 1),
		done:            make(chan struct{}),
	}
	r.address.Store(address)
	go r.run()
	return r
}

func (r *routeReconciler) nudge(tunnelID string) {
	r.dirtyMu.Lock()
	if r.stopped {
		r.dirtyMu.Unlock()
		return
	}
	r.dirty[tunnelID] = struct{}{}
	r.dirtyMu.Unlock()

	select {
	case r.nudges <- struct{}{}:
	default:
	}
}

func (r *routeReconciler) requestRevoke(ctx context.Context, tunnelID string) {
	done := make(chan struct{})
	request := revokeRequest{ctx: ctx, tunnelID: tunnelID, done: done}
	select {
	case r.revoke <- request:
	case <-r.done:
		return
	}
	select {
	case <-done:
	case <-r.done:
	case <-ctx.Done():
	}
}

func (r *routeReconciler) requestDrain(ctx context.Context, tunnelIDs []string) {
	cleanupTimeout := finalCleanupTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < cleanupTimeout {
			cleanupTimeout = remaining
		}
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	r.dirtyMu.Lock()
	r.stopped = true
	r.dirtyMu.Unlock()
	r.cancel()
	r.drain <- drainRequest{ctx: cleanupCtx, cancel: cancel, tunnelIDs: tunnelIDs}
}

func (r *routeReconciler) takeDirty() []string {
	r.dirtyMu.Lock()
	dirty := r.dirty
	r.dirty = make(map[string]struct{})
	r.dirtyMu.Unlock()

	tunnelIDs := make([]string, 0, len(dirty))
	for tunnelID := range dirty {
		tunnelIDs = append(tunnelIDs, tunnelID)
	}
	return tunnelIDs
}

func (r *routeReconciler) run() {
	ticker := time.NewTicker(r.refreshInterval)
	defer ticker.Stop()
	defer close(r.done)

	everOwned := make(map[string]struct{})
	revokes := make(map[string]revokeState)
	for {
		select {
		case req := <-r.drain:
			r.finishDrain(req, everOwned)
			return
		default:
		}

		select {
		case req := <-r.drain:
			r.finishDrain(req, everOwned)
			return
		case <-r.nudges:
			r.reconcile(r.lifetimeCtx, r.takeDirty(), false, everOwned, revokes)
		case req := <-r.revoke:
			state := revokeState{globalDeleteComplete: false}
			if r.deleteTunnel(req.ctx, req.tunnelID) {
				state.globalDeleteComplete = true
				delete(everOwned, req.tunnelID)
			}
			revokes[req.tunnelID] = state
			close(req.done)
		case <-ticker.C:
			for tunnelID, state := range revokes {
				if state.globalDeleteComplete {
					continue
				}
				if r.deleteTunnel(r.lifetimeCtx, tunnelID) {
					state.globalDeleteComplete = true
					revokes[tunnelID] = state
					delete(everOwned, tunnelID)
				}
			}

			tunnelIDs := r.reg.liveTunnelIDs()
			live := make(map[string]struct{}, len(tunnelIDs))
			for _, tunnelID := range tunnelIDs {
				live[tunnelID] = struct{}{}
			}
			for tunnelID := range everOwned {
				if _, ok := live[tunnelID]; !ok {
					tunnelIDs = append(tunnelIDs, tunnelID)
				}
			}
			r.reconcile(r.lifetimeCtx, tunnelIDs, true, everOwned, revokes)
			for tunnelID, state := range revokes {
				if state.globalDeleteComplete && len(r.reg.sessionKeys(tunnelID)) == 0 {
					delete(revokes, tunnelID)
				}
			}
		}
	}
}

func (r *routeReconciler) reconcile(
	ctx context.Context,
	tunnelIDs []string,
	validateActive bool,
	everOwned map[string]struct{},
	revokes map[string]revokeState,
) {
	jobs := make(chan reconcileJob, len(tunnelIDs))
	results := make(chan reconcileResult, len(tunnelIDs))
	for _, tunnelID := range tunnelIDs {
		_, suppressPublish := revokes[tunnelID]
		jobs <- reconcileJob{tunnelID: tunnelID, suppressPublish: suppressPublish}
	}
	close(jobs)

	for range min(reconcileConcurrency, len(tunnelIDs)) {
		go func() {
			for job := range jobs {
				results <- r.reconcileTunnel(ctx, job.tunnelID, validateActive, job.suppressPublish)
			}
		}()
	}

	for range tunnelIDs {
		result := <-results
		if result.owned {
			everOwned[result.tunnelID] = struct{}{}
		}
		if result.cleanupRan && result.cleaned {
			delete(everOwned, result.tunnelID)
		}
		if result.reactivated {
			delete(revokes, result.tunnelID)
		}
	}
}

func (r *routeReconciler) reconcileTunnel(
	ctx context.Context,
	tunnelID string,
	validateActive bool,
	suppressPublish bool,
) reconcileResult {
	result := reconcileResult{tunnelID: tunnelID, owned: false, cleanupRan: false, cleaned: false, reactivated: false}
	if validateActive {
		if checker, ok := r.keys.(ActiveTunnelChecker); ok {
			sessionKeys := r.reg.sessionKeys(tunnelID)
			if len(sessionKeys) == 0 {
				result.cleanupRan = true
				result.cleaned = r.cleanupTunnel(ctx, tunnelID)
				return result
			}

			activeByHash := make(map[string]bool, len(sessionKeys))
			for _, sessionKey := range sessionKeys {
				if _, checked := activeByHash[sessionKey.keyHash]; checked {
					continue
				}
				opCtx, cancel := routeOperationContext(ctx)
				active, err := checker.IsActive(opCtx, tunnelID, sessionKey.keyHash)
				if err != nil {
					if ctx.Err() == nil {
						r.logger.WarnContext(opCtx, "tunnel active check failed",
							slog.String("tunnel_id", tunnelID), slog.Any("error", err))
					}
					cancel()
					return result
				}
				cancel()
				activeByHash[sessionKey.keyHash] = active
			}
			activeSession := false
			for _, sessionKey := range sessionKeys {
				if activeByHash[sessionKey.keyHash] {
					activeSession = true
				} else {
					r.reg.killSession(tunnelID, sessionKey.id)
				}
			}
			if suppressPublish && activeSession {
				suppressPublish = false
				result.reactivated = true
			}
		}
	}
	if suppressPublish {
		return result
	}

	heartbeatAt := time.Now().UTC()
	connections := r.reg.connections(tunnelID, heartbeatAt)
	if len(connections) == 0 {
		result.cleanupRan = true
		result.cleaned = r.cleanupTunnel(ctx, tunnelID)
		return result
	}

	result.owned = true
	address := r.address.Load().(string)
	opCtx, cancel := routeOperationContext(ctx)
	if err := r.routes.Publish(opCtx, tunnelID, address, routeTTL); err != nil {
		r.logger.WarnContext(opCtx, "tunnel route publish failed",
			slog.String("tunnel_id", tunnelID), slog.Any("error", err))
	}
	if store, ok := r.routes.(route.ConnectionSnapshotStore); ok {
		if err := store.PublishConnections(opCtx, tunnelID, address, connections, routeTTL); err != nil {
			r.logger.WarnContext(opCtx, "tunnel connection snapshot publish failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", err))
		}
	}
	cancel()
	return result
}

func (r *routeReconciler) cleanupTunnel(ctx context.Context, tunnelID string) bool {
	// Disconnect cleanup shares one budget to bound reconciler worker lifetime.
	cleaned := true
	address := r.address.Load().(string)
	opCtx, cancel := routeOperationContext(ctx)
	defer cancel()

	if err := r.routes.Unpublish(opCtx, tunnelID, address); err != nil {
		cleaned = false
		r.logger.WarnContext(opCtx, "tunnel route unpublish failed",
			slog.String("tunnel_id", tunnelID), slog.Any("error", err))
	}
	if store, ok := r.routes.(route.ConnectionSnapshotStore); ok {
		if err := store.DeleteConnectionOwner(opCtx, tunnelID, address); err != nil {
			cleaned = false
			r.logger.WarnContext(opCtx, "tunnel connection snapshot delete failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", err))
		}
	}
	return cleaned
}

func (r *routeReconciler) cleanupTunnelDuringDrain(ctx context.Context, tunnelID string) bool {
	// Drain cleanup gives each mutation a budget to maximize cleanup completeness.
	cleaned := true
	address := r.address.Load().(string)
	opCtx, cancel := routeOperationContext(ctx)
	if err := r.routes.Unpublish(opCtx, tunnelID, address); err != nil {
		cleaned = false
		r.logger.WarnContext(opCtx, "tunnel route unpublish failed",
			slog.String("tunnel_id", tunnelID), slog.Any("error", err))
	}
	cancel()
	if store, ok := r.routes.(route.ConnectionSnapshotStore); ok {
		opCtx, cancel = routeOperationContext(ctx)
		if err := store.DeleteConnectionOwner(opCtx, tunnelID, address); err != nil {
			cleaned = false
			r.logger.WarnContext(opCtx, "tunnel connection snapshot delete failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", err))
		}
		cancel()
	}
	return cleaned
}

func (r *routeReconciler) deleteTunnel(ctx context.Context, tunnelID string) bool {
	deleted := true
	opCtx, cancel := routeOperationContext(ctx)
	if err := r.routes.Delete(opCtx, tunnelID); err != nil {
		deleted = false
		r.logger.WarnContext(opCtx, "tunnel route delete failed",
			slog.String("tunnel_id", tunnelID), slog.Any("error", err))
	}
	cancel()
	if store, ok := r.routes.(route.ConnectionSnapshotStore); ok {
		opCtx, cancel = routeOperationContext(ctx)
		if err := store.DeleteConnections(opCtx, tunnelID); err != nil {
			deleted = false
			r.logger.WarnContext(opCtx, "tunnel connection snapshots delete failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", err))
		}
		cancel()
	}
	return deleted
}

func (r *routeReconciler) finishDrain(req drainRequest, everOwned map[string]struct{}) {
	defer req.cancel()
	for _, tunnelID := range req.tunnelIDs {
		everOwned[tunnelID] = struct{}{}
	}
	tunnelIDs := make([]string, 0, len(everOwned))
	for tunnelID := range everOwned {
		tunnelIDs = append(tunnelIDs, tunnelID)
	}

	jobs := make(chan string, len(tunnelIDs))
	completed := make(chan struct{}, len(tunnelIDs))
	for _, tunnelID := range tunnelIDs {
		jobs <- tunnelID
	}
	close(jobs)
	for range min(reconcileConcurrency, len(tunnelIDs)) {
		go func() {
			for tunnelID := range jobs {
				r.cleanupTunnelDuringDrain(req.ctx, tunnelID)
				completed <- struct{}{}
			}
		}()
	}
	for range tunnelIDs {
		<-completed
	}
}
