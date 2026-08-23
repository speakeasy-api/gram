package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
)

// reconcileInterval is how often the supervisor re-reads the ingress set.
// A Redis pub/sub nudge from gram-server can shorten reaction time later;
// MVP polls only.
const reconcileInterval = 15 * time.Second

// IngressSource is the configuration and health persistence the supervisor
// needs; implemented by PostgresStore.
type IngressSource interface {
	ListEnabledIngresses(ctx context.Context) ([]IngressConfig, error)
	SetStatus(ctx context.Context, ingressID uuid.UUID, status string, ns NodeStatus) error
	NullAuthKey(ctx context.Context, ingressID uuid.UUID) error
	NodeState(ingressID uuid.UUID) StateStore
}

// LeaseManager is per-ingress ownership; implemented by RedisLease.
type LeaseManager interface {
	Claim(ctx context.Context, ingressID uuid.UUID) (bool, error)
	Heartbeat(ctx context.Context, ingressID uuid.UUID) (bool, error)
	Release(ctx context.Context, ingressID uuid.UUID) error
}

// SupervisorConfig carries the supervisor's dependencies and limits.
type SupervisorConfig struct {
	Source IngressSource
	Lease  LeaseManager

	// Providers maps provider kind to implementation, e.g. "tailscale".
	Providers map[string]Provider

	// Upstream is gram-server's in-cluster URL.
	Upstream *url.URL

	// ForwardToken authenticates proxied requests to gram-server.
	ForwardToken string

	// MaxNodes caps concurrently running nodes on this replica.
	MaxNodes int

	Logger *slog.Logger
}

// Supervisor reconciles running overlay nodes against enabled ingress rows:
// claiming leases, starting and stopping nodes, and persisting health.
type Supervisor struct {
	cfg SupervisorConfig

	mu      sync.Mutex
	running map[uuid.UUID]*runningNode
}

type runningNode struct {
	cfg    IngressConfig
	node   Node
	server *http.Server

	// started closes once the node's Start attempt finished (ok or not).
	started chan struct{}
	// startErr is set before started closes.
	startErr error

	stop func()
}

func NewSupervisor(cfg SupervisorConfig) *Supervisor {
	return &Supervisor{cfg: cfg, mu: sync.Mutex{}, running: map[uuid.UUID]*runningNode{}}
}

