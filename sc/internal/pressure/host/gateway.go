package host

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/sc"
)

const (
	// DefaultPollInterval is how often the gateway reconciles desired
	// ingresses and heartbeats its hosts.
	DefaultPollInterval = 15 * time.Second

	// DefaultDrainTimeout bounds how long in-flight requests may finish.
	DefaultDrainTimeout = 20 * time.Second

	// DefaultStartTimeout bounds node registration and connection.
	DefaultStartTimeout = 60 * time.Second

	// DefaultMaxHosts caps nodes per pod.
	DefaultMaxHosts = 200

	// DefaultShard is the multi-tenant gateway every ingress starts on.
	DefaultShard = "shared"

	// DefaultTag is the ACL tag every gateway node advertises.
	DefaultTag = "tag:speakeasy"

	// maxErrorLength bounds the error text persisted in heartbeats.
	maxErrorLength = 240
)

// Config is the gateway pod's runtime configuration. The upstream, token
// and transport fields of the original belong to the request path, which
// this port leaves out; Handler stands in for them.
type Config struct {
	Ordinal      int32
	PodName      string
	Shard        string
	StateDir     string
	Tag          string
	PollInterval time.Duration
	DrainTimeout time.Duration
	StartTimeout time.Duration
	MaxHosts     int
	ControlURL   string

	// Handler builds the request chain for a started node; nil serves 404.
	Handler func(ingress Ingress, node Node, fqdn string) (http.Handler, error)

	// Now supplies the clock; nil means time.Now.
	Now func() time.Time
}

// Validate rejects an incomplete configuration.
func (c Config) Validate() error {
	switch {
	case c.Ordinal < 0:
		return errors.New("tailnet gateway ordinal must not be negative")
	case c.Shard == "":
		return errors.New("tailnet gateway shard is required")
	case c.StateDir == "":
		return errors.New("tailnet gateway state directory is required")
	case c.Tag == "":
		return errors.New("tailnet gateway tag is required")
	case c.PollInterval <= 0 || c.DrainTimeout <= 0 || c.StartTimeout <= 0 || c.MaxHosts <= 0:
		return errors.New("tailnet gateway intervals and host cap must be positive")
	default:
		return nil
	}
}

type slotInstance = sc.Instance[State, Slot]

// Gateway hosts every desired ingress for one pod ordinal.
type Gateway struct {
	logger  *slog.Logger
	store   Store
	cfg     Config
	runtime Runtime
	metrics Metrics
	now     func() time.Time
	chart   *sc.Machine[State, Slot]

	// mu guards slots, the map itself; each instance guards its snapshot.
	mu     sync.Mutex
	slots  map[uuid.UUID]*slotInstance
	starts sync.WaitGroup

	ready    atomic.Bool
	lastPoll atomic.Int64

	unregister func()
}

// New builds a gateway; call Run to start it.
func New(logger *slog.Logger, store Store, metrics Metrics, cfg Config, runtime Runtime) (*Gateway, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if store == nil || metrics == nil || runtime == nil {
		return nil, errors.New("tailnet gateway requires a store, metrics and a node runtime")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	g := &Gateway{
		logger: logger.With(slog.String("component", "tailnet_gateway"), slog.Int("slot", int(cfg.Ordinal))),
		store:  store, cfg: cfg, runtime: runtime, metrics: metrics, now: cfg.Now, chart: nil,
		mu: sync.Mutex{}, slots: map[uuid.UUID]*slotInstance{}, starts: sync.WaitGroup{},
		ready: atomic.Bool{}, lastPoll: atomic.Int64{}, unregister: nil,
	}
	chart, err := newChart()
	if err != nil {
		return nil, err
	}
	if g.chart, err = bindChart(chart, gatewayEnv{g: g}); err != nil {
		return nil, err
	}

	unregister, err := metrics.ObserveCertificates(g.certificateObservations)
	if err != nil {
		return nil, fmt.Errorf("observe certificate expiry: %w", err)
	}
	g.unregister = unregister
	return g, nil
}

// Chart returns the slot chart, for export and exploration.
func (g *Gateway) Chart() *sc.Machine[State, Slot] {
	return g.chart
}

func (g *Gateway) certificateObservations() []CertificateObservation {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]CertificateObservation, 0, len(g.slots))
	for _, inst := range g.slots {
		snap := inst.Snapshot()
		if hosting(snap) && !snap.Context.CertificateExpiresAt.IsZero() {
			out = append(out, CertificateObservation{
				OrganizationSlug: snap.Context.Ingress.OrganizationSlug, IngressID: snap.Context.Ingress.ID,
				Slot: g.cfg.Ordinal, ExpiresAt: snap.Context.CertificateExpiresAt,
			})
		}
	}
	return out
}

