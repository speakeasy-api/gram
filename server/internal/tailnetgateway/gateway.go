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
	"github.com/speakeasy-api/gram/server/internal/netingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
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

// Slot state machine
//
// Each ingress this pod may host is a slot. A slot's state is the NodeStatus
// the pod last recorded for it, which is also what the reconciler and the
// dashboard see: there is no separate in-memory status to drift from it.
// Events move a slot between states, and every move goes through
// (*Gateway).move, which writes the heartbeat row, keeps the host gauge
// current and wakes the reconciler when the transition calls for it.
//
// Transitions:
//
//   - pending -> starting: the reconciler installed a one-shot key.
//   - starting -> online: the node is up and the tailnet approved it to host
//     the service.
//   - starting | online -> awaiting_approval: the node is up but the tailnet
//     has not approved it, or revoked its approval; -> online once approved.
//   - starting | awaiting_approval -> awaiting_certificate: the node is
//     approved but holds no TLS certificate for the service yet; -> online
//     once the certificate is issued.
//   - starting -> error: the start failed; retried next poll, backing off
//     after maxStartFailures attempts in a row that did not reach online.
//   - any running state -> draining -> error: the node failed while running;
//     it is stopped and retried like a failed start.
//   - starting -> needs_login: control refused the slot's identity before
//     the node came up.
//   - any running state -> draining -> stopped: the ingress is no longer
//     desired, was renamed, or the pod is shutting down.
//   - any running state -> draining -> needs_login: control refused the
//     identity while the node ran.
//   - starting -> pending: the key is missing or expired; the slot waits for
//     the reconciler.
//
// Holds (pending without a key, needs_login awaiting re-registration) record
// a state without waking the reconciler: it already knows and is the one that
// clears them. Crossing the online boundary, and entering awaiting_approval,
// awaiting_certificate or needs_login, wake it so the dashboard shows the
// reason promptly.
type slot struct {
	// mu guards every field. Poll, start, probe and stop run from different
	// goroutines; the gateway lock is always taken before this one.
	mu sync.Mutex

	ingress desiredIngress

	// state is the status last recorded for the slot, "" before the first.
	state     NodeStatus
	lastError string

	// node is set while the slot hosts a running node.
	node    Node
	nodeID  string
	dnsName string

	// authKeyCleared is set once the one-shot key has been removed from the
	// slot row after the node persisted its state.
	authKeyCleared bool

	// inFlight is set while a start goroutine owns the slot.
	inFlight bool

	// certificate is the running node's certificate as last probed, nil
	// while none is held or the slot hosts no node. It is atomic so the
	// metrics collector never waits on a slot that is starting or stopping.
	certificate atomic.Pointer[CertificateObservation]

	// failures counts attempts in a row that did not reach online, whether
	// the start failed or the node died first. Once it reaches
	// maxStartFailures the slot is held in error until retryAt.
	failures int
	retryAt  time.Time
}

// running reports whether the slot hosts a node. The caller holds s.mu.
func (s *slot) running() bool {
	return s.node != nil
}

// transition is what an event decided for a slot: the state to record, the
// message stored with it, and whether to wake the reconciler.
type transition struct {
	to      NodeStatus
	message string
	wake    bool
}

// hold records a state without waking the reconciler: the slot is waiting on
// something the reconciler already knows about.
func hold(to NodeStatus, message string) transition {
	return transition{to: to, message: message, wake: false}
}

// wakes reports whether moving between two states should wake the
// reconciler: the host started or stopped serving, or it started waiting on
// something only the customer can fix.
func wakes(from, to NodeStatus) bool {
	if from == to {
		return false
	}

	switch {
	case from == NodeStatusDraining:
		// The stop that entered draining already woke the reconciler if the
		// host was serving; recording the final state is not news.
		return false
	case from == NodeStatusOnline, to == NodeStatusOnline:
		return true
	case to == NodeStatusAwaitingApproval, to == NodeStatusAwaitingCertificate, to == NodeStatusNeedsLogin:
		return true
	default:
		return false
	}
}

