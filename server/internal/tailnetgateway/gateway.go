package tailnetgateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/netingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
)

const (
	// DefaultPollInterval is how often the gateway reconciles desired ingresses
	// and heartbeats its hosts. Customers see enable/disable within one poll.
	DefaultPollInterval = 15 * time.Second

	// DefaultDrainTimeout bounds how long in-flight requests may finish during
	// shutdown. It must fit inside the pod's termination grace period.
	DefaultDrainTimeout = 20 * time.Second

	// DefaultStartTimeout bounds node registration and connection. First
	// registration includes a certificate request, which can take ~30s.
	DefaultStartTimeout = 60 * time.Second

	// DefaultMaxHosts caps nodes per pod so memory stays bounded; each tsnet
	// node costs tens of MiB.
	DefaultMaxHosts = 200

	// DefaultShard is the multi-tenant gateway every ingress starts on.
	DefaultShard = "shared"

	// maxStartFailures is how many consecutive failed starts a host tolerates
	// before it backs off for backoffAfterFailures instead of every poll.
	maxStartFailures = 3

	// backoffAfterFailures spaces retries once a host keeps failing, so a
	// broken tailnet does not burn a registration attempt every poll.
	backoffAfterFailures = 5 * time.Minute

	// maxErrorLength bounds the error text persisted in heartbeats.
	maxErrorLength = 240
)

// Config is the gateway pod's runtime configuration.
type Config struct {
	// Ordinal is the StatefulSet pod index and the slot this pod hosts.
	Ordinal int32

	// PodName is recorded in heartbeats for diagnostics.
	PodName string

	// Shard selects which ingresses this gateway serves.
	Shard string

	// StateDir is the scratch directory for tsnet; a per-node subdirectory is
	// created under it.
	StateDir string

	// Tag is the ACL tag every node advertises.
	Tag string

	// Upstream is the private listener the gateway proxies to.
	Upstream *url.URL

	// UpstreamTransport reaches Upstream with its pinned CA.
	UpstreamTransport http.RoundTripper

	// TokenPath is the projected service-account token used as attestation.
	TokenPath string

	// PollInterval is the reconcile cadence.
	PollInterval time.Duration

	// DrainTimeout bounds in-flight requests during shutdown.
	DrainTimeout time.Duration

	// StartTimeout bounds one node start.
	StartTimeout time.Duration

	// MaxHosts caps nodes hosted by this pod.
	MaxHosts int

	// ReconcileTaskQueue names the reconciler queue woken on status changes;
	// empty disables the wake-up.
	ReconcileTaskQueue string

	// ControlURL overrides the Tailscale coordination server.
	ControlURL string
}

// Validate rejects an incomplete configuration.
func (c Config) Validate() error {
	if c.Ordinal < 0 {
		return errors.New("tailnet gateway ordinal must not be negative")
	}
	if c.Shard == "" {
		return errors.New("tailnet gateway shard is required")
	}
	if c.StateDir == "" {
		return errors.New("tailnet gateway state directory is required")
	}
	if err := ValidateTag(c.Tag); err != nil {
		return err
	}
	if c.Upstream == nil || c.Upstream.Scheme != "https" || c.Upstream.Host == "" {
		return errors.New("tailnet gateway upstream must be an https url")
	}
	if c.Upstream.User != nil || c.Upstream.Path != "" || c.Upstream.RawPath != "" || c.Upstream.RawQuery != "" || c.Upstream.ForceQuery || c.Upstream.Fragment != "" {
		return errors.New("tailnet gateway upstream must not contain userinfo, path, query, or fragment")
	}
	if c.UpstreamTransport == nil {
		return errors.New("tailnet gateway upstream transport is required")
	}
	if c.TokenPath == "" {
		return errors.New("tailnet gateway token path is required")
	}
	if c.PollInterval <= 0 || c.DrainTimeout <= 0 || c.StartTimeout <= 0 || c.MaxHosts <= 0 {
		return errors.New("tailnet gateway intervals and host cap must be positive")
	}
	return nil
}