// Ready reports whether the first reconcile pass completed.
func (g *Gateway) Ready() bool {
	return g.ready.Load()
}

// Healthy fails when the poll loop has stalled for three intervals. It is
// the liveness watchdog behind /healthz.
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
			if g.unregister != nil {
				g.unregister()
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

	rows, err := g.store.ListDesired(ctx, g.cfg.Ordinal, g.cfg.Shard)
	if err != nil {
		g.logger.ErrorContext(ctx, "list desired tailnet ingresses", slog.Any("error", err))
		g.probeAll(ctx)
		return
	}
	desired := make(map[uuid.UUID]Ingress, len(rows))
	for _, row := range rows {
		desired[row.ID] = row
	}

	var stops sync.WaitGroup
	for _, inst := range g.undesired(ctx, desired) {
		stops.Go(func() { g.stop(ctx, inst) })
	}
	stops.Wait()

	for _, inst := range g.startable(ctx, desired) {
		g.starts.Go(func() {
			// The original's start goroutine owned the slot until it had
			// probed the new node; drain waits for it. Its services run on
			// the instance's goroutines, so this one only waits for them.
			_, _ = inst.Await(context.WithoutCancel(ctx), func(s sc.Snapshot[State, Slot]) bool {
				return !s.In(StatePreparing) && !s.In(StateRegistering)
			})
			g.probe(ctx, inst)
		})
	}

	g.probeAll(ctx)
}

// undesired returns the running slots to stop: no longer desired, or desired
// under a different label. It forgets idle slots that are no longer desired
// and refreshes the rest from the desired set.
func (g *Gateway) undesired(ctx context.Context, desired map[uuid.UUID]Ingress) []*slotInstance {
	g.mu.Lock()
	defer g.mu.Unlock()

	var stops []*slotInstance
	for id, inst := range g.slots {
		snap := inst.Snapshot()
		want, ok := desired[id]
		switch {
		case snap.In(StateRunning) && (!ok || want.Label != snap.Context.Ingress.Label):
			stops = append(stops, inst)
		case !ok && snap.In(StateIdle):
			g.send(ctx, inst, Forget{})
			inst.Close()
			delete(g.slots, id)
		case ok:
			// Metrics stay keyed by the organization's current slug.
			g.send(ctx, inst, Refresh{Ingress: want})
		}
	}

	return stops
}

