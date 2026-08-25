// Package replyinbox routes enforcement replies from a replica-scoped Redis
// list to in-process scan waiters.
package replyinbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	DefaultBlockTimeout = time.Second
	defaultReplyTTL     = 60 * time.Second
	defaultDrainCount   = 128
	defaultPoolSize     = 2
	reconnectBackoff    = 100 * time.Millisecond
)

var ErrDuplicateWaiter = errors.New("enforcement reply waiter already registered")

// Config controls a replica's dedicated blocking Redis client.
type Config struct {
	// RedisOptions are copied before reply-inbox settings are applied.
	RedisOptions redis.Options

	// ReplicaID identifies the process in reply URNs and Redis inbox keys.
	ReplicaID string

	// BlockTimeout bounds each BLPOP so cancellation and reconnects are observed.
	// go-redis v9 gives blocking commands an internal BlockTimeout+10s socket
	// read deadline; RedisOptions.ReadTimeout does not control BLPOP. A TCP stall
	// beyond that margin can still lose an element popped by Redis, so callers
	// must retain a deadline and apply their configured failure mode.
	BlockTimeout time.Duration

	// DrainGate blocks routing after BLPOP until the channel closes. It is nil
	// in production and supports controlled backlog tests without stopping Redis.
	DrainGate <-chan struct{}

	drainFunc func(context.Context)
}

// Lane identifies one distinct enforcement result. PolicyID is empty except
// for policy-specific lanes such as judge.
type Lane struct {
	// Scanner identifies the enforcement engine.
	Scanner riskv1.EnforcementScanner

	// PolicyID distinguishes multiple policy-specific results from one scanner.
	PolicyID string
}

// Outcome folds complete and deadline-limited enforcement replies by lane.
type Outcome struct {
	// ByLane contains the first reply received for each requested lane.
	ByLane map[Lane]*riskv1.EnforcementReply

	// Complete reports whether every requested lane replied.
	Complete bool

	// Deadline reports that the wait ceiling elapsed before all lanes replied.
	Deadline bool
}

// Stats is a point-in-time snapshot of reply-inbox load and Redis pool state.
type Stats struct {
	// Waiters is the number of scans currently registered in this process.
	Waiters int

	// OrphanedReplies is the number of decoded replies with no local waiter.
	OrphanedReplies uint64

	// DrainBatches is the number of BLPOP wake cycles completed.
	DrainBatches uint64

	// DrainedReplies is the number of Redis list entries routed or discarded.
	DrainedReplies uint64

	// MaxDrainBatch is the largest BLPOP plus LPOP catch-up batch observed.
	MaxDrainBatch uint64

	// RedisPool is the dedicated drainer client's current pool snapshot.
	RedisPool *redis.PoolStats

	// DrainerAlive reports whether the supervised drainer is currently running.
	DrainerAlive bool

	// DrainerErrors is the number of unexpected drainer exits or panics.
	DrainerErrors uint64
}

// Inbox owns one replica-scoped drainer and its in-process waiter map.
type Inbox struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	metrics   inboxMetrics
	options   redis.Options
	clientMu  sync.Mutex
	client    *redis.Client
	replicaID string
	key       string
	block     time.Duration
	drainGate <-chan struct{}

	orphanedReplies atomic.Uint64
	drainBatches    atomic.Uint64
	drainedReplies  atomic.Uint64
	maxDrainBatch   atomic.Uint64
	drainerAlive    atomic.Bool
	drainerErrors   atomic.Uint64

	cancel    context.CancelFunc
	done      chan struct{}
	shutdown  chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex
	waiters map[string]*waiter
}

type waiter struct {
	mu      sync.Mutex
	lanes   map[Lane]struct{}
	replies map[Lane]*riskv1.EnforcementReply
	notify  chan struct{}
	closed  bool
}

type inboxMetrics struct {
	roundTrip metric.Float64Histogram
	replies   metric.Int64Counter
	orphaned  metric.Int64Counter
	alive     metric.Int64Gauge
	drainErrs metric.Int64Counter
}