type desiredIngress struct {
	id               uuid.UUID
	organizationID   string
	organizationSlug string
	label            string
	updatedAt        time.Time
}

type host struct {
	// mu guards every field below; heartbeats, stops, slug refreshes and
	// Hosts run from different goroutines.
	mu        sync.Mutex
	ingress   desiredIngress
	node      Node
	status    NodeStatus
	lastError string
	nodeID    string
	dnsName   string
	// authKeyCleared is set once the one-shot key has been removed from the
	// slot row after the node persisted its state.
	authKeyCleared bool
}

type startAttempt struct {
	failures int
	nextAt   time.Time
}

// Gateway hosts every desired ingress for one pod ordinal.
type Gateway struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	enc       *encryption.Client
	cfg       Config
	runtime   NodeRuntime
	metrics   *Metrics
	telemetry *netingress.Telemetry
	requester *networkingress.OutboxRequester
	now       func() time.Time

	mu       sync.Mutex
	hosts    map[uuid.UUID]*host
	starting map[uuid.UUID]struct{}
	attempts map[uuid.UUID]*startAttempt
	starts   sync.WaitGroup

	ready    atomic.Bool
	lastPoll atomic.Int64
}

// New builds a gateway; call Run to start it.
func New(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, enc *encryption.Client, cfg Config, runtime NodeRuntime) (*Gateway, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if db == nil || enc == nil || runtime == nil {
		return nil, errors.New("tailnet gateway requires a database, encryption client and node runtime")
	}
	return &Gateway{
		logger:    logger.With(attr.SlogComponent("tailnet_gateway"), attr.SlogTailnetGatewaySlot(cfg.Ordinal)),
		db:        db,
		enc:       enc,
		cfg:       cfg,
		runtime:   runtime,
		metrics:   NewMetrics(logger, meterProvider),
		telemetry: netingress.NewTelemetry(logger, meterProvider),
		requester: networkingress.NewOutboxRequester(cfg.ReconcileTaskQueue),
		now:       time.Now,
		mu:        sync.Mutex{},
		hosts:     make(map[uuid.UUID]*host),
		starting:  make(map[uuid.UUID]struct{}),
		attempts:  make(map[uuid.UUID]*startAttempt),
		starts:    sync.WaitGroup{},
		ready:     atomic.Bool{},
		lastPoll:  atomic.Int64{},
	}, nil
}

// Ready reports whether the first reconcile pass completed.
func (g *Gateway) Ready() bool {
	return g.ready.Load()
}

// Healthy fails when the poll loop has stalled for three intervals.
func (g *Gateway) Healthy() error {
	last := g.lastPoll.Load()
	if last == 0 {
		return nil
	}
	if g.now().Sub(time.Unix(0, last)) > 3*g.cfg.PollInterval {
		return errors.New("tailnet gateway poll loop is stalled")
	}
	return nil
}

// Run polls until ctx is cancelled, then drains every host.
func (g *Gateway) Run(ctx context.Context) error {
	g.Poll(ctx)
	g.ready.Store(true)

	ticker := time.NewTicker(g.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			g.ready.Store(false)
			g.drain(context.WithoutCancel(ctx))
			return nil
		case <-ticker.C:
			g.Poll(ctx)
		}
	}
}

