package litellm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/litellm/callcache"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const (
	traceProcessorWorkers   = 4
	traceProcessorQueueSize = 16
	tracePersistenceTimeout = 5 * time.Second
)

type otlpJob struct {
	rows []telemetry.LogParams
}

type otlpSignal uint8

const (
	otlpSignalSpans otlpSignal = iota
	otlpSignalMetrics
)

func (s otlpSignal) name() string {
	if s == otlpSignalMetrics {
		return "metrics"
	}
	return "spans"
}

type TraceProcessor struct {
	logger   *slog.Logger
	logBulk  func(context.Context, []telemetry.LogParams) error
	calls    *callcache.Cache
	resolver *InstanceResolver
	signal   otlpSignal
	jobs     chan otlpJob
	stop     chan struct{}
	done     chan struct{}
	workers  int

	mu       sync.Mutex
	started  bool
	stopping bool
	wg       sync.WaitGroup

	accepted           metric.Int64Counter
	dropped            metric.Int64Counter
	persistenceFailed  metric.Int64Counter
	truncatedAttrs     metric.Int64Counter
	invalidIdentifiers metric.Int64Counter
}

func NewTraceProcessor(logger *slog.Logger, meterProvider metric.MeterProvider, telemetryLogger *telemetry.Logger, calls *callcache.Cache) *TraceProcessor {
	processor := newOTLPProcessor(logger, meterProvider, telemetryLogger.LogBulkBounded, traceProcessorWorkers, traceProcessorQueueSize, otlpSignalSpans)
	processor.calls = calls
	return processor
}

type MetricProcessor struct {
	*TraceProcessor
}

func NewMetricProcessor(logger *slog.Logger, meterProvider metric.MeterProvider, telemetryLogger *telemetry.Logger) *MetricProcessor {
	return newMetricProcessor(logger, meterProvider, telemetryLogger.LogBulkBounded, traceProcessorWorkers, traceProcessorQueueSize)
}

func newMetricProcessor(logger *slog.Logger, meterProvider metric.MeterProvider, logBulk func(context.Context, []telemetry.LogParams) error, workers, queueSize int) *MetricProcessor {
	return &MetricProcessor{TraceProcessor: newOTLPProcessor(logger, meterProvider, logBulk, workers, queueSize, otlpSignalMetrics)}
}

type traceAttribution struct {
	record callcache.Record
	found  bool
}

func enrichTraceAttribution(ctx context.Context, logger *slog.Logger, calls *callcache.Cache, spans []telemetry.LogParams) {
	cache := make(map[string]traceAttribution)
	for index := range spans {
		if spans[index].Attributes[attr.EventURNKey] == liteLLMEventURN("unknown") {
			continue
		}
		callID, _ := spans[index].Attributes[attr.LiteLLMCallIDKey].(string)
		if callID == "" {
			continue
		}
		cacheKey := spans[index].ToolInfo.ProjectID + ":" + callID
		attribution, ok := cache[cacheKey]
		if !ok {
			if ctx.Err() != nil {
				return
			}
			projectID, err := uuid.Parse(spans[index].ToolInfo.ProjectID)
			if err != nil {
				continue
			}
			record, err := calls.Get(ctx, projectID, callID)
			attribution = traceAttribution{record: record, found: err == nil}
			cache[cacheKey] = attribution
			if err != nil && !callcache.IsMiss(err) {
				logger.WarnContext(ctx, "read cached LiteLLM trace attribution",
					attr.SlogError(err),
					attr.SlogProjectID(spans[index].ToolInfo.ProjectID),
					attr.SlogLiteLLMCallID(callID),
				)
			}
		}
		if !attribution.found {
			continue
		}
		if attribution.record.UserID != "" || attribution.record.Email != "" {
			spans[index].UserInfo = telemetry.UserInfoByIDAndEmail(attribution.record.UserID, attribution.record.Email)
		}
		if attribution.record.SessionID != "" {
			spans[index].Attributes[attr.GenAIConversationIDKey] = attribution.record.SessionID
		}
		if attribution.record.TraceID != "" {
			spans[index].Attributes[attr.LiteLLMTraceIDKey] = attribution.record.TraceID
		}
	}
}