// startable starts each desired slot that is idle, not backing off, and
// within the host cap, and returns them. The Start transition is taken under
// the gateway lock, so the next poll sees the slot in flight and does not
// start it again.
func (g *Gateway) startable(ctx context.Context, desired map[uuid.UUID]Ingress) []*slotInstance {
	g.mu.Lock()
	defer g.mu.Unlock()

	active := 0
	for _, inst := range g.slots {
		if !inst.Snapshot().In(StateIdle) {
			active++
		}
	}

	var starts []*slotInstance
	for id, want := range desired {
		inst, ok := g.slots[id]
		if !ok {
			var err error
			inst, err = g.chart.Instance(ctx, Slot{
				Ingress: want, Message: "", NodeID: "", DNSName: "", Failures: 0, RetryAt: time.Time{}, Final: "",
				Registered: false, HadKey: false, AuthKey: "", AuthKeyCleared: false, CertificateExpiresAt: time.Time{}, Node: nil,
			}, sc.WithClock(g.now))
			if err != nil {
				g.logger.ErrorContext(ctx, "start tailnet gateway slot", slog.Any("error", err))
				continue
			}
			g.slots[id] = inst
		}

		snap := inst.Snapshot()
		switch {
		case !snap.In(StateIdle):
		case !g.chart.Can(snap, Start{Ingress: want}, g.now()):
			// Backing off.
		case active >= g.cfg.MaxHosts:
			g.logger.WarnContext(ctx, "tailnet gateway host cap reached", slog.String("network_ingress_id", id.String()))
		case g.send(ctx, inst, Start{Ingress: want}).Accepted():
			active++
			starts = append(starts, inst)
		}
	}

	return starts
}

// send delivers ev and logs an action that failed. Actions cannot fail here
// except by being unbound, which bindChart rules out.
func (g *Gateway) send(ctx context.Context, inst *slotInstance, ev any) sc.Step[State, Slot] {
	step, err := inst.Send(ctx, ev)
	if err != nil {
		g.logger.ErrorContext(ctx, "tailnet gateway slot action failed", slog.Any("error", err))
	}
	return step
}

// stop drains a running slot and returns once its node is closed and its
// final status recorded.
func (g *Gateway) stop(ctx context.Context, inst *slotInstance) {
	g.send(ctx, inst, Stop{})
	g.settle(ctx, inst)
}

// settle waits for what a stop or a probe started: closing a draining node,
// and clearing the key of one that came online. The original did both inside
// the call that decided them.
func (g *Gateway) settle(ctx context.Context, inst *slotInstance) {
	_, _ = inst.Await(ctx, func(s sc.Snapshot[State, Slot]) bool {
		return !s.In(StateDraining) && !s.In(StateClearingKey)
	})
}

func (g *Gateway) runningSlots() []*slotInstance {
	g.mu.Lock()
	defer g.mu.Unlock()

	out := make([]*slotInstance, 0, len(g.slots))
	for _, inst := range g.slots {
		if inst.Snapshot().In(StateRunning) {
			out = append(out, inst)
		}
	}
	return out
}

// probeAll refreshes every running slot. A probe that finds the node
// unusable drains it before the next slot is probed.
func (g *Gateway) probeAll(ctx context.Context) {
	for _, inst := range g.runningSlots() {
		g.probe(ctx, inst)
	}
}

// probe asks the slot's node for its status and gives the answer to the
// chart. The probe runs outside the instance; the chart ignores an answer
// about a node the slot no longer holds.
func (g *Gateway) probe(ctx context.Context, inst *slotInstance) {
	snap := inst.Snapshot()
	if !snap.In(StateRunning) {
		return
	}
	node := snap.Context.Node
	state, err := node.Status(ctx)
	g.send(ctx, inst, Probed{Node: node, State: state, Err: err})
	g.settle(ctx, inst)
}

// drain stops every running slot; it runs with a context that survives
// cancellation so the final heartbeats are written. The instances are closed
// afterwards, which stops what they still run.
func (g *Gateway) drain(ctx context.Context) {
	g.starts.Wait()

	var wg sync.WaitGroup
	for _, inst := range g.runningSlots() {
		wg.Go(func() { g.stop(ctx, inst) })
	}
	wg.Wait()

	g.mu.Lock()
	defer g.mu.Unlock()
	for _, inst := range g.slots {
		inst.Close()
	}
}

// HostSummary describes one running host for diagnostics.
type HostSummary struct {
	IngressID uuid.UUID
	Status    NodeStatus
	DNSName   string
}