// Poll runs one reconcile pass: start hosts for newly desired ingresses, stop
// hosts for ingresses that went away, and heartbeat every running host.
func (g *Gateway) Poll(ctx context.Context) {
	started := g.now()
	defer func() {
		g.lastPoll.Store(g.now().UnixNano())
		g.metrics.Poll(ctx, g.now().Sub(started))
	}()

	rows, err := repo.New(g.db).ListDesiredGatewayIngressesForSlot(ctx, repo.ListDesiredGatewayIngressesForSlotParams{Slot: g.cfg.Ordinal, GatewayShard: g.cfg.Shard})
	if err != nil {
		g.logger.ErrorContext(ctx, "list desired tailnet ingresses", attr.SlogError(err))
		g.heartbeatAll(ctx)
		return
	}
	desired := make(map[uuid.UUID]desiredIngress, len(rows))
	for _, row := range rows {
		desired[row.ID] = desiredIngress{
			id: row.ID, organizationID: row.OrganizationID, organizationSlug: row.OrganizationSlug,
			label: row.Hostname, updatedAt: row.UpdatedAt.Time,
		}
	}

	g.mu.Lock()
	var toStop []*host
	for id, h := range g.hosts {
		want, ok := desired[id]
		h.mu.Lock()
		if !ok || want.label != h.ingress.label {
			toStop = append(toStop, h)
			delete(g.hosts, id)
		} else if want.organizationSlug != h.ingress.organizationSlug {
			// Metrics stay keyed by the organization's current slug.
			h.ingress.organizationSlug = want.organizationSlug
		}
		h.mu.Unlock()
	}
	var toStart []desiredIngress
	for id, want := range desired {
		if _, hosted := g.hosts[id]; hosted {
			continue
		}
		if _, inFlight := g.starting[id]; inFlight {
			continue
		}
		if attempt, ok := g.attempts[id]; ok && g.now().Before(attempt.nextAt) {
			continue
		}
		if len(g.hosts)+len(g.starting)+len(toStart) >= g.cfg.MaxHosts {
			g.logger.WarnContext(ctx, "tailnet gateway host cap reached", attr.SlogNetworkIngressID(id.String()))
			continue
		}
		toStart = append(toStart, want)
		g.starting[id] = struct{}{}
	}
	g.mu.Unlock()

	for _, h := range toStop {
		g.stopHost(ctx, h, NodeStatusStopped)
	}
	for _, want := range toStart {
		g.starts.Add(1)
		go func(want desiredIngress) {
			defer g.starts.Done()
			g.startHost(ctx, want)
		}(want)
	}
	g.heartbeatAll(ctx)
}

func (g *Gateway) heartbeatAll(ctx context.Context) {
	g.mu.Lock()
	hosts := make([]*host, 0, len(g.hosts))
	for _, h := range g.hosts {
		hosts = append(hosts, h)
	}
	g.mu.Unlock()
	for _, h := range hosts {
		g.heartbeat(ctx, h)
	}
}