func newTraceProcessor(logger *slog.Logger, meterProvider metric.MeterProvider, logBulk func(context.Context, []telemetry.LogParams) error, workers, queueSize int) *TraceProcessor {
	return newOTLPProcessor(logger, meterProvider, logBulk, workers, queueSize, otlpSignalSpans)
}

func newOTLPProcessor(logger *slog.Logger, meterProvider metric.MeterProvider, logBulk func(context.Context, []telemetry.LogParams) error, workers, queueSize int, signal otlpSignal) *TraceProcessor {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/litellm")
	signalName := signal.name()
	accepted, _ := meter.Int64Counter("litellm.otel."+signalName+".accepted", metric.WithDescription("LiteLLM OTLP "+signalName+" accepted into the processing queue"))
	dropped, _ := meter.Int64Counter("litellm.otel."+signalName+".dropped", metric.WithDescription("LiteLLM OTLP "+signalName+" dropped because the processing queue was full"))
	persistenceFailed, _ := meter.Int64Counter("litellm.otel."+signalName+".persistence_failed", metric.WithDescription("LiteLLM OTLP "+signalName+" permanently lost because telemetry persistence failed"))
	truncatedAttrs, _ := meter.Int64Counter("litellm.otel.attributes.truncated", metric.WithDescription("LiteLLM OTLP attributes truncated or dropped by ingest limits"))
	invalidIdentifiers, _ := meter.Int64Counter("litellm.otel.identifiers.invalid", metric.WithDescription("Invalid LiteLLM OTLP trace and span identifiers omitted during ingest"))

	return &TraceProcessor{
		logger:             logger.With(attr.SlogComponent("litellm.otel.processor")),
		logBulk:            logBulk,
		calls:              nil,
		resolver:           nil,
		signal:             signal,
		jobs:               make(chan otlpJob, queueSize),
		stop:               make(chan struct{}),
		done:               make(chan struct{}),
		workers:            workers,
		mu:                 sync.Mutex{},
		started:            false,
		stopping:           false,
		wg:                 sync.WaitGroup{},
		accepted:           accepted,
		dropped:            dropped,
		persistenceFailed:  persistenceFailed,
		truncatedAttrs:     truncatedAttrs,
		invalidIdentifiers: invalidIdentifiers,
	}
}

func (p *TraceProcessor) SetInstanceResolver(resolver *InstanceResolver) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolver = resolver
}

func (p *TraceProcessor) Start(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return
	}
	p.started = true
	workerCtx := context.WithoutCancel(ctx)
	for range p.workers {
		p.wg.Add(1)
		go p.run(workerCtx)
	}
}

func (p *TraceProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	if !p.stopping {
		p.stopping = true
		close(p.stop)
		go func() {
			p.wg.Wait()
			close(p.done)
		}()
	}
	done := p.done
	p.mu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("drain LiteLLM OTLP %s processor: %w", p.signal.name(), ctx.Err())
	}
}

func (p *TraceProcessor) Enqueue(ctx context.Context, spans []telemetry.LogParams) bool {
	return p.enqueue(ctx, otlpJob{rows: spans})
}

func (p *MetricProcessor) Enqueue(ctx context.Context, points []telemetry.LogParams) bool {
	return p.enqueue(ctx, otlpJob{rows: points})
}

func (p *TraceProcessor) enqueue(ctx context.Context, job otlpJob) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started || p.stopping {
		p.dropped.Add(ctx, int64(len(job.rows)))
		p.logger.WarnContext(ctx, "LiteLLM OTLP processor unavailable, dropping export",
			attr.SlogEvent("litellm_otel_processor_unavailable"),
		)
		return false
	}

	select {
	case p.jobs <- job:
		p.accepted.Add(ctx, int64(len(job.rows)))
		return true
	default:
		p.dropped.Add(ctx, int64(len(job.rows)))
		p.logger.WarnContext(ctx, "LiteLLM OTLP queue full, dropping export",
			attr.SlogEvent("litellm_otel_queue_full"),
		)
		return false
	}
}