// Hosts lists the slots hosting a node, in no particular order.
func (g *Gateway) Hosts() []HostSummary {
	g.mu.Lock()
	defer g.mu.Unlock()

	out := make([]HostSummary, 0, len(g.slots))
	for _, inst := range g.slots {
		if snap := inst.Snapshot(); hosting(snap) {
			status, _ := statusOf(current(snap))
			out = append(out, HostSummary{IngressID: snap.Context.Ingress.ID, Status: status, DNSName: snap.Context.DNSName})
		}
	}
	return out
}

// gatewayEnv carries out the chart's actions and services for a gateway.
type gatewayEnv struct{ g *Gateway }

func (e gatewayEnv) logger(c Slot) *slog.Logger {
	return e.g.logger.With(slog.String("network_ingress_id", c.Ingress.ID.String()), slog.String("organization_slug", c.Ingress.OrganizationSlug))
}

// prepare loads what a start needs. The original returned from start()
// without recording anything when the row was gone or unreadable; that is
// the gone outcome here.
func (e gatewayEnv) prepare(ctx context.Context, c Slot) (preparation, error) {
	g, want := e.g, c.Ingress
	gone := preparation{Outcome: prepGone, AuthKey: "", Registered: false, HadKey: false}

	row, err := g.store.GetSlot(ctx, want.ID, want.OrganizationID, g.cfg.Ordinal)
	if errors.Is(err, ErrSlotNotFound) {
		return gone, nil
	}
	if err != nil {
		e.logger(c).ErrorContext(ctx, "load tailnet node slot", slog.Any("error", err))
		return gone, nil
	}
	if row.Retired {
		return gone, nil
	}

	store, err := g.store.NodeStateStore(want.ID, want.OrganizationID, g.cfg.Ordinal)
	if err != nil {
		e.logger(c).ErrorContext(ctx, "build tailnet state store", slog.Any("error", err))
		return gone, e.startFailure(ctx, c, "node state unavailable")
	}
	registered, err := store.Registered(ctx)
	if err != nil {
		e.logger(c).ErrorContext(ctx, "check tailnet node registration", slog.Any("error", err))
		return gone, e.startFailure(ctx, c, secretFailure(err, "node state unreadable"))
	}
	if registered && row.Status == string(NodeStatusNeedsLogin) {
		return preparation{Outcome: prepRefused, AuthKey: "", Registered: true, HadKey: row.HasAuthKey}, nil
	}

	authKey := ""
	if !registered {
		if !row.HasAuthKey || row.AuthKeyEncrypted == "" {
			return preparation{Outcome: prepAwaitingKey, AuthKey: "", Registered: false, HadKey: false}, nil
		}
		if !row.AuthKeyExpiresAt.IsZero() && !row.AuthKeyExpiresAt.After(g.now()) {
			return preparation{Outcome: prepKeyExpired, AuthKey: "", Registered: false, HadKey: true}, nil
		}
		plaintext, err := g.store.DecryptAuthKey(ctx, want.OrganizationID, row.AuthKeyEncrypted)
		if err != nil {
			e.logger(c).ErrorContext(ctx, "decrypt tailnet auth key", slog.Any("error", err))
			return gone, e.startFailure(ctx, c, secretFailure(err, "auth key unreadable"))
		}
		authKey = plaintext
	}

	return preparation{Outcome: prepReady, AuthKey: authKey, Registered: registered, HadKey: row.HasAuthKey}, nil
}

// startFailure counts a failed start and returns the message it records.
func (e gatewayEnv) startFailure(ctx context.Context, c Slot, message string) error {
	e.g.metrics.Lifecycle(ctx, c.Ingress.OrganizationSlug, OperationStart, ResultError)
	return errors.New(message)
}