// startHost brings one node up and registers it as a host. It runs outside
// the gateway lock because registration can take tens of seconds.
func (g *Gateway) startHost(ctx context.Context, want desiredIngress) {
	logger := g.logger.With(attr.SlogNetworkIngressID(want.id.String()), attr.SlogOrganizationSlug(want.organizationSlug))
	defer func() {
		g.mu.Lock()
		delete(g.starting, want.id)
		g.mu.Unlock()
	}()

	queries := repo.New(g.db)
	slotRow, err := queries.GetNetworkIngressNodeSlot(ctx, repo.GetNetworkIngressNodeSlotParams{
		NetworkIngressID: want.id, OrganizationID: want.organizationID, Slot: g.cfg.Ordinal,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The slot was retired or removed between the poll and now.
		return
	}
	if err != nil {
		logger.ErrorContext(ctx, "load tailnet node slot", attr.SlogError(err))
		return
	}
	if slotRow.RetiredAt.Valid {
		return
	}

	store, err := NewPostgresStateStore(g.db, g.enc, want.id, want.organizationID, g.cfg.Ordinal)
	if err != nil {
		logger.ErrorContext(ctx, "build tailnet state store", attr.SlogError(err))
		return
	}
	registered, err := store.Registered(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "check tailnet node registration", attr.SlogError(err))
		return
	}
	authKey := ""
	if !registered {
		if !slotRow.AuthKeyEncrypted.Valid || slotRow.AuthKeyEncrypted.String == "" {
			g.writeHeartbeat(ctx, want, NodeStatusPending, "", "", "awaiting auth key")
			return
		}
		if slotRow.AuthKeyExpiresAt.Valid && !slotRow.AuthKeyExpiresAt.Time.After(g.now()) {
			g.writeHeartbeat(ctx, want, NodeStatusPending, "", "", "auth key expired")
			return
		}
		authKey, err = g.enc.Decrypt(slotRow.AuthKeyEncrypted.String)
		if err != nil {
			logger.ErrorContext(ctx, "decrypt tailnet auth key", attr.SlogError(err))
			g.recordStartFailure(ctx, want, "auth key unreadable")
			return
		}
	}
	// A first registration that fails may have consumed the one-shot key and
	// left partial state behind. Clear both so the reconciler mints a fresh
	// key and the next attempt starts clean.
	failRegistration := func(message string) {
		if !registered {
			if err := store.Reset(ctx); err != nil {
				logger.ErrorContext(ctx, "reset partial tailnet node state", attr.SlogError(err))
			}
			if err := repo.New(g.db).ClearNetworkIngressNodeAuthKey(ctx, repo.ClearNetworkIngressNodeAuthKeyParams{
				NetworkIngressID: want.id, OrganizationID: want.organizationID, Slot: g.cfg.Ordinal,
			}); err != nil {
				logger.ErrorContext(ctx, "clear tailnet auth key after failed registration", attr.SlogError(err))
			}
		}
		g.recordStartFailure(ctx, want, message)
		if !registered {
			g.wakeReconciler(ctx, want)
		}
	}

	g.writeHeartbeat(ctx, want, NodeStatusStarting, "", "", "")
	startedAt := g.now()
	node, err := g.runtime.Start(ctx, NodeSpec{
		IngressID: want.id, OrganizationID: want.organizationID, Slot: g.cfg.Ordinal,
		Hostname: NodeHostname(g.cfg.Ordinal), ServiceName: ServiceName(want.label), Tag: g.cfg.Tag,
		AuthKey: authKey, Store: store, StateDir: filepath.Join(g.cfg.StateDir, want.id.String()),
		ControlURL: g.cfg.ControlURL, StartTimeout: g.cfg.StartTimeout,
	})
	if err != nil {
		logger.ErrorContext(ctx, "start tailnet node", attr.SlogError(err))
		failRegistration(err.Error())
		return
	}
	state, err := node.Status(ctx)
	if err != nil {
		_ = node.Close(ctx)
		failRegistration(err.Error())
		return
	}
	handler, err := g.handler(want, node, state.FQDN)
	if err != nil {
		_ = node.Close(ctx)
		g.recordStartFailure(ctx, want, err.Error())
		return
	}
	if err := node.Serve(handler); err != nil {
		_ = node.Close(ctx)
		g.recordStartFailure(ctx, want, err.Error())
		return
	}
	g.metrics.Lifecycle(ctx, want.organizationSlug, OperationStart, ResultOK)
	g.metrics.StartDuration(ctx, want.organizationSlug, !registered, g.now().Sub(startedAt))

	h := &host{
		mu: sync.Mutex{}, ingress: want, node: node, status: NodeStatusStarting, lastError: "", nodeID: "", dnsName: state.FQDN,
		// The key row is the source of truth: a restart after a failed clear
		// still removes it once the node is online.
		authKeyCleared: !slotRow.AuthKeyEncrypted.Valid,
	}
	g.mu.Lock()
	delete(g.attempts, want.id)
	if _, exists := g.hosts[want.id]; exists {
		g.mu.Unlock()
		_ = node.Close(ctx)
		return
	}
	g.hosts[want.id] = h
	g.mu.Unlock()
	g.metrics.HostStatusChanged(ctx, want.organizationSlug, g.cfg.Ordinal, "", NodeStatusStarting)
	logger.InfoContext(ctx, "tailnet node started", attr.SlogHostName(state.FQDN))
	g.heartbeat(ctx, h)
}