// probeTransition maps one status probe of a running node to the slot's next
// state.
func probeTransition(from NodeStatus, label string, state NodeState, err error) transition {
	t := transition{to: NodeStatusStarting, message: state.BackendState, wake: false}
	switch {
	case err != nil:
		t.to, t.message = NodeStatusError, err.Error()
	case state.Online:
		t.to, t.message = NodeStatusOnline, ""
	case state.NeedsLogin:
		t.to = NodeStatusNeedsLogin
	case state.AwaitingApproval:
		t.to = NodeStatusAwaitingApproval
		t.message = "the tailnet has not approved this node to host " + ServiceName(label)
	case state.AwaitingCertificate:
		t.to = NodeStatusAwaitingCertificate
		t.message = state.CertificateStatus
	}
	t.wake = wakes(from, t.to)

	return t
}

// Gateway hosts every desired ingress for one pod ordinal.
type Gateway struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	enc       *orgencryption.Service
	cfg       Config
	runtime   NodeRuntime
	metrics   *Metrics
	telemetry *netingress.Telemetry
	requester *networkingress.OutboxRequester
	now       func() time.Time

	// mu guards slots, the map itself; each slot guards its own fields.
	mu     sync.Mutex
	slots  map[uuid.UUID]*slot
	starts sync.WaitGroup

	ready    atomic.Bool
	lastPoll atomic.Int64

	// certificates reports the certificate expiry gauge until Run returns.
	certificates metric.Registration
}

// New builds a gateway; call Run to start it. enc decrypts auth keys and
// node state under each organization's key; the gateway never mints keys.
func New(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, enc *orgencryption.Service, cfg Config, runtime NodeRuntime) (*Gateway, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if db == nil || enc == nil || runtime == nil {
		return nil, errors.New("tailnet gateway requires a database, organization encryption and node runtime")
	}

	g := &Gateway{
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
		slots:     make(map[uuid.UUID]*slot),
		starts:    sync.WaitGroup{},
		ready:     atomic.Bool{},
		lastPoll:  atomic.Int64{},

		certificates: nil,
	}
	registration, err := g.metrics.ObserveCertificates(g.now, g.certificateObservations)
	if err != nil {
		return nil, err
	}
	g.certificates = registration
	return g, nil
}

// certificateObservations lists the certificate of every running node.
func (g *Gateway) certificateObservations() []CertificateObservation {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]CertificateObservation, 0, len(g.slots))
	for _, s := range g.slots {
		if c := s.certificate.Load(); c != nil {
			out = append(out, *c)
		}
	}
	return out
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
			if g.certificates != nil {
				if err := g.certificates.Unregister(); err != nil {
					g.logger.WarnContext(ctx, "unregister certificate expiry metric", attr.SlogError(err))
				}
			}
			return nil
		case <-ticker.C:
			g.Poll(ctx)
		}
	}
}

// Poll runs one reconcile pass. It stops slots that are no longer desired or
// were renamed, starts idle slots that are desired, and probes every running
// node. Stops finish before starts so a renamed ingress releases its old
// service first.
func (g *Gateway) Poll(ctx context.Context) {
	started := g.now()
	defer func() {
		g.lastPoll.Store(g.now().UnixNano())
		g.metrics.Poll(ctx, g.now().Sub(started))
	}()

	rows, err := repo.New(g.db).ListDesiredGatewayIngressesForSlot(ctx, repo.ListDesiredGatewayIngressesForSlotParams{Slot: g.cfg.Ordinal, GatewayShard: g.cfg.Shard})
	if err != nil {
		g.logger.ErrorContext(ctx, "list desired tailnet ingresses", attr.SlogError(err))
		g.probeAll(ctx)
		return
	}
	desired := make(map[uuid.UUID]desiredIngress, len(rows))
	for _, row := range rows {
		desired[row.ID] = desiredIngress{
			id: row.ID, organizationID: row.OrganizationID, organizationSlug: row.OrganizationSlug,
			label: row.Hostname, updatedAt: row.UpdatedAt.Time,
		}
	}

	var stops sync.WaitGroup
	for _, s := range g.undesired(desired) {
		stops.Go(func() { g.stop(ctx, s, NodeStatusStopped) })
	}
	stops.Wait()

	for _, s := range g.startable(ctx, desired) {
		g.starts.Go(func() { g.start(ctx, s) })
	}

	g.probeAll(ctx)
}