// register brings the node up and starts serving.
func (e gatewayEnv) register(ctx context.Context, c Slot) (registration, error) {
	g, want := e.g, c.Ingress
	logger := e.logger(c)

	store, err := g.store.NodeStateStore(want.ID, want.OrganizationID, g.cfg.Ordinal)
	if err != nil {
		logger.ErrorContext(ctx, "build tailnet state store", slog.Any("error", err))
		return registration{Node: nil, FQDN: ""}, e.startFailure(ctx, c, "node state unavailable")
	}

	startedAt := g.now()
	node, err := g.runtime.Start(ctx, NodeSpec{
		IngressID: want.ID, OrganizationID: want.OrganizationID, Slot: g.cfg.Ordinal,
		Hostname: NodeHostname(g.cfg.Ordinal), ServiceName: ServiceName(want.Label), Tag: g.cfg.Tag,
		AuthKey: c.AuthKey, Store: store, StateDir: filepath.Join(g.cfg.StateDir, want.ID.String()),
		ControlURL: g.cfg.ControlURL, StartTimeout: g.cfg.StartTimeout,
		CertificateEvents: func(ctx context.Context, operation, result string) {
			g.metrics.CertificateRequest(ctx, want.OrganizationSlug, operation, result)
		},
	})
	if err != nil {
		logger.ErrorContext(ctx, "start tailnet node", slog.Any("error", err))
		if c.Registered && errors.Is(err, ErrNodeNeedsLogin) {
			// The reconciler deletes the refused device, resets the slot
			// and mints a fresh key.
			g.metrics.Lifecycle(ctx, want.OrganizationSlug, OperationStart, ResultError)
			// Returned as is: the slot records its message, and the chart
			// classifies it with errors.Is.
			return registration{Node: nil, FQDN: ""}, err //nolint:wrapcheck // the runtime's message is what the row records
		}
		return registration{Node: nil, FQDN: ""}, e.failRegistration(ctx, c, store, err)
	}
	state, err := node.Status(ctx)
	if err != nil {
		_ = node.Close(ctx)
		return registration{Node: nil, FQDN: ""}, e.failRegistration(ctx, c, store, err)
	}
	handler := http.NotFoundHandler()
	if g.cfg.Handler != nil {
		if handler, err = g.cfg.Handler(want, node, state.FQDN); err != nil {
			_ = node.Close(ctx)
			return registration{Node: nil, FQDN: ""}, e.startFailure(ctx, c, err.Error())
		}
	}
	if err := node.Serve(handler); err != nil {
		_ = node.Close(ctx)
		return registration{Node: nil, FQDN: ""}, e.startFailure(ctx, c, err.Error())
	}
	g.metrics.Lifecycle(ctx, want.OrganizationSlug, OperationStart, ResultOK)
	g.metrics.StartDuration(ctx, want.OrganizationSlug, !c.Registered, g.now().Sub(startedAt))
	logger.InfoContext(ctx, "tailnet node started", slog.String("host_name", state.FQDN))

	return registration{Node: node, FQDN: state.FQDN}, nil
}

// failRegistration handles a start that failed before the node served. A
// first registration that fails may have consumed the one-shot key and left
// partial state behind; both are cleared so the reconciler mints a fresh
// key. A login that did complete is a real device and is kept.
func (e gatewayEnv) failRegistration(ctx context.Context, c Slot, store NodeStateStore, cause error) error {
	g := e.g
	g.metrics.Lifecycle(ctx, c.Ingress.OrganizationSlug, OperationStart, ResultError)
	if c.Registered {
		return errors.New(cause.Error())
	}
	nowRegistered, err := store.Registered(ctx)
	if err != nil {
		e.logger(c).ErrorContext(ctx, "recheck tailnet node registration", slog.Any("error", err))
		return errors.New(cause.Error())
	}
	if nowRegistered {
		return errors.New(cause.Error())
	}
	if err := store.Reset(ctx); err != nil {
		e.logger(c).ErrorContext(ctx, "reset partial tailnet node state", slog.Any("error", err))
	}
	if err := g.store.ClearAuthKey(ctx, c.Ingress.ID, c.Ingress.OrganizationID, g.cfg.Ordinal); err != nil {
		e.logger(c).ErrorContext(ctx, "clear tailnet auth key after failed registration", slog.Any("error", err))
	}
	return keyResetError{err: errors.New(cause.Error())}
}

