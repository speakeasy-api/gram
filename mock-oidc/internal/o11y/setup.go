package o11y

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"goa.design/clue/clue"
)

type SetupOTelSDKOptions struct {
	ServiceName    string
	ServiceVersion string
	EnableTracing  bool
	EnableMetrics  bool
}

// SetupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func SetupOTelSDK(ctx context.Context, logger *slog.Logger, options SetupOTelSDKOptions) (shutdown func(context.Context) error, err error) {
	logger = logger.With(slog.String("component", "otel"))
	var shutdownFuncs []func(context.Context) error

	var metricExporter sdkmetric.Exporter
	var spanExporter sdktrace.SpanExporter

	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	if options.EnableMetrics {
		logger.InfoContext(ctx, "otel metrics enabled")

		// Emit delta aggregation temporality for monotonic sums and histograms.
		// We ship to a per-node Datadog Agent OTLP receiver; the default
		// cumulative temporality forces the Agent to do a stateful
		// cumulative-to-delta conversion that is fragile in our horizontally
		// scaled, pod-churning topology and corrupts counter values. Emitting
		// delta at the SDK makes each pod self-contained and the Agent a
		// pass-through. See Datadog's "Producing Delta Temporality Metrics with
		// OpenTelemetry" guide.
		//
		// NOTE: this overrides OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE.
		// The exporter applies env config before explicit options, so this
		// selector wins regardless of that env var.
		exp, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithTemporalitySelector(instrumentKindTemporalitySelector),
		)
		if err != nil {
			handleErr(err)
			return nil, err
		}
		shutdownFuncs = append(shutdownFuncs, exp.Shutdown)
		metricExporter = exp
	} else {
		logger.InfoContext(ctx, "otel metrics disabled")
	}

	if options.EnableTracing {
		logger.InfoContext(ctx, "otel tracing enabled")

		exp, err := otlptracegrpc.New(ctx)
		if err != nil {
			handleErr(err)
			return nil, err
		}
		shutdownFuncs = append(shutdownFuncs, exp.Shutdown)
		spanExporter = exp
	} else {
		logger.InfoContext(ctx, "otel tracing disabled")
	}

	cfg, err := newClueConfig(
		ctx,
		options.ServiceName,
		options.ServiceVersion,
		metricExporter,
		spanExporter,
		prop,
		clue.AdaptiveSampler(2, 10),
		otel.ErrorHandlerFunc(func(err error) {
			logger.ErrorContext(ctx, "otel error", ErrAttr(err))
		}),
	)
	if err != nil {
		handleErr(err)
		return
	}

	otellogger := logr.FromSlogHandler(logger.Handler())
	clue.ConfigureOpenTelemetry(ctx, cfg)
	otel.SetLogger(otellogger)

	err = runtime.Start()
	if err != nil {
		handleErr(err)
		return
	}

	return
}

// instrumentKindTemporalitySelector reports delta aggregation temporality for every
// instrument kind except UpDownCounters, which stay cumulative because delta is
// meaningless for a non-monotonic running value. This matches Datadog's
// recommended selector for OTLP metrics.
func instrumentKindTemporalitySelector(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	switch kind {
	case sdkmetric.InstrumentKindUpDownCounter, sdkmetric.InstrumentKindObservableUpDownCounter:
		return metricdata.CumulativeTemporality
	default:
		return metricdata.DeltaTemporality
	}
}

func newClueConfig(
	ctx context.Context,
	svcName string,
	svcVersion string,
	metricExporter sdkmetric.Exporter,
	spanExporter sdktrace.SpanExporter,
	propagators propagation.TextMapPropagator,
	sampler sdktrace.Sampler,
	errorHandler otel.ErrorHandler,
) (*clue.Config, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(svcName),
			semconv.ServiceVersionKey.String(svcVersion),
		))
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}
	var meterProvider metric.MeterProvider
	if metricExporter == nil {
		meterProvider = metricnoop.NewMeterProvider()
	} else {
		reader := sdkmetric.NewPeriodicReader(
			metricExporter,
			sdkmetric.WithInterval(60*time.Second),
		)
		meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(reader),
		)
	}
	var tracerProvider trace.TracerProvider
	if spanExporter == nil {
		tracerProvider = tracenoop.NewTracerProvider()
	} else {
		sampler := sdktrace.ParentBased(sampler)
		tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sampler),
			sdktrace.WithBatcher(spanExporter),
		)
	}
	return &clue.Config{
		MeterProvider:  meterProvider,
		TracerProvider: tracerProvider,
		Propagators:    propagators,
		ErrorHandler:   errorHandler,
	}, nil
}
