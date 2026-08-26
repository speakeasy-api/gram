// Package replyinbox routes enforcement replies from a replica-scoped Redis
// list to in-process scan waiters.
package replyinbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
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
	// DefaultPollInterval paces LPOP polling while scans are in flight. It
	// bounds the added reply pickup latency and is noise against the
	// enforcement deadline; with no waiters registered the drainer does not
	// poll at all.
	DefaultPollInterval = 25 * time.Millisecond
	defaultReplyTTL     = 60 * time.Second
	defaultDrainCount   = 128
	defaultPoolSize     = 2
	reconnectBackoff    = 100 * time.Millisecond
	// duplicateReplySlack sizes waiter buffers beyond the lane count so
	// redelivered duplicates cannot displace a distinct lane's reply.
	duplicateReplySlack = 8
)

var ErrDuplicateWaiter = errors.New("enforcement reply waiter already registered")

// Config controls a replica's dedicated reply-drain Redis client.
type Config struct {
	// RedisOptions are copied before reply-inbox settings are applied.
	RedisOptions redis.Options

	// ReplicaID identifies the process in reply URNs and Redis inbox keys.
	ReplicaID string

	// PollInterval paces non-blocking LPOP polling while waiters are
	// registered. Polling instead of BLPOP keeps ordinary read-timeout and
	// pool-reconnect semantics (a timed-out poll leaves the element in the
	// list, unlike a blocking pop racing its socket deadline) at the cost of
	// up to one interval of reply pickup latency.
	PollInterval time.Duration

	// DrainGate blocks routing after a drained batch until the channel
	// closes. It is nil in production and supports controlled backlog tests
	// without stopping Redis.
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
	client    *redis.Client
	replicaID string
	key       string
	poll      time.Duration
	drainGate <-chan struct{}

	// activeWaiters gates polling: with no scans in flight the drainer sleeps
	// on wake instead of issuing idle LPOPs.
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

	// scan id -> *waiter. Load/store churn is per scan, not per reply, and
	// the router only ever Loads, which is sync.Map's optimized case.
	waiters sync.Map
}

// waiter follows the net/rpc client shape: the router only looks a waiter up
// and sends into its buffered channel; folding, lane refusal, and duplicate
// dropping all happen on the awaiting goroutine.
type waiter struct {
	lanes map[Lane]struct{}
	reply chan *riskv1.EnforcementReply
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
		client:          client,
		replicaID:       cfg.ReplicaID,
		key:             InboxKey(cfg.ReplicaID),
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
func (i *Inbox) Snapshot() Stats {
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

var replicaIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validReplicaID(value string) bool {
	return replicaIDPattern.MatchString(value)
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
	// Buffered past the lane count so at-least-once redeliveries cannot crowd
	// a distinct lane's reply out of the buffer; the router's send never
	// blocks, and overflow beyond the slack is dropped and counted.
	w := &waiter{
		lanes: requested,
		reply: make(chan *riskv1.EnforcementReply, len(lanes)+duplicateReplySlack),
	}
	if _, exists := i.waiters.LoadOrStore(scanID, w); exists {
		return nil, nil, fmt.Errorf("scan %s: %w", scanID, ErrDuplicateWaiter)
	}
	i.activeWaiters.Add(1)
	// Nudge the drainer out of its idle sleep; the buffer coalesces nudges.
	select {
	case i.wake <- struct{}{}:
	default:
	}
	release := func() {
		if _, loaded := i.waiters.LoadAndDelete(scanID); loaded {
			i.activeWaiters.Add(-1)
		}
	}
	return w, release, nil
}

func (i *Inbox) awaitRegistered(ctx context.Context, scanID string, w *waiter, started time.Time) (Outcome, error) {
	ctx, span := i.tracer.Start(ctx, "risk.enforcement.await", trace.WithAttributes(attribute.String("risk.enforcement.scan_id", scanID)))
	defer span.End()
	defer func() {
		i.metrics.roundTrip.Record(ctx, time.Since(started).Seconds())
	}()

	// The router only sends raw replies into w.reply; this loop owns all
	// per-scan state. It folds one reply per distinct requested lane,
	// refuses unrequested lanes (counted as orphans), and drops duplicate
	// redeliveries, exiting once every lane has answered.
	byLane := make(map[Lane]*riskv1.EnforcementReply, len(w.lanes))
	for len(byLane) < len(w.lanes) {
		select {
		case <-ctx.Done():
			ctxErr := ctx.Err()
			span.SetStatus(codes.Error, ctxErr.Error())
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return Outcome{ByLane: byLane, Complete: false, Deadline: true}, nil
			}
			return Outcome{ByLane: byLane, Complete: false, Deadline: false}, fmt.Errorf("await enforcement replies: %w", ctxErr)
		case <-i.shutdown:
			span.SetStatus(codes.Error, "reply inbox closed")
			return Outcome{ByLane: byLane, Complete: false, Deadline: false}, fmt.Errorf("await enforcement replies: reply inbox closed")
		case reply := <-w.reply:
			lane := Lane{Scanner: reply.GetScanner(), PolicyID: reply.GetPolicyId()}
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
	return Outcome{ByLane: byLane, Complete: true, Deadline: false}, nil
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

// drain polls the inbox with non-blocking LPOPs while scans are in flight and
// sleeps on wake otherwise, so an idle replica issues no Redis commands.
// Ordinary commands keep the client's read timeout and the pool's automatic
// reconnection, so a transient error needs only a paced retry: a timed-out
// pop leaves the elements in the list for the next cycle.
func (i *Inbox) drain(ctx context.Context) {
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

func (i *Inbox) route(ctx context.Context, raw string) {
	reply := new(riskv1.EnforcementReply)
	if err := proto.Unmarshal([]byte(raw), reply); err != nil {
		i.logger.WarnContext(ctx, "discard malformed enforcement reply", attr.SlogError(err))
		return
	}
	i.metrics.replies.Add(ctx, 1, metric.WithAttributes(attribute.String("status", statusLabel(reply.GetStatus()))))

	value, ok := i.waiters.Load(reply.GetScanId())
	if !ok {
		// The scan already completed or timed out and released its waiter.
		i.recordOrphan(ctx)
		return
	}
	w, ok := value.(*waiter)
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

func (i *Inbox) recordOrphan(ctx context.Context) {
	i.metrics.orphaned.Add(ctx, 1)
	i.orphanedReplies.Add(1)
}

func statusLabel(status riskv1.EnforcementStatus) string {
	return strings.ToLower(strings.TrimPrefix(status.String(), "ENFORCEMENT_STATUS_"))
}

// Close stops the drainer, releases any blocked waiters, and closes the
// dedicated Redis client. The client closes only after the drainer has
// exited, so it cannot race a replaceClient swap; the wait is bounded by the
// BLPOP block timeout.
func (i *Inbox) Close() error {
	i.closeOnce.Do(func() { close(i.shutdown) })
	i.cancel()
	<-i.done
	if err := i.client.Close(); err != nil {
		return fmt.Errorf("close reply inbox redis: %w", err)
	}
	return nil
}