func (p *TraceProcessor) run(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case job := <-p.jobs:
			p.process(ctx, job)
		case <-p.stop:
			for {
				select {
				case job := <-p.jobs:
					p.process(ctx, job)
				default:
					return
				}
			}
		}
	}
}

func (p *TraceProcessor) process(ctx context.Context, job otlpJob) {
	persistenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracePersistenceTimeout)
	defer cancel()
	p.mu.Lock()
	resolver := p.resolver
	p.mu.Unlock()
	if resolver != nil {
		enrichLiteLLMInstanceAttribution(persistenceCtx, resolver, job.rows)
	}
	if p.signal == otlpSignalSpans && p.calls != nil {
		cacheCtx, cacheCancel := context.WithTimeout(persistenceCtx, callCacheTimeout)
		enrichTraceAttribution(cacheCtx, p.logger, p.calls, job.rows)
		cacheCancel()
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			p.persistenceFailed.Add(ctx, int64(len(job.rows)), metric.WithAttributes(attr.Reason("log_bulk_panic")))
			p.logger.ErrorContext(ctx, "persist LiteLLM OTLP export callback panicked",
				attr.SlogEvent("litellm_otel_log_bulk_panic"),
				attr.SlogError(fmt.Errorf("telemetry persistence callback panic: %v", recovered)),
			)
		}
	}()
	if err := p.logBulk(persistenceCtx, job.rows); err != nil {
		p.persistenceFailed.Add(ctx, int64(len(job.rows)), metric.WithAttributes(attr.Reason("log_bulk_error")))
		p.logger.WarnContext(ctx, "persist LiteLLM OTLP export", attr.SlogError(err))
	}
}

type resolvedLiteLLMInstance struct {
	id    string
	found bool
}

func enrichLiteLLMInstanceAttribution(ctx context.Context, resolver *InstanceResolver, rows []telemetry.LogParams) {
	resolveCtx, cancel := context.WithTimeout(ctx, instanceResolverTimeout)
	defer cancel()
	resolved := make(map[string]resolvedLiteLLMInstance)
	for i := range rows {
		if _, ok := rows[i].Attributes[attr.LiteLLMInstanceIDKey]; ok {
			continue
		}
		apiKeyID, _ := rows[i].Attributes[attr.APIKeyIDKey].(string)
		if apiKeyID == "" {
			continue
		}
		organizationID := rows[i].ToolInfo.OrganizationID
		projectID := rows[i].ToolInfo.ProjectID
		cacheKey := instanceResolverCacheKey(organizationID, projectID, apiKeyID)
		instance, ok := resolved[cacheKey]
		if !ok {
			if resolveCtx.Err() != nil {
				continue
			}
			instanceID, found := resolver.Resolve(resolveCtx, organizationID, projectID, apiKeyID)
			instance = resolvedLiteLLMInstance{id: instanceID.String(), found: found}
			resolved[cacheKey] = instance
		}
		if instance.found {
			rows[i].Attributes[attr.LiteLLMInstanceIDKey] = instance.id
		}
	}
}

func enrichAcceptedTelemetryAttribution(ctx context.Context, resolver *InstanceResolver, authCtx *contextvalues.AuthContext, rows []telemetry.LogParams) {
	instanceID, encoded := auth.LiteLLMInstanceIDFromAPIKeyName(authCtx.APIKeyName)
	for i := range rows {
		rows[i].Attributes[attr.APIKeyIDKey] = authCtx.APIKeyID
		if encoded {
			rows[i].Attributes[attr.LiteLLMInstanceIDKey] = instanceID.String()
		}
	}
	if !encoded && auth.IsLiteLLMAPIKeyName(authCtx.APIKeyName) {
		enrichLiteLLMInstanceAttribution(ctx, resolver, rows)
	}
}

func (p *TraceProcessor) recordTruncatedAttributes(ctx context.Context, count int) {
	if count > 0 {
		p.truncatedAttrs.Add(ctx, int64(count))
	}
}

func (p *TraceProcessor) recordInvalidIdentifiers(ctx context.Context, count int) {
	if count > 0 {
		p.invalidIdentifiers.Add(ctx, int64(count))
	}
}