// undesired returns the running slots to stop: no longer desired, or desired
// under a different label. It forgets idle slots that are no longer desired
// and refreshes the rest from the desired set.
func (g *Gateway) undesired(desired map[uuid.UUID]desiredIngress) []*slot {
	g.mu.Lock()
	defer g.mu.Unlock()

	var stops []*slot
	for id, s := range g.slots {
		want, ok := desired[id]
		s.mu.Lock()
		switch {
		case s.running() && (!ok || want.label != s.ingress.label):
			stops = append(stops, s)
		case !ok && !s.running() && !s.inFlight:
			delete(g.slots, id)
		case ok:
			// Metrics stay keyed by the organization's current slug.
			s.ingress = want
		}
		s.mu.Unlock()
	}

	return stops
}

// startable returns the desired slots to start this poll: idle, not backing
// off, and within the host cap. Each is marked in flight before it is
// returned, so the next poll does not start it again.
func (g *Gateway) startable(ctx context.Context, desired map[uuid.UUID]desiredIngress) []*slot {
	g.mu.Lock()
	defer g.mu.Unlock()

	active := 0
	for _, s := range g.slots {
		s.mu.Lock()
		if s.running() || s.inFlight {
			active++
		}
		s.mu.Unlock()
	}

	var starts []*slot
	for id, want := range desired {
		s, ok := g.slots[id]
		if !ok {
			s = &slot{
				mu: sync.Mutex{}, ingress: want, state: "", lastError: "", node: nil, nodeID: "", dnsName: "",
				authKeyCleared: false, inFlight: false, failures: 0, retryAt: time.Time{},
				certificate: atomic.Pointer[CertificateObservation]{},
			}
			g.slots[id] = s
		}

		s.mu.Lock()
		switch {
		case s.running() || s.inFlight:
		case g.now().Before(s.retryAt):
		case active >= g.cfg.MaxHosts:
			g.logger.WarnContext(ctx, "tailnet gateway host cap reached", attr.SlogNetworkIngressID(id.String()))
		default:
			s.ingress = want
			s.inFlight = true
			active++
			starts = append(starts, s)
		}
		s.mu.Unlock()
	}

	return starts
}

// runningSlots snapshots the slots that host a node.
func (g *Gateway) runningSlots() []*slot {
	g.mu.Lock()
	defer g.mu.Unlock()

	out := make([]*slot, 0, len(g.slots))
	for _, s := range g.slots {
		s.mu.Lock()
		if s.running() {
			out = append(out, s)
		}
		s.mu.Unlock()
	}

	return out
}

// probeAll refreshes every running slot, stopping any that can no longer
// serve: its identity was refused, or its node failed.
func (g *Gateway) probeAll(ctx context.Context) {
	for _, s := range g.runningSlots() {
		if final := g.probe(ctx, s); final != "" {
			g.stop(ctx, s, final)
		}
	}
}

// move applies a transition to a slot: records the state, writes the
// heartbeat row, keeps the host gauge current and wakes the reconciler when
// the transition asks for it. The caller holds s.mu.
func (g *Gateway) move(ctx context.Context, s *slot, t transition) {
	from := s.state
	s.state = t.to
	s.lastError = t.message

	g.writeHeartbeat(ctx, s.ingress, t.to, s.nodeID, s.dnsName, t.message)
	if s.running() && from != t.to {
		g.metrics.HostStatusChanged(ctx, s.ingress.organizationSlug, g.cfg.Ordinal, from, t.to)
	}
	if t.wake {
		g.wakeReconciler(ctx, s.ingress)
	}
}

// settle applies a transition to a slot that is not being probed or stopped
// by the caller.
func (g *Gateway) settle(ctx context.Context, s *slot, t transition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g.move(ctx, s, t)
}

// noteFailure counts one failure; after maxStartFailures in a row the slot
// backs off instead of retrying every poll. The caller holds s.mu.
func (g *Gateway) noteFailure(s *slot) {
	s.failures++
	if s.failures >= maxStartFailures {
		s.retryAt = g.now().Add(backoffAfterFailures)
	}
}