// closeNode disconnects a draining node. Its failure is counted and logged;
// the chart records the final status either way, as the original did.
func (e gatewayEnv) closeNode(ctx context.Context, c Slot) (struct{}, error) {
	g := e.g
	closeCtx, cancel := context.WithTimeout(ctx, g.cfg.DrainTimeout)
	defer cancel()
	result := ResultOK
	err := c.Node.Close(closeCtx)
	if err != nil {
		result = ResultError
		e.logger(c).WarnContext(ctx, "close tailnet node", slog.Any("error", err))
		err = fmt.Errorf("close tailnet node: %w", err)
	}
	g.metrics.Lifecycle(ctx, c.Ingress.OrganizationSlug, OperationStop, result)
	return struct{}{}, err
}

// clearAuthKey removes the one-shot key once the node is online.
func (e gatewayEnv) clearAuthKey(ctx context.Context, c Slot) (struct{}, error) {
	if c.AuthKeyCleared {
		return struct{}{}, nil
	}
	if err := e.g.store.ClearAuthKey(ctx, c.Ingress.ID, c.Ingress.OrganizationID, e.g.cfg.Ordinal); err != nil {
		e.logger(c).ErrorContext(ctx, "clear tailnet auth key", slog.Any("error", err))
		return struct{}{}, fmt.Errorf("clear tailnet auth key: %w", err)
	}
	return struct{}{}, nil
}

// move carries out one move: the heartbeat, the hosts gauge, and the wake,
// in the original's order, so the reconciler reads the row it is woken for.
func (e gatewayEnv) move(ctx context.Context, c Slot, m move) {
	if m.Beat != "" {
		e.heartbeat(ctx, c, m.Beat)
	}
	if m.GaugeFrom != m.GaugeTo {
		e.g.metrics.HostStatusChanged(ctx, c.Ingress.OrganizationSlug, e.g.cfg.Ordinal, m.GaugeFrom, m.GaugeTo)
	}
	if m.Wake {
		e.wake(ctx, c)
	}
}

func (e gatewayEnv) heartbeat(ctx context.Context, c Slot, status NodeStatus) {
	g := e.g
	message := c.Message
	if len(message) > maxErrorLength {
		message = message[:maxErrorLength]
	}
	rows, err := g.store.Heartbeat(ctx, Heartbeat{
		IngressID: c.Ingress.ID, OrganizationID: c.Ingress.OrganizationID, Slot: g.cfg.Ordinal,
		PodName: g.cfg.PodName, Status: status, NodeID: c.NodeID, DNSName: c.DNSName, LastError: message,
	})
	if err != nil {
		g.metrics.Lifecycle(ctx, c.Ingress.OrganizationSlug, OperationHeartbeat, ResultError)
		e.logger(c).ErrorContext(ctx, "write tailnet node heartbeat", slog.Any("error", err))
		return
	}
	if rows == 0 {
		e.logger(c).WarnContext(ctx, "tailnet node slot row is missing")
	}
	g.metrics.Lifecycle(ctx, c.Ingress.OrganizationSlug, OperationHeartbeat, ResultOK)
}

func (e gatewayEnv) wake(ctx context.Context, c Slot) {
	if err := e.g.store.EnqueueReconcile(ctx, c.Ingress.OrganizationID, c.Ingress.ID); err != nil {
		e.logger(c).ErrorContext(ctx, "enqueue reconcile wake", slog.Any("error", err))
	}
}

// secretFailure is the slot message for a secret the gateway could not open.
func secretFailure(err error, unreadable string) string {
	if errors.Is(err, ErrEncryptionKeyUnavailable) {
		return "organization encryption key unavailable"
	}
	return unreadable
}
