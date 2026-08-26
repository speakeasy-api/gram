// Package replicainbox implements a generic request-reply rendezvous over a
// per-replica Redis list. Each process owns one list; downstream responders
// append encoded replies addressed by a URN carrying the replica and a
// correlation id, and one supervised drainer per process routes them to
// in-process waiters. Waiters fold one reply per distinct lane, so a caller
// that fans a request out to several responders collects exactly one answer
// from each. The risk enforcement path (internal/risk/enforcereply) is the
// first instantiation.
package replicainbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	// DefaultPollInterval paces LPOP polling while waiters are registered. It
	// bounds the added reply pickup latency; with no waiters registered the
	// drainer does not poll at all.
	DefaultPollInterval = 25 * time.Millisecond

	// DefaultReplyTTL is the inbox key expiry refreshed by every reply write.
	DefaultReplyTTL = 60 * time.Second

	defaultDrainCount = 128
	defaultPoolSize   = 2
	reconnectBackoff  = 100 * time.Millisecond
	// duplicateReplySlack sizes waiter buffers beyond the lane count so
	// redelivered duplicates cannot displace a distinct lane's reply.
	duplicateReplySlack = 8
)

var ErrDuplicateWaiter = errors.New("reply waiter already registered")

// Codec adapts one reply type to the inbox: how it serializes, which request
// it answers, and which lane of that request it settles.
type Codec[K comparable, R any] struct {
	// Decode parses one Redis list element into a reply.
	Decode func([]byte) (R, error)

	// Encode serializes a reply for the writer side.
	Encode func(R) ([]byte, error)

	// CorrelationID returns the request id the reply answers.
	CorrelationID func(R) string

	// Lane returns the distinct slot of the request this reply settles.
	Lane func(R) K

	// StatusLabel optionally labels the replies counter metric; nil records
	// the counter without a status attribute.
	StatusLabel func(R) string
}

func (c Codec[K, R]) validate() error {
	if c.Decode == nil || c.Encode == nil || c.CorrelationID == nil || c.Lane == nil {
		return errors.New("reply codec must set Decode, Encode, CorrelationID, and Lane")
	}
	return nil
}

// Config controls one replica's inbox: its Redis client, identity, and codec.
type Config[K comparable, R any] struct {
	// RedisOptions are copied before inbox settings are applied.
	RedisOptions redis.Options

	// ReplicaID identifies the process in reply URNs and Redis inbox keys.
	ReplicaID string

	// PollInterval paces non-blocking LPOP polling while waiters are
	// registered. Polling instead of BLPOP keeps ordinary read-timeout and
	// pool-reconnect semantics (a timed-out poll leaves the element in the
	// list, unlike a blocking pop racing its socket deadline) at the cost of
	// up to one interval of reply pickup latency.
	PollInterval time.Duration

	// URNNamespace names the URN family, e.g. "risk:enforce" yields
	// "urn:gram:risk:enforce:<replica>:<id>".
	URNNamespace string

	// Keyspace prefixes the Redis list key, e.g. "enforce:reply" yields
	// "enforce:reply:<replica>".
	Keyspace string

	// MetricPrefix namespaces the inbox metrics and the await span, e.g.
	// "risk.enforcement" yields "risk.enforcement.round_trip_duration".
	MetricPrefix string

	// Component labels the inbox's log lines.
	Component string

	// Codec adapts the reply type.
	Codec Codec[K, R]

	// DrainGate blocks routing after a drained batch until the channel
	// closes. It is nil in production and supports controlled backlog tests
	// without stopping Redis.
	DrainGate <-chan struct{}

	drainFunc func(context.Context)
}

// Outcome folds complete and deadline-limited replies by lane.
type Outcome[K comparable, R any] struct {
	// ByLane contains the first reply received for each requested lane.
	ByLane map[K]R

	// Complete reports whether every requested lane replied.
	Complete bool

	// Deadline reports that the wait ceiling elapsed before all lanes replied.
	Deadline bool
}

// Stats is a point-in-time snapshot of inbox load and Redis pool state.
type Stats struct {
	// Waiters is the number of requests currently registered in this process.
	Waiters int

	// OrphanedReplies is the number of decoded replies with no local waiter.
	OrphanedReplies uint64

	// DrainBatches is the number of poll cycles that routed at least one reply.
	DrainBatches uint64

	// DrainedReplies is the number of Redis list entries routed or discarded.
	DrainedReplies uint64

	// MaxDrainBatch is the largest single-cycle drain observed.
	MaxDrainBatch uint64

	// RedisPool is the dedicated drainer client's current pool snapshot.
	RedisPool *redis.PoolStats

	// DrainerAlive reports whether the supervised drainer is currently running.
	DrainerAlive bool

	// DrainerErrors is the number of unexpected drainer exits or panics.
	DrainerErrors uint64
}