// New starts one Redis drainer for a stable process replica id.
func New(
	ctx context.Context,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	cfg Config,
) (*Inbox, error) {
	if cfg.ReplicaID == "" {
		cfg.ReplicaID = uuid.NewString()
	}
	if cfg.BlockTimeout <= 0 {
		cfg.BlockTimeout = DefaultBlockTimeout
	}
	if cfg.BlockTimeout < time.Second {
		return nil, fmt.Errorf("reply inbox block timeout %s must be at least 1s", cfg.BlockTimeout)
	}
	if !validReplicaID(cfg.ReplicaID) {
		return nil, fmt.Errorf("invalid reply inbox replica id %q", cfg.ReplicaID)
	}

	redisOptions := cfg.RedisOptions
	redisOptions.PoolSize = defaultPoolSize
	redisOptions.DisableIdentity = true
	client := redis.NewClient(&redisOptions)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping reply inbox redis: %w", err)
	}

	metrics, err := newInboxMetrics(meterProvider)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	drainCtx, cancel := context.WithCancel(ctx) //nolint:gosec // cancel is retained on the Inbox and called in Close
	inbox := &Inbox{
		logger:          logger.With(attr.SlogComponent("enforcement-reply-inbox")),
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/risk/replyinbox"),
		metrics:         metrics,
		options:         redisOptions,
		client:          client,
		replicaID:       cfg.ReplicaID,
		key:             InboxKey(cfg.ReplicaID),
		block:           cfg.BlockTimeout,
		drainGate:       cfg.DrainGate,
		orphanedReplies: atomic.Uint64{},
		drainBatches:    atomic.Uint64{},
		drainedReplies:  atomic.Uint64{},
		maxDrainBatch:   atomic.Uint64{},
		drainerAlive:    atomic.Bool{},
		drainerErrors:   atomic.Uint64{},
		cancel:          cancel,
		done:            make(chan struct{}),
		shutdown:        make(chan struct{}),
		closeOnce:       sync.Once{},
		clientMu:        sync.Mutex{},
		mu:              sync.Mutex{},
		waiters:         make(map[string]*waiter),
	}
	drainFunc := inbox.drain
	if cfg.drainFunc != nil {
		drainFunc = cfg.drainFunc
	}
	go inbox.superviseDrainer(drainCtx, drainFunc)

	return inbox, nil
}

// Snapshot returns load counters without retaining a Redis connection.
func (i *Inbox) Snapshot() Stats {
	i.mu.Lock()
	waiters := len(i.waiters)
	i.mu.Unlock()

	i.clientMu.Lock()
	pool := i.client.PoolStats()
	i.clientMu.Unlock()

	return Stats{
		Waiters:         waiters,
		OrphanedReplies: i.orphanedReplies.Load(),
		DrainBatches:    i.drainBatches.Load(),
		DrainedReplies:  i.drainedReplies.Load(),
		MaxDrainBatch:   i.maxDrainBatch.Load(),
		RedisPool:       pool,
		DrainerAlive:    i.drainerAlive.Load(),
		DrainerErrors:   i.drainerErrors.Load(),
	}
}

