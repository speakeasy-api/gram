package o11y

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	braintrust "github.com/braintrustdata/braintrust-sdk-go"
	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"goa.design/clue/clue"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type SetupOTelSDKOptions struct {
	ServiceName       string
	ServiceVersion    string
	GitSHA            string
	EnableTracing     bool
	EnableMetrics     bool
	BraintrustProject string
	BraintrustAPIKey  string
}

// SetupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func SetupOTelSDK(ctx context.Context, logger *slog.Logger, options SetupOTelSDKOptions) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	var metricExporter sdkmetric.Exporter
	var spanExporter sdktrace.SpanExporter

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	if options.EnableMetrics {
		logger.InfoContext(ctx, "otel metrics enabled")

		exp, err := otlpmetricgrpc.New(ctx)
		if err != nil {
			handleErr(err)
			return nil, err
		}
		shutdownFuncs = append(shutdownFuncs, exp.Shutdown)
		metricExporter = exp
	} else {
		logger.InfoContext(ctx, "otel metrics disabled")
		// nil metrics exporter tells clue.NewConfig to use a no-op metrics provider
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
		// nil trace exporter tells clue.NewConfig to use a no-op tracer provider
	}

	cfg, err := newClueConfig(
		options.ServiceName,
		options.ServiceVersion,
		options.GitSHA,
		metricExporter,
		spanExporter,
		prop,
		NewLLMPrioritySampler(clue.AdaptiveSampler(2, 10)),
		otel.ErrorHandlerFunc(func(err error) {
			logger.ErrorContext(ctx, "otel error", attr.SlogError(err))
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

// GetBraintrustTracerProvider creates a TracerProvider that sends spans to Braintrust.
// This allows specific services (like chat) to use Braintrust without affecting the global tracer.
func GetBraintrustTracerProvider(ctx context.Context, logger *slog.Logger, apiKey, projectName string) (trace.TracerProvider, func(context.Context) error, error) {
	if apiKey == "" {
		logger.InfoContext(ctx, "braintrust disabled: API key not set")
		return tracenoop.NewTracerProvider(), func(context.Context) error { return nil }, nil
	}

	if projectName == "" {
		logger.InfoContext(ctx, "braintrust disabled: project not configured")
		return tracenoop.NewTracerProvider(), func(context.Context) error { return nil }, nil
	}

	logger.InfoContext(ctx, "initializing braintrust tracer", attr.SlogProjectName(projectName))

	// Create a simple TracerProvider specifically for Braintrust
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// HACK WARNING
	// Temporarily unset OTEL_EXPORTER_OTLP_ENDPOINT to prevent Braintrust's internal
	// OTLP client from reading it. The Braintrust SDK creates its own OTLP exporter
	// which reads this env var and ignores our WithAPIURL option.
	originalEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if originalEndpoint != "" {
		if err := os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT"); err != nil {
			logger.WarnContext(ctx, "failed to unset OTEL_EXPORTER_OTLP_ENDPOINT", attr.SlogError(err))
		}
	}

	_, err := braintrust.New(
		tp,
		braintrust.WithProject(projectName),
		braintrust.WithAPIKey(apiKey),
	)

	// Restore the original OTLP endpoint
	if originalEndpoint != "" {
		if restoreErr := os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", originalEndpoint); restoreErr != nil {
			logger.WarnContext(ctx, "failed to restore OTEL_EXPORTER_OTLP_ENDPOINT", attr.SlogError(restoreErr))
		}
	}

	if err != nil {
		logger.WarnContext(ctx, "failed to initialize braintrust", attr.SlogError(err))
		return tracenoop.NewTracerProvider(), func(context.Context) error { return nil }, err
	}

	logger.InfoContext(ctx, "braintrust tracer initialized successfully")

	shutdown := func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}

	return tp, shutdown, nil
}

func newClueConfig(
	svcName string,
	svcVersion string,
	gitSHA string,
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
			attr.DataDogGitCommitSHA(gitSHA),
			attr.DataDogGitRepoURL("github.com/speakeasy-api/gram"),
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
	var sdkTP *sdktrace.TracerProvider
	if spanExporter == nil {
		tracerProvider = tracenoop.NewTracerProvider()
	} else {
		sampler := sdktrace.ParentBased(sampler)
		sdkTP = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sampler),
			sdktrace.WithBatcher(spanExporter),
		)
		tracerProvider = sdkTP
	}
	return &clue.Config{
		MeterProvider:  meterProvider,
		TracerProvider: tracerProvider,
		Propagators:    propagators,
		ErrorHandler:   errorHandler,
	}, nil
}