// Run reconciles until ctx is done, then stops every owned node.
func (s *Supervisor) Run(ctx context.Context) {
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	s.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *Supervisor) reconcile(ctx context.Context) {
	rows, err := s.cfg.Source.ListEnabledIngresses(ctx)
	if err != nil {
		s.cfg.Logger.ErrorContext(ctx, "network ingress reconcile list failed", slog.Any("error", err))
		return
	}

	desired := make(map[uuid.UUID]IngressConfig, len(rows))
	for _, row := range rows {
		desired[row.ID] = row
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop nodes whose rows are gone, disabled, or changed.
	for id, rn := range s.running {
		row, ok := desired[id]
		if ok && row.UpdatedAt.Equal(rn.cfg.UpdatedAt) {
			continue
		}
		s.stopLocked(ctx, id, rn)
		if !ok {
			// Row deleted or disabled: best-effort status write; no-op when
			// the row is fully gone.
			if err := s.cfg.Source.SetStatus(ctx, id, "disabled", NodeStatus{Online: false, NetworkName: "", DNSName: "", NodeID: "", Err: ""}); err != nil {
				s.cfg.Logger.WarnContext(ctx, "network ingress status write failed", slog.String("ingress_id", id.String()), slog.Any("error", err))
			}
		}
	}

	// Heartbeat owned leases; a lost lease stops the node so another replica
	// can adopt it from the shared state store.
	for id, rn := range s.running {
		ok, err := s.cfg.Lease.Heartbeat(ctx, id)
		if err != nil {
			s.cfg.Logger.WarnContext(ctx, "network ingress lease heartbeat failed", slog.String("ingress_id", id.String()), slog.Any("error", err))
			continue
		}
		if !ok {
			s.cfg.Logger.WarnContext(ctx, "network ingress lease lost; stopping node", slog.String("ingress_id", id.String()))
			s.stopLocked(ctx, id, rn)
		}
	}

	// Start missing nodes, up to the replica cap.
	for id, row := range desired {
		if _, ok := s.running[id]; ok {
			continue
		}
		if len(s.running) >= s.cfg.MaxNodes {
			s.cfg.Logger.WarnContext(ctx, "network ingress node cap reached; skipping", slog.String("ingress_id", id.String()))
			continue
		}
		claimed, err := s.cfg.Lease.Claim(ctx, id)
		if err != nil {
			s.cfg.Logger.ErrorContext(ctx, "network ingress lease claim failed", slog.String("ingress_id", id.String()), slog.Any("error", err))
			continue
		}
		if !claimed {
			continue
		}
		s.startLocked(row)
	}
}

// startLocked launches one node asynchronously; the supervisor lock only
// guards the running map, never a network join.
func (s *Supervisor) startLocked(cfg IngressConfig) {
	logger := s.cfg.Logger.With(slog.String("ingress_id", cfg.ID.String()), slog.String("provider", cfg.Provider))

	nodeCtx, cancel := context.WithCancel(context.Background())
	rn := &runningNode{
		cfg:      cfg,
		node:     nil,
		server:   nil,
		started:  make(chan struct{}),
		startErr: nil,
		stop:     cancel,
	}
	s.running[cfg.ID] = rn

	provider, ok := s.cfg.Providers[cfg.Provider]
	if !ok {
		rn.startErr = errors.New("unknown provider: " + cfg.Provider)
		close(rn.started)
		_ = s.cfg.Source.SetStatus(nodeCtx, cfg.ID, "error", NodeStatus{Online: false, NetworkName: "", DNSName: "", NodeID: "", Err: rn.startErr.Error()})
		return
	}

	go func() {
		if err := s.cfg.Source.SetStatus(nodeCtx, cfg.ID, "connecting", NodeStatus{Online: false, NetworkName: "", DNSName: "", NodeID: "", Err: ""}); err != nil {
			logger.WarnContext(nodeCtx, "network ingress status write failed", slog.Any("error", err))
		}

		node, err := provider.NewNode(nodeCtx, cfg, s.cfg.Source.NodeState(cfg.ID))
		if err == nil {
			rn.node = node
			err = node.Start(nodeCtx)
		}
		if err != nil {
			rn.startErr = err
			close(rn.started)
			logger.ErrorContext(nodeCtx, "network ingress node start failed", slog.Any("error", err))
			_ = s.cfg.Source.SetStatus(nodeCtx, cfg.ID, "error", NodeStatus{Online: false, NetworkName: "", DNSName: "", NodeID: "", Err: err.Error()})
			s.remove(cfg.ID, rn)
			return
		}

		ln, err := node.Listener(nodeCtx)
		if err != nil {
			rn.startErr = err
			close(rn.started)
			logger.ErrorContext(nodeCtx, "network ingress listener failed", slog.Any("error", err))
			_ = s.cfg.Source.SetStatus(nodeCtx, cfg.ID, "error", NodeStatus{Online: false, NetworkName: "", DNSName: "", NodeID: "", Err: err.Error()})
			_ = node.Close(nodeCtx)
			s.remove(cfg.ID, rn)
			return
		}

		status := node.Status(nodeCtx)
		if err := s.cfg.Source.SetStatus(nodeCtx, cfg.ID, "online", status); err != nil {
			logger.WarnContext(nodeCtx, "network ingress status write failed", slog.Any("error", err))
		}

		// A one-shot join key is spent once the device identity is durable.
		if cfg.Credential.Kind == CredentialKindAuthKey && cfg.Credential.AuthKey != "" {
			if err := s.cfg.Source.NullAuthKey(nodeCtx, cfg.ID); err != nil {
				logger.WarnContext(nodeCtx, "network ingress auth key cleanup failed", slog.Any("error", err))
			}
		}

		srv := &http.Server{
			Handler:           NewProxyHandler(cfg, node, s.cfg.Upstream, s.cfg.ForwardToken, logger),
			ReadHeaderTimeout: 15 * time.Second,
		}
		rn.server = srv
		logger.InfoContext(nodeCtx, "network ingress node online", slog.String("dns_name", status.DNSName))

		// Startup is complete: stopLocked may now observe rn.node and
		// rn.server. Serve blocks until Shutdown, so this close must not
		// wait for goroutine exit.
		close(rn.started)

		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(nodeCtx, "network ingress node server stopped", slog.Any("error", err))
			_ = s.cfg.Source.SetStatus(nodeCtx, cfg.ID, "error", NodeStatus{Online: false, NetworkName: "", DNSName: "", NodeID: "", Err: err.Error()})
			s.remove(cfg.ID, rn)
		}
	}()
}

// remove drops a failed node from the running map and releases its lease.
func (s *Supervisor) remove(id uuid.UUID, rn *runningNode) {
	s.mu.Lock()
	if s.running[id] == rn {
		delete(s.running, id)
	}
	s.mu.Unlock()
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.cfg.Lease.Release(releaseCtx, id); err != nil {
		s.cfg.Logger.WarnContext(releaseCtx, "network ingress lease release failed", slog.String("ingress_id", id.String()), slog.Any("error", err))
	}
}

// stopLocked shuts one node down and releases its lease. Callers hold s.mu.
func (s *Supervisor) stopLocked(ctx context.Context, id uuid.UUID, rn *runningNode) {
	delete(s.running, id)
	rn.stop()
	<-rn.started
	if rn.server != nil {
		shutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = rn.server.Shutdown(shutCtx)
		cancel()
	}
	if rn.node != nil {
		closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := rn.node.Close(closeCtx); err != nil {
			s.cfg.Logger.WarnContext(ctx, "network ingress node close failed", slog.String("ingress_id", id.String()), slog.Any("error", err))
		}
		cancel()
	}
	if err := s.cfg.Lease.Release(ctx, id); err != nil {
		s.cfg.Logger.WarnContext(ctx, "network ingress lease release failed", slog.String("ingress_id", id.String()), slog.Any("error", err))
	}
}

func (s *Supervisor) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, rn := range s.running {
		s.stopLocked(ctx, id, rn)
		if err := s.cfg.Source.SetStatus(ctx, id, "offline", NodeStatus{Online: false, NetworkName: "", DNSName: "", NodeID: "", Err: ""}); err != nil {
			s.cfg.Logger.WarnContext(ctx, "network ingress status write failed", slog.String("ingress_id", id.String()), slog.Any("error", err))
		}
	}
}