func newInboxMetrics(meterProvider metric.MeterProvider) (inboxMetrics, error) {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/risk/replyinbox")
	roundTrip, err := meter.Float64Histogram(
		"risk.enforcement.round_trip_duration",
		metric.WithDescription("End-to-end enforcement request-reply duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		return inboxMetrics{}, fmt.Errorf("create risk.enforcement.round_trip_duration metric: %w", err)
	}
	replies, err := meter.Int64Counter(
		"risk.enforcement.replies",
		metric.WithDescription("Total enforcement replies by status"),
		metric.WithUnit("{reply}"),
	)
	if err != nil {
		return inboxMetrics{}, fmt.Errorf("create risk.enforcement.replies metric: %w", err)
	}
	orphaned, err := meter.Int64Counter(
		"risk.enforcement.orphaned_replies",
		metric.WithDescription("Total enforcement replies without a local waiter"),
		metric.WithUnit("{reply}"),
	)
	if err != nil {
		return inboxMetrics{}, fmt.Errorf("create risk.enforcement.orphaned_replies metric: %w", err)
	}
	alive, err := meter.Int64Gauge(
		"risk.enforcement.drainer_alive",
		metric.WithDescription("Whether the replica enforcement reply drainer is running"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return inboxMetrics{}, fmt.Errorf("create risk.enforcement.drainer_alive metric: %w", err)
	}
	drainErrs, err := meter.Int64Counter(
		"risk.enforcement.drainer_errors",
		metric.WithDescription("Unexpected enforcement reply drainer exits that triggered a restart"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return inboxMetrics{}, fmt.Errorf("create risk.enforcement.drainer_errors metric: %w", err)
	}
	return inboxMetrics{
		roundTrip: roundTrip,
		replies:   replies,
		orphaned:  orphaned,
		alive:     alive,
		drainErrs: drainErrs,
	}, nil
}

func validReplicaID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// ReplicaID returns the stable id represented by this inbox.
func (i *Inbox) ReplicaID() string {
	return i.replicaID
}

// ReplyURN returns the return address for scanID on this replica.
func (i *Inbox) ReplyURN(scanID string) string {
	return ReplyURN(i.replicaID, scanID)
}

// Await waits for one reply from every distinct lane or until ctx ends. It
// accepts a caller-supplied scan ID so the standalone reply-leg load harness
// can pair synthetic writers with a waiter. Production callers use Dispatcher,
// which mints the scan ID internally.
func (i *Inbox) Await(ctx context.Context, scanID string, lanes []Lane) (Outcome, error) {
	started := time.Now()
	w, release, err := i.register(scanID, lanes)
	if err != nil {
		return Outcome{}, err
	}
	defer release()
	return i.awaitRegistered(ctx, scanID, w, started)
}

func (i *Inbox) register(scanID string, lanes []Lane) (*waiter, func(), error) {
	requested := make(map[Lane]struct{}, len(lanes))
	for _, lane := range lanes {
		if _, duplicate := requested[lane]; duplicate {
			return nil, nil, fmt.Errorf("duplicate enforcement lane %s", lane.String())
		}
		requested[lane] = struct{}{}
	}
	w := &waiter{
		mu:      sync.Mutex{},
		lanes:   requested,
		replies: make(map[Lane]*riskv1.EnforcementReply, len(lanes)),
		notify:  make(chan struct{}, 1),
		closed:  false,
	}
	i.mu.Lock()
	if _, exists := i.waiters[scanID]; exists {
		i.mu.Unlock()
		return nil, nil, fmt.Errorf("scan %s: %w", scanID, ErrDuplicateWaiter)
	}
	i.waiters[scanID] = w
	i.mu.Unlock()

	var once sync.Once
	release := func() {
		once.Do(func() {
			i.mu.Lock()
			delete(i.waiters, scanID)
			i.mu.Unlock()
		})
	}
	return w, release, nil
}

func (i *Inbox) awaitRegistered(ctx context.Context, scanID string, w *waiter, started time.Time) (Outcome, error) {
	ctx, span := i.tracer.Start(ctx, "risk.enforcement.await", trace.WithAttributes(attribute.String("risk.enforcement.scan_id", scanID)))
	defer span.End()
	defer func() {
		i.metrics.roundTrip.Record(ctx, time.Since(started).Seconds())
	}()

	for {
		w.mu.Lock()
		if len(w.replies) == len(w.lanes) {
			w.closed = true
			outcome := waiterOutcome(w, true, false)
			w.mu.Unlock()
			return outcome, nil
		}
		w.mu.Unlock()

		select {
		case <-ctx.Done():
			w.mu.Lock()
			w.closed = true
			deadline := errors.Is(ctx.Err(), context.DeadlineExceeded)
			outcome := waiterOutcome(w, false, deadline)
			w.mu.Unlock()
			ctxErr := ctx.Err()
			span.SetStatus(codes.Error, ctxErr.Error())
			if deadline {
				return outcome, nil
			}
			return outcome, fmt.Errorf("await enforcement replies: %w", ctxErr)
		case <-i.shutdown:
			w.mu.Lock()
			w.closed = true
			outcome := waiterOutcome(w, false, false)
			w.mu.Unlock()
			span.SetStatus(codes.Error, "reply inbox closed")
			return outcome, fmt.Errorf("await enforcement replies: reply inbox closed")
		case <-w.notify:
		}
	}
}

func waiterOutcome(w *waiter, complete, deadline bool) Outcome {
	byLane := make(map[Lane]*riskv1.EnforcementReply, len(w.replies))
	maps.Copy(byLane, w.replies)
	return Outcome{ByLane: byLane, Complete: complete, Deadline: deadline}
}

// String returns a stable diagnostic representation of a lane.
func (l Lane) String() string {
	if l.PolicyID == "" {
		return l.Scanner.String()
	}
	return l.Scanner.String() + "/" + l.PolicyID
}

func (i *Inbox) superviseDrainer(ctx context.Context, run func(context.Context)) {
	defer close(i.done)
	metricCtx := context.WithoutCancel(ctx)
	for ctx.Err() == nil {
		i.drainerAlive.Store(true)
		i.metrics.alive.Record(metricCtx, 1)
		panicked, recovered, stack := invokeDrainer(ctx, run)
		i.drainerAlive.Store(false)
		i.metrics.alive.Record(metricCtx, 0)
		if ctx.Err() != nil {
			return
		}

		i.drainerErrors.Add(1)
		i.metrics.drainErrs.Add(metricCtx, 1)
		if panicked {
			i.logger.ErrorContext(ctx, "enforcement reply drainer panicked; restarting",
				attr.SlogErrorMessage(fmt.Sprintf("%v", recovered)),
				attr.SlogErrorStack(stack),
			)
		} else {
			i.logger.ErrorContext(ctx, "enforcement reply drainer exited unexpectedly; restarting",
				attr.SlogError(errors.New("drainer exited while its context was active")),
			)
		}

		timer := time.NewTimer(reconnectBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func invokeDrainer(ctx context.Context, run func(context.Context)) (panicked bool, recovered any, stack string) {
	defer func() {
		if value := recover(); value != nil {
			panicked = true
			recovered = value
			stack = string(debug.Stack())
		}
	}()
	run(ctx)
	return false, nil, ""
}

func (i *Inbox) drain(ctx context.Context) {
	for ctx.Err() == nil {
		client := i.redisClient()
		first, err := client.BLPop(ctx, i.block, i.key).Result()
		switch {
		case errors.Is(err, redis.Nil):
			continue
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			timer := time.NewTimer(reconnectBackoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			i.replaceClient(ctx, client)
			continue
		}

		if len(first) == 2 {
			if i.drainGate != nil {
				select {
				case <-ctx.Done():
					return
				case <-i.drainGate:
				}
			}

			batchSize := uint64(1)
			i.route(ctx, first[1])
			for {
				rest, err := client.LPopCount(ctx, i.key, defaultDrainCount).Result()
				if errors.Is(err, redis.Nil) || len(rest) == 0 {
					break
				}
				if err != nil {
					break
				}
				batchSize += uint64(len(rest))
				for _, raw := range rest {
					i.route(ctx, raw)
				}
			}
			i.drainBatches.Add(1)
			i.drainedReplies.Add(batchSize)
			for previous := i.maxDrainBatch.Load(); batchSize > previous; previous = i.maxDrainBatch.Load() {
				if i.maxDrainBatch.CompareAndSwap(previous, batchSize) {
					break
				}
			}
		}
	}
}

func (i *Inbox) redisClient() *redis.Client {
	i.clientMu.Lock()
	defer i.clientMu.Unlock()
	return i.client
}

func (i *Inbox) replaceClient(ctx context.Context, stale *redis.Client) {
	i.clientMu.Lock()
	defer i.clientMu.Unlock()
	if i.client != stale || ctx.Err() != nil {
		return
	}
	_ = i.client.Close()
	i.client = redis.NewClient(&i.options)
}

func (i *Inbox) route(ctx context.Context, raw string) {
	reply := new(riskv1.EnforcementReply)
	if err := proto.Unmarshal([]byte(raw), reply); err != nil {
		i.logger.WarnContext(ctx, "discard malformed enforcement reply", attr.SlogError(err))
		return
	}
	i.metrics.replies.Add(ctx, 1, metric.WithAttributes(attribute.String("status", statusLabel(reply.GetStatus()))))

	lane := Lane{Scanner: reply.GetScanner(), PolicyID: reply.GetPolicyId()}
	i.mu.Lock()
	w := i.waiters[reply.GetScanId()]
	if w == nil {
		i.mu.Unlock()
		i.recordOrphan(ctx)
		return
	}

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		i.mu.Unlock()
		i.recordOrphan(ctx)
		return
	}
	if _, requested := w.lanes[lane]; !requested {
		w.mu.Unlock()
		i.mu.Unlock()
		i.recordOrphan(ctx)
		return
	}
	if _, duplicate := w.replies[lane]; duplicate {
		w.mu.Unlock()
		i.mu.Unlock()
		return
	}
	w.replies[lane] = reply
	w.mu.Unlock()
	select {
	case w.notify <- struct{}{}:
	default:
	}
	i.mu.Unlock()
}

func (i *Inbox) recordOrphan(ctx context.Context) {
	i.metrics.orphaned.Add(ctx, 1)
	i.orphanedReplies.Add(1)
}

func statusLabel(status riskv1.EnforcementStatus) string {
	return strings.ToLower(strings.TrimPrefix(status.String(), "ENFORCEMENT_STATUS_"))
}

// Close stops the drainer, releases any blocked waiters, and closes the
// dedicated Redis client.
func (i *Inbox) Close() error {
	i.closeOnce.Do(func() { close(i.shutdown) })
	i.cancel()
	i.clientMu.Lock()
	err := i.client.Close()
	i.clientMu.Unlock()
	<-i.done
	if err != nil {
		return fmt.Errorf("close reply inbox redis: %w", err)
	}
	return nil
}