func (g *Gateway) recordStartFailure(ctx context.Context, want desiredIngress, message string) {
	g.metrics.Lifecycle(ctx, want.organizationSlug, OperationStart, ResultError)
	g.mu.Lock()
	attempt, ok := g.attempts[want.id]
	if !ok {
		attempt = &startAttempt{failures: 0, nextAt: time.Time{}}
		g.attempts[want.id] = attempt
	}
	attempt.failures++
	if attempt.failures >= maxStartFailures {
		attempt.nextAt = g.now().Add(backoffAfterFailures)
	}
	g.mu.Unlock()
	g.writeHeartbeat(ctx, want, NodeStatusError, "", "", message)
}

// heartbeat refreshes one host's row from its live node state and wakes the
// reconciler when the node crosses the online boundary.
func (g *Gateway) heartbeat(ctx context.Context, h *host) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, err := h.node.Status(ctx)
	next := NodeStatusStarting
	message := ""
	switch {
	case err != nil:
		next = NodeStatusError
		message = err.Error()
	case state.Online:
		next = NodeStatusOnline
	default:
		message = state.BackendState
	}
	previous := h.status
	h.status = next
	h.lastError = message
	if state.NodeID != "" {
		h.nodeID = state.NodeID
	}
	if state.FQDN != "" {
		h.dnsName = state.FQDN
	}
	g.writeHeartbeat(ctx, h.ingress, next, h.nodeID, h.dnsName, message)
	if previous != next {
		g.metrics.HostStatusChanged(ctx, h.ingress.organizationSlug, g.cfg.Ordinal, previous, next)
	}
	if next == NodeStatusOnline && !h.authKeyCleared {
		if err := repo.New(g.db).ClearNetworkIngressNodeAuthKey(ctx, repo.ClearNetworkIngressNodeAuthKeyParams{
			NetworkIngressID: h.ingress.id, OrganizationID: h.ingress.organizationID, Slot: g.cfg.Ordinal,
		}); err != nil {
			g.logger.ErrorContext(ctx, "clear tailnet auth key", attr.SlogError(err))
		} else {
			h.authKeyCleared = true
		}
	}
	if (previous == NodeStatusOnline) != (next == NodeStatusOnline) {
		g.wakeReconciler(ctx, h.ingress)
	}
}

func (g *Gateway) writeHeartbeat(ctx context.Context, ingress desiredIngress, status NodeStatus, nodeID, dnsName, message string) {
	if len(message) > maxErrorLength {
		message = message[:maxErrorLength]
	}
	rows, err := repo.New(g.db).HeartbeatNetworkIngressNode(ctx, repo.HeartbeatNetworkIngressNodeParams{
		PodName: conv.ToPGTextEmpty(g.cfg.PodName), Status: string(status),
		NodeID: conv.ToPGTextEmpty(nodeID), DnsName: conv.ToPGTextEmpty(dnsName), LastError: conv.ToPGTextEmpty(message),
		NetworkIngressID: ingress.id, OrganizationID: ingress.organizationID, Slot: g.cfg.Ordinal,
	})
	if err != nil {
		g.metrics.Lifecycle(ctx, ingress.organizationSlug, OperationHeartbeat, ResultError)
		g.logger.ErrorContext(ctx, "write tailnet node heartbeat", attr.SlogError(err), attr.SlogNetworkIngressID(ingress.id.String()))
		return
	}
	if rows == 0 {
		g.logger.WarnContext(ctx, "tailnet node slot row is missing", attr.SlogNetworkIngressID(ingress.id.String()))
	}
	g.metrics.Lifecycle(ctx, ingress.organizationSlug, OperationHeartbeat, ResultOK)
}

// wakeReconciler asks the reconciler to re-observe the ingress so the
// dashboard reflects the new host state before the hourly sweep.
func (g *Gateway) wakeReconciler(ctx context.Context, ingress desiredIngress) {
	tx, err := g.db.Begin(ctx)
	if err != nil {
		g.logger.ErrorContext(ctx, "begin reconcile wake", attr.SlogError(err))
		return
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(ctx)) })
	if err := g.requester.Enqueue(ctx, tx, ingress.organizationID, ingress.id); err != nil {
		g.logger.ErrorContext(ctx, "enqueue reconcile wake", attr.SlogError(err))
		return
	}
	if err := tx.Commit(ctx); err != nil {
		g.logger.ErrorContext(ctx, "commit reconcile wake", attr.SlogError(err))
	}
}