// fail records a failed start: the slot moves to error and counts toward the
// backoff.
func (g *Gateway) fail(ctx context.Context, s *slot, message string, wake bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g.metrics.Lifecycle(ctx, s.ingress.organizationSlug, OperationStart, ResultError)
	g.noteFailure(s)
	g.move(ctx, s, transition{to: NodeStatusError, message: message, wake: wake})
}

// start brings one node up for an idle slot. It runs outside the gateway
// lock because registration can take tens of seconds, and it owns the slot
// until it returns.
func (g *Gateway) start(ctx context.Context, s *slot) {
	s.mu.Lock()
	want := s.ingress
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight = false
		s.mu.Unlock()
	}()
	logger := g.logger.With(attr.SlogNetworkIngressID(want.id.String()), attr.SlogOrganizationSlug(want.organizationSlug))

	queries := repo.New(g.db)
	row, err := queries.GetNetworkIngressNodeSlot(ctx, repo.GetNetworkIngressNodeSlotParams{
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
	if row.RetiredAt.Valid {
		return
	}

	store, err := NewPostgresStateStore(g.db, g.enc, want.id, want.organizationID, g.cfg.Ordinal)
	if err != nil {
		logger.ErrorContext(ctx, "build tailnet state store", attr.SlogError(err))
		g.fail(ctx, s, "node state unavailable", false)
		return
	}
	registered, err := store.Registered(ctx)
	if err != nil {
		// Usually the organization's key is unreachable or suspended. Record
		// it, so the slot backs off and the row says why it is not serving.
		logger.ErrorContext(ctx, "check tailnet node registration", attr.SlogError(err))
		g.fail(ctx, s, nodeStateFailure(err), false)
		return
	}
	if registered && row.Status == string(NodeStatusNeedsLogin) {
		// The identity was refused; starting again would only block until
		// the start timeout. Keep the beat fresh so the reconciler repairs it.
		g.settle(ctx, s, hold(NodeStatusNeedsLogin, "awaiting re-registration"))
		return
	}

	authKey := ""
	if !registered {
		if !row.AuthKeyEncrypted.Valid || row.AuthKeyEncrypted.String == "" {
			g.settle(ctx, s, hold(NodeStatusPending, "awaiting auth key"))
			return
		}
		if row.AuthKeyExpiresAt.Valid && !row.AuthKeyExpiresAt.Time.After(g.now()) {
			g.settle(ctx, s, hold(NodeStatusPending, "auth key expired"))
			return
		}
		plaintext, err := g.decryptAuthKey(ctx, want.organizationID, row.AuthKeyEncrypted.String)
		if err != nil {
			logger.ErrorContext(ctx, "decrypt tailnet auth key", attr.SlogError(err))
			g.fail(ctx, s, secretFailure(err, "auth key unreadable"), false)
			return
		}
		authKey = plaintext
	}

	g.settle(ctx, s, hold(NodeStatusStarting, ""))
	startedAt := g.now()
	node, err := g.runtime.Start(ctx, NodeSpec{
		IngressID: want.id, OrganizationID: want.organizationID, Slot: g.cfg.Ordinal,
		Hostname: NodeHostname(g.cfg.Ordinal), ServiceName: ServiceName(want.label), Tag: g.cfg.Tag,
		AuthKey: authKey, Store: store, StateDir: filepath.Join(g.cfg.StateDir, want.id.String()),
		ControlURL: g.cfg.ControlURL, StartTimeout: g.cfg.StartTimeout,
		CertificateEvents: func(ctx context.Context, operation CertificateOperation, result CertificateResult) {
			g.metrics.CertificateRequest(ctx, want.organizationSlug, operation, result)
		},
	})
	if err != nil {
		logger.ErrorContext(ctx, "start tailnet node", attr.SlogError(err))
		if registered && errors.Is(err, ErrNodeNeedsLogin) {
			// The reconciler deletes the refused device, resets the slot and
			// mints a fresh key.
			g.metrics.Lifecycle(ctx, want.organizationSlug, OperationStart, ResultError)
			g.settle(ctx, s, transition{to: NodeStatusNeedsLogin, message: err.Error(), wake: true})
			return
		}
		g.failRegistration(ctx, s, store, registered, err.Error())
		return
	}
	state, err := node.Status(ctx)
	if err != nil {
		_ = node.Close(ctx)
		g.failRegistration(ctx, s, store, registered, err.Error())
		return
	}
	handler, err := g.handler(want, node, state.FQDN)
	if err != nil {
		_ = node.Close(ctx)
		g.fail(ctx, s, err.Error(), false)
		return
	}
	if err := node.Serve(handler); err != nil {
		_ = node.Close(ctx)
		g.fail(ctx, s, err.Error(), false)
		return
	}
	g.metrics.Lifecycle(ctx, want.organizationSlug, OperationStart, ResultOK)
	g.metrics.StartDuration(ctx, want.organizationSlug, !registered, g.now().Sub(startedAt))

	s.mu.Lock()
	if s.running() {
		// Only one start owns a slot at a time, so this cannot happen; the
		// new node is closed rather than leaked if it ever does.
		s.mu.Unlock()
		_ = node.Close(ctx)
		return
	}
	s.node = node
	s.nodeID = ""
	s.dnsName = state.FQDN
	// The key row is the source of truth: a restart after a failed clear
	// still removes it once the node is online.
	s.authKeyCleared = !row.AuthKeyEncrypted.Valid
	g.metrics.HostStatusChanged(ctx, want.organizationSlug, g.cfg.Ordinal, "", s.state)
	s.mu.Unlock()
	logger.InfoContext(ctx, "tailnet node started", attr.SlogHostName(state.FQDN))

	if final := g.probe(ctx, s); final != "" {
		g.stop(ctx, s, final)
	}
}

// failRegistration records a start that failed before the node served. A
// first registration that fails may have consumed the one-shot key and left
// partial state behind; both are cleared so the reconciler mints a fresh key
// and the next attempt starts clean. A login that did complete is a real
// device on the tailnet, so that identity is kept and reconnected with next
// time rather than registering a second device.
func (g *Gateway) failRegistration(ctx context.Context, s *slot, store *PostgresStateStore, registered bool, message string) {
	wake := false
	if !registered {
		nowRegistered, err := store.Registered(ctx)
		if err != nil {
			g.logger.ErrorContext(ctx, "recheck tailnet node registration", attr.SlogError(err), attr.SlogNetworkIngressID(s.ingress.id.String()))
		}
		if err == nil && !nowRegistered {
			if err := store.Reset(ctx); err != nil {
				g.logger.ErrorContext(ctx, "reset partial tailnet node state", attr.SlogError(err))
			}
			if err := repo.New(g.db).ClearNetworkIngressNodeAuthKey(ctx, repo.ClearNetworkIngressNodeAuthKeyParams{
				NetworkIngressID: s.ingress.id, OrganizationID: s.ingress.organizationID, Slot: g.cfg.Ordinal,
			}); err != nil {
				g.logger.ErrorContext(ctx, "clear tailnet auth key after failed registration", attr.SlogError(err))
			}
			// The key is gone; the reconciler mints another.
			wake = true
		}
	}

	g.fail(ctx, s, message, wake)
}

// probe refreshes a running slot from its node and applies what it finds. It
// returns the state to stop the node with when it can no longer serve
// (needs_login when control refused its identity, error when the node
// failed), or "" to keep it running.
func (g *Gateway) probe(ctx context.Context, s *slot) NodeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running() {
		return ""
	}

	state, err := s.node.Status(ctx)
	if !state.CertificateExpiresAt.IsZero() {
		s.certificate.Store(&CertificateObservation{
			OrganizationSlug: s.ingress.organizationSlug, IngressID: s.ingress.id, Slot: g.cfg.Ordinal, ExpiresAt: state.CertificateExpiresAt,
		})
	}
	if state.NodeID != "" {
		s.nodeID = state.NodeID
	}
	if state.FQDN != "" {
		s.dnsName = state.FQDN
	}

	t := probeTransition(s.state, s.ingress.label, state, err)
	g.move(ctx, s, t)

	if t.to == NodeStatusOnline {
		// Serving is what counts as success for the backoff.
		s.failures = 0
		s.retryAt = time.Time{}
	}
	if t.to == NodeStatusOnline && !s.authKeyCleared {
		if err := repo.New(g.db).ClearNetworkIngressNodeAuthKey(ctx, repo.ClearNetworkIngressNodeAuthKeyParams{
			NetworkIngressID: s.ingress.id, OrganizationID: s.ingress.organizationID, Slot: g.cfg.Ordinal,
		}); err != nil {
			g.logger.ErrorContext(ctx, "clear tailnet auth key", attr.SlogError(err))
		} else {
			s.authKeyCleared = true
		}
	}

	switch t.to {
	case NodeStatusNeedsLogin, NodeStatusError:
		return t.to
	default:
		return ""
	}
}

