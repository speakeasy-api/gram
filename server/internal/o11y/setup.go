package o11y

import (
	"context"
	"errors"
	"log/slog"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"goa.design/clue/clue"
)

type SetupOTelSDKOptions struct {
	EnableTracing bool
	EnableMetrics bool
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

	appInfo := PullAppInfo(ctx)

	cfg, err := clue.NewConfig(
		ctx,
		"gram",
		appInfo.GitSHA,
		metricExporter,
		spanExporter,
		clue.WithPropagators(prop),
		clue.WithErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			logger.ErrorContext(ctx, "otel error", slog.String("error", err.Error()))
		})),
		clue.WithMaxSamplingRate(2),
		clue.WithSampleSize(10),
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