// Inbox owns one replica-scoped drainer and its in-process waiter map.
type Inbox[K comparable, R any] struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	metrics   inboxMetrics
	spanName  string
	idAttr    string
	codec     Codec[K, R]
	client    *redis.Client
	replicaID string
	urnNS     string
	key       string
	poll      time.Duration
	drainGate <-chan struct{}

	// activeWaiters gates polling: with no requests in flight the drainer
	// sleeps on wake instead of issuing idle LPOPs.
	activeWaiters atomic.Int64
	wake          chan struct{}

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

	// correlation id -> *Waiter. Load/store churn is per request, not per
	// reply, and the router only ever Loads, sync.Map's optimized case.
	waiters sync.Map
}

// Waiter follows the net/rpc client shape: the router only looks a waiter up
// and sends into its buffered channel; folding, lane refusal, and duplicate
// dropping all happen on the awaiting goroutine.
type Waiter[K comparable, R any] struct {
	lanes map[K]struct{}
	reply chan R
}

type inboxMetrics struct {
	roundTrip metric.Float64Histogram
	replies   metric.Int64Counter
	orphaned  metric.Int64Counter
	alive     metric.Int64Gauge
	drainErrs metric.Int64Counter
}

// New starts one Redis drainer for a stable process replica id.
func New[K comparable, R any](
	ctx context.Context,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	cfg Config[K, R],
) (*Inbox[K, R], error) {
	if err := cfg.Codec.validate(); err != nil {
		return nil, err
	}
	if cfg.URNNamespace == "" || cfg.Keyspace == "" || cfg.MetricPrefix == "" {
		return nil, errors.New("reply inbox namespace, keyspace, and metric prefix are required")
	}
	if cfg.Component == "" {
		cfg.Component = "replica-inbox"
	}
	if cfg.ReplicaID == "" {
		cfg.ReplicaID = uuid.NewString()
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
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

	metrics, err := newInboxMetrics(meterProvider, cfg.MetricPrefix)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	drainCtx, cancel := context.WithCancel(ctx) //nolint:gosec // cancel is retained on the Inbox and called in Close
	inbox := &Inbox[K, R]{
		logger:          logger.With(attr.SlogComponent(cfg.Component)),
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/replicainbox"),
		metrics:         metrics,
		spanName:        cfg.MetricPrefix + ".await",
		idAttr:          cfg.MetricPrefix + ".scan_id",
		codec:           cfg.Codec,
		client:          client,
		replicaID:       cfg.ReplicaID,
		urnNS:           cfg.URNNamespace,
		key:             Key(cfg.Keyspace, cfg.ReplicaID),
		poll:            cfg.PollInterval,
		drainGate:       cfg.DrainGate,
		activeWaiters:   atomic.Int64{},
		wake:            make(chan struct{}, 1),
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
		waiters:         sync.Map{},
	}
	drainFunc := inbox.drain
	if cfg.drainFunc != nil {
		drainFunc = cfg.drainFunc
	}
	go inbox.superviseDrainer(drainCtx, drainFunc)

	return inbox, nil
}

// Snapshot returns load counters without retaining a Redis connection.
func (i *Inbox[K, R]) Snapshot() Stats {
	waiters := 0
	i.waiters.Range(func(_, _ any) bool {
		waiters++
		return true
	})
	pool := i.client.PoolStats()

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

func newInboxMetrics(meterProvider metric.MeterProvider, prefix string) (inboxMetrics, error) {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/replicainbox")
	roundTrip, err := meter.Float64Histogram(
		prefix+".round_trip_duration",
		metric.WithDescription("End-to-end request-reply duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		return inboxMetrics{}, fmt.Errorf("create %s.round_trip_duration metric: %w", prefix, err)
	}
	replies, err := meter.Int64Counter(
		prefix+".replies",
		metric.WithDescription("Total replies by status"),
		metric.WithUnit("{reply}"),
	)
	if err != nil {
		return inboxMetrics{}, fmt.Errorf("create %s.replies metric: %w", prefix, err)
	}
	orphaned, err := meter.Int64Counter(
		prefix+".orphaned_replies",
		metric.WithDescription("Total replies without a local waiter"),
		metric.WithUnit("{reply}"),
	)
	if err != nil {
		return inboxMetrics{}, fmt.Errorf("create %s.orphaned_replies metric: %w", prefix, err)
	}
	alive, err := meter.Int64Gauge(
		prefix+".drainer_alive",
		metric.WithDescription("Whether the replica reply drainer is running"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return inboxMetrics{}, fmt.Errorf("create %s.drainer_alive metric: %w", prefix, err)
	}
	drainErrs, err := meter.Int64Counter(
		prefix+".drainer_errors",
		metric.WithDescription("Unexpected reply drainer exits that triggered a restart"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return inboxMetrics{}, fmt.Errorf("create %s.drainer_errors metric: %w", prefix, err)
	}
	return inboxMetrics{
		roundTrip: roundTrip,
		replies:   replies,
		orphaned:  orphaned,
		alive:     alive,
		drainErrs: drainErrs,
	}, nil
}

var replicaIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validReplicaID(value string) bool {
	return replicaIDPattern.MatchString(value)
}

// ReplicaID returns the stable id represented by this inbox.
func (i *Inbox[K, R]) ReplicaID() string {
	return i.replicaID
}

// URN returns the return address for id on this replica.
func (i *Inbox[K, R]) URN(id string) string {
	return URN(i.urnNS, i.replicaID, id)
}

// Await waits for one reply from every distinct lane or until ctx ends.
func (i *Inbox[K, R]) Await(ctx context.Context, id string, lanes []K) (Outcome[K, R], error) {
	started := time.Now()
	w, release, err := i.Register(id, lanes)
	if err != nil {
		return Outcome[K, R]{}, err
	}
	defer release()
	return i.AwaitRegistered(ctx, id, w, started)
}

// Register installs a waiter before the request is published, so a fast reply
// cannot race an unregistered waiter. The returned release is idempotent and
// must run once the wait ends.
func (i *Inbox[K, R]) Register(id string, lanes []K) (*Waiter[K, R], func(), error) {
	requested := make(map[K]struct{}, len(lanes))
	for _, lane := range lanes {
		if _, duplicate := requested[lane]; duplicate {
			return nil, nil, fmt.Errorf("duplicate reply lane %v", lane)
		}
		requested[lane] = struct{}{}
	}
	// Buffered past the lane count so at-least-once redeliveries cannot crowd
	// a distinct lane's reply out of the buffer; the router's send never
	// blocks, and overflow beyond the slack is dropped and counted.
	w := &Waiter[K, R]{
		lanes: requested,
		reply: make(chan R, len(lanes)+duplicateReplySlack),
	}
	if _, exists := i.waiters.LoadOrStore(id, w); exists {
		return nil, nil, fmt.Errorf("request %s: %w", id, ErrDuplicateWaiter)
	}
	i.activeWaiters.Add(1)
	// Nudge the drainer out of its idle sleep; the buffer coalesces nudges.
	select {
	case i.wake <- struct{}{}:
	default:
	}
	release := func() {
		if _, loaded := i.waiters.LoadAndDelete(id); loaded {
			i.activeWaiters.Add(-1)
		}
	}
	return w, release, nil
}

// AwaitRegistered folds replies for a waiter installed by Register. started
// anchors the round-trip metric at the moment the request began.
func (i *Inbox[K, R]) AwaitRegistered(ctx context.Context, id string, w *Waiter[K, R], started time.Time) (Outcome[K, R], error) {
	ctx, span := i.tracer.Start(ctx, i.spanName, trace.WithAttributes(attribute.String(i.idAttr, id)))
	defer span.End()
	defer func() {
		i.metrics.roundTrip.Record(ctx, time.Since(started).Seconds())
	}()

	// The router only sends raw replies into w.reply; this loop owns all
	// per-request state. It folds one reply per distinct requested lane,
	// refuses unrequested lanes (counted as orphans), and drops duplicate
	// redeliveries, exiting once every lane has answered.
	byLane := make(map[K]R, len(w.lanes))
	for len(byLane) < len(w.lanes) {
		select {
		case <-ctx.Done():
			ctxErr := ctx.Err()
			span.SetStatus(codes.Error, ctxErr.Error())
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return Outcome[K, R]{ByLane: byLane, Complete: false, Deadline: true}, nil
			}
			return Outcome[K, R]{ByLane: byLane, Complete: false, Deadline: false}, fmt.Errorf("await replies: %w", ctxErr)
		case <-i.shutdown:
			span.SetStatus(codes.Error, "reply inbox closed")
			return Outcome[K, R]{ByLane: byLane, Complete: false, Deadline: false}, fmt.Errorf("await replies: reply inbox closed")
		case reply := <-w.reply:
			lane := i.codec.Lane(reply)
			if _, requested := w.lanes[lane]; !requested {
				i.recordOrphan(ctx)
				continue
			}
			if _, duplicate := byLane[lane]; duplicate {
				continue
			}
			byLane[lane] = reply
		}
	}
	return Outcome[K, R]{ByLane: byLane, Complete: true, Deadline: false}, nil
}

func (i *Inbox[K, R]) superviseDrainer(ctx context.Context, run func(context.Context)) {
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
			i.logger.ErrorContext(ctx, "reply drainer panicked; restarting",
				attr.SlogErrorMessage(fmt.Sprintf("%v", recovered)),
				attr.SlogErrorStack(stack),
			)
		} else {
			i.logger.ErrorContext(ctx, "reply drainer exited unexpectedly; restarting",
				attr.SlogError(errors.New("drainer exited while its context was active")),
			)
		}

		if !sleepOrDone(ctx, reconnectBackoff) {
			return
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

// drain polls the inbox with non-blocking LPOPs while requests are in flight
// and sleeps on wake otherwise, so an idle replica issues no Redis commands.
// Ordinary commands keep the client's read timeout and the pool's automatic
// reconnection, so a transient error needs only a paced retry: a timed-out
// pop leaves the elements in the list for the next cycle.
func (i *Inbox[K, R]) drain(ctx context.Context) {
	for ctx.Err() == nil {
		if i.activeWaiters.Load() == 0 {
			select {
			case <-ctx.Done():
				return
			case <-i.wake:
			}
			continue
		}

		batchSize := uint64(0)
		for {
			rest, err := i.client.LPopCount(ctx, i.key, defaultDrainCount).Result()
			if errors.Is(err, redis.Nil) || len(rest) == 0 {
				break
			}
			if err != nil {
				if !sleepOrDone(ctx, reconnectBackoff) {
					return
				}
				break
			}
			if i.drainGate != nil {
				select {
				case <-ctx.Done():
					return
				case <-i.drainGate:
				}
			}
			batchSize += uint64(len(rest))
			for _, raw := range rest {
				i.route(ctx, raw)
			}
		}
		if batchSize > 0 {
			i.drainBatches.Add(1)
			i.drainedReplies.Add(batchSize)
			for previous := i.maxDrainBatch.Load(); batchSize > previous; previous = i.maxDrainBatch.Load() {
				if i.maxDrainBatch.CompareAndSwap(previous, batchSize) {
					break
				}
			}
		}
		if !sleepOrDone(ctx, i.poll) {
			return
		}
	}
}

// sleepOrDone pauses for d and reports false when ctx ended first.
func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (i *Inbox[K, R]) route(ctx context.Context, raw string) {
	reply, err := i.codec.Decode([]byte(raw))
	if err != nil {
		i.logger.WarnContext(ctx, "discard malformed reply", attr.SlogError(err))
		return
	}
	if i.codec.StatusLabel != nil {
		i.metrics.replies.Add(ctx, 1, metric.WithAttributes(attribute.String("status", i.codec.StatusLabel(reply))))
	} else {
		i.metrics.replies.Add(ctx, 1)
	}

	value, ok := i.waiters.Load(i.codec.CorrelationID(reply))
	if !ok {
		// The request already completed or timed out and released its waiter.
		i.recordOrphan(ctx)
		return
	}
	w, ok := value.(*Waiter[K, R])
	if !ok {
		i.recordOrphan(ctx)
		return
	}
	select {
	case w.reply <- reply:
	default:
		// The buffer absorbs the lane count plus duplicate slack; overflow
		// means a redelivery storm and dropping is the safe disposition.
		i.recordOrphan(ctx)
	}
}

func (i *Inbox[K, R]) recordOrphan(ctx context.Context) {
	i.metrics.orphaned.Add(ctx, 1)
	i.orphanedReplies.Add(1)
}

// Close stops the drainer, releases any blocked waiters, and closes the
// dedicated Redis client. The client closes only after the drainer has
// exited, so an in-flight poll cannot race the close; the wait is bounded by
// one poll interval.
func (i *Inbox[K, R]) Close() error {
	i.closeOnce.Do(func() { close(i.shutdown) })
	i.cancel()
	<-i.done
	if err := i.client.Close(); err != nil {
		return fmt.Errorf("close reply inbox redis: %w", err)
	}
	return nil
}