// stop drains a running slot's node and records final. The slot stays known
// so a later poll can start it again. Stopping a node because of its current
// state keeps that state's message; a node that failed counts toward the
// backoff, so one that keeps dying is not restarted every poll.
func (g *Gateway) stop(ctx context.Context, s *slot, final NodeStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running() {
		return
	}

	message := ""
	if final == s.state {
		message = s.lastError
	}
	if final == NodeStatusError {
		g.noteFailure(s)
	}

	g.move(ctx, s, transition{to: NodeStatusDraining, message: message, wake: wakes(s.state, NodeStatusDraining)})

	closeCtx, cancel := context.WithTimeout(ctx, g.cfg.DrainTimeout)
	defer cancel()
	result := ResultOK
	if err := s.node.Close(closeCtx); err != nil {
		result = ResultError
		g.logger.WarnContext(ctx, "close tailnet node", attr.SlogError(err), attr.SlogNetworkIngressID(s.ingress.id.String()))
	}
	g.metrics.Lifecycle(ctx, s.ingress.organizationSlug, OperationStop, result)
	g.metrics.HostStatusChanged(ctx, s.ingress.organizationSlug, g.cfg.Ordinal, s.state, "")
	s.node = nil
	s.certificate.Store(nil)

	g.move(ctx, s, hold(final, message))
}