func (g *Gateway) stopHost(ctx context.Context, h *host, final NodeStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	g.writeHeartbeat(ctx, h.ingress, NodeStatusDraining, h.nodeID, h.dnsName, "")
	closeCtx, cancel := context.WithTimeout(ctx, g.cfg.DrainTimeout)
	defer cancel()
	result := ResultOK
	if err := h.node.Close(closeCtx); err != nil {
		result = ResultError
		g.logger.WarnContext(ctx, "close tailnet node", attr.SlogError(err), attr.SlogNetworkIngressID(h.ingress.id.String()))
	}
	g.metrics.Lifecycle(ctx, h.ingress.organizationSlug, OperationStop, result)
	g.metrics.HostStatusChanged(ctx, h.ingress.organizationSlug, g.cfg.Ordinal, h.status, "")
	wasOnline := h.status == NodeStatusOnline
	h.status = final
	g.writeHeartbeat(ctx, h.ingress, final, h.nodeID, h.dnsName, "")
	if wasOnline {
		g.wakeReconciler(ctx, h.ingress)
	}
}

// drain stops every host; it runs with a context that survives cancellation
// so the final heartbeats are written.
func (g *Gateway) drain(ctx context.Context) {
	g.starts.Wait()
	g.mu.Lock()
	hosts := make([]*host, 0, len(g.hosts))
	for id, h := range g.hosts {
		hosts = append(hosts, h)
		delete(g.hosts, id)
	}
	g.mu.Unlock()

	var wg sync.WaitGroup
	for _, h := range hosts {
		wg.Add(1)
		go func(h *host) {
			defer wg.Done()
			g.stopHost(ctx, h, NodeStatusStopped)
		}(h)
	}
	wg.Wait()
}

// handler builds the per-ingress request chain: resolve the tailnet peer,
// stamp identity headers, then proxy to the private listener with the pod's
// attestation token.
func (g *Gateway) handler(want desiredIngress, node Node, fqdn string) (http.Handler, error) {
	proxy, err := netingress.NewAttestorHandler(netingress.AttestorConfig{
		Upstream:     g.cfg.Upstream,
		ExpectedHost: fqdn,
		TokenPath:    g.cfg.TokenPath,
		Transport:    g.cfg.UpstreamTransport,
		Logger:       g.logger.With(attr.SlogNetworkIngressID(want.id.String())),
		Telemetry:    g.telemetry,
	})
	if err != nil {
		return nil, fmt.Errorf("build private listener proxy: %w", err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := PeerAddr(r)
		if err != nil {
			http.Error(w, "unknown tailnet peer", http.StatusForbidden)
			return
		}
		who, err := node.WhoIs(r.Context(), peer)
		if err != nil {
			g.metrics.Identity(r.Context(), want.organizationSlug, IdentityKindUnknown)
			http.Error(w, "unknown tailnet peer", http.StatusForbidden)
			return
		}
		kind := SetIdentityHeaders(r.Header, who)
		g.metrics.Identity(r.Context(), want.organizationSlug, kind)
		proxy.ServeHTTP(w, r)
	}), nil
}

// HostSummary describes one running host for diagnostics.
type HostSummary struct {
	// IngressID is the hosted ingress.
	IngressID uuid.UUID

	// Status is the last heartbeat status.
	Status NodeStatus

	// DNSName is the advertised service name.
	DNSName string
}

// Hosts lists running hosts in no particular order.
func (g *Gateway) Hosts() []HostSummary {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]HostSummary, 0, len(g.hosts))
	for _, h := range g.hosts {
		h.mu.Lock()
		out = append(out, HostSummary{IngressID: h.ingress.id, Status: h.status, DNSName: h.dnsName})
		h.mu.Unlock()
	}
	return out
}
