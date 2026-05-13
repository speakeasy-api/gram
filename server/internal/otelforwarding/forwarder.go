package otelforwarding

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	defaultWorkers   = 32
	defaultQueueSize = 1024
	defaultTimeout   = 10 * time.Second
)

// Job is a single OTEL payload queued for forwarding to a customer endpoint.
// Body is the raw bytes received on /rpc/hooks.otel/v1/*, forwarded verbatim
// (raw passthrough).
type Job struct {
	OrgID       string
	URL         string
	ContentType string
	Headers     map[string]string
	Body        []byte
}

// Forwarder is a bounded async worker pool. Jobs that don't fit in the queue
// are dropped with a logged warning — the OTEL ingest path must not block
// because a customer's downstream endpoint is slow.
type Forwarder struct {
	logger  *slog.Logger
	tracer  trace.Tracer
	client  *guardian.HTTPClient
	jobs    chan Job
	stop    chan struct{}
	wg      sync.WaitGroup
	started bool
	mu      sync.Mutex

	dropped metric.Int64Counter
	sent    metric.Int64Counter
	failed  metric.Int64Counter
}

func NewForwarder(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, policy *guardian.Policy) *Forwarder {
	logger = logger.With(attr.SlogComponent("otelforwarding.forwarder"))

	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/otelforwarding")
	dropped, _ := meter.Int64Counter("otelforwarding.dropped", metric.WithDescription("OTEL forward jobs dropped due to a full queue"))
	sent, _ := meter.Int64Counter("otelforwarding.sent", metric.WithDescription("OTEL forward jobs successfully delivered"))
	failed, _ := meter.Int64Counter("otelforwarding.failed", metric.WithDescription("OTEL forward jobs that failed to deliver"))

	return &Forwarder{
		logger:  logger,
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/otelforwarding"),
		client:  policy.PooledClient(),
		jobs:    make(chan Job, defaultQueueSize),
		stop:    make(chan struct{}),
		wg:      sync.WaitGroup{},
		started: false,
		mu:      sync.Mutex{},
		dropped: dropped,
		sent:    sent,
		failed:  failed,
	}
}

// Start spawns the worker goroutines. Safe to call once.
func (f *Forwarder) Start(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.started {
		return
	}
	f.started = true

	for i := 0; i < defaultWorkers; i++ {
		f.wg.Add(1)
		go f.run(ctx)
	}
}

// Shutdown stops accepting new jobs and waits for in-flight workers to
// drain. Jobs already in the queue are sent best-effort before exit.
func (f *Forwarder) Shutdown(ctx context.Context) {
	f.mu.Lock()
	if !f.started {
		f.mu.Unlock()
		return
	}
	close(f.stop)
	f.mu.Unlock()

	done := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Enqueue queues a forward job. If the queue is full, the job is dropped and
// a metric is incremented — we deliberately do not block the OTEL ingest
// path on slow customer endpoints.
func (f *Forwarder) Enqueue(ctx context.Context, job Job) {
	select {
	case f.jobs <- job:
	default:
		f.dropped.Add(ctx, 1)
		f.logger.WarnContext(ctx, "otel forward queue full, dropping job",
			attr.SlogOrganizationID(job.OrgID),
		)
	}
}

func (f *Forwarder) run(ctx context.Context) {
	defer f.wg.Done()
	for {
		select {
		case job := <-f.jobs:
			f.send(ctx, job)
		case <-f.stop:
			// Drain whatever's already in the queue before exiting so a
			// graceful shutdown doesn't lose buffered traffic.
			for {
				select {
				case job := <-f.jobs:
					f.send(ctx, job)
				default:
					return
				}
			}
		}
	}
}

func (f *Forwarder) send(ctx context.Context, job Job) {
	sendCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	sendCtx, span := f.tracer.Start(sendCtx, "otelforwarding.send")
	defer span.End()

	req, err := http.NewRequestWithContext(sendCtx, http.MethodPost, job.URL, bytes.NewReader(job.Body))
	if err != nil {
		f.failed.Add(ctx, 1)
		f.logger.WarnContext(sendCtx, "build otel forward request",
			attr.SlogError(err),
			attr.SlogOrganizationID(job.OrgID),
		)
		return
	}
	if job.ContentType != "" {
		req.Header.Set("Content-Type", job.ContentType)
	}
	for k, v := range job.Headers {
		req.Header.Set(k, v)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		f.failed.Add(ctx, 1)
		f.logger.WarnContext(sendCtx, "otel forward request failed",
			attr.SlogError(err),
			attr.SlogOrganizationID(job.OrgID),
		)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		f.failed.Add(ctx, 1)
		f.logger.WarnContext(sendCtx, "otel forward returned error status",
			attr.SlogHTTPResponseStatusCode(resp.StatusCode),
			attr.SlogOrganizationID(job.OrgID),
		)
		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	f.sent.Add(ctx, 1)
}

// ErrQueueFull is exported so tests can distinguish drop-due-to-backpressure
// from other failure modes.
var ErrQueueFull = errors.New("otel forward queue full")