// drain stops every running slot; it runs with a context that survives
// cancellation so the final heartbeats are written.
func (g *Gateway) drain(ctx context.Context) {
	g.starts.Wait()

	var wg sync.WaitGroup
	for _, s := range g.runningSlots() {
		wg.Go(func() { g.stop(ctx, s, NodeStatusStopped) })
	}
	wg.Wait()
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

// Hosts lists the slots hosting a node, in no particular order.
func (g *Gateway) Hosts() []HostSummary {
	g.mu.Lock()
	defer g.mu.Unlock()

	out := make([]HostSummary, 0, len(g.slots))
	for _, s := range g.slots {
		s.mu.Lock()
		if s.running() {
			out = append(out, HostSummary{IngressID: s.ingress.id, Status: s.state, DNSName: s.dnsName})
		}
		s.mu.Unlock()
	}

	return out
}

// decryptAuthKey opens a slot's one-shot auth key under the organization's
// key.
func (g *Gateway) decryptAuthKey(ctx context.Context, organizationID, ciphertext string) (string, error) {
	keys, err := g.enc.For(organizationID, EncryptionUseCase)
	if err != nil {
		return "", fmt.Errorf("auth key encryption: %w", err)
	}
	plaintext, err := keys.Decrypt(ctx, authKeyColumn, ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt auth key: %w", err)
	}
	return string(plaintext), nil
}

// nodeStateFailure is the bounded message recorded when a slot's persisted
// state cannot be read.
func nodeStateFailure(err error) string {
	return secretFailure(err, "node state unreadable")
}

// secretFailure is the slot message for a secret the gateway could not open:
// the organization's key being unusable is named, so the customer can see
// the cause, and anything else is reported as unreadable.
func secretFailure(err error, unreadable string) string {
	if encryptionKeyUnavailable(err) {
		return "organization encryption key unavailable"
	}
	return unreadable
}
