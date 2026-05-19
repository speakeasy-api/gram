package mockoidc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sourcegraph/conc/pool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/speakeasy-api/gram/mock-oidc/internal/o11y"
)

var version = "dev"

func NewApp() *cli.App {
	return &cli.App{
		Name:  "mock-oidc",
		Usage: "Mock OIDC provider for local development",
		Flags: []cli.Flag{
			&cli.PathFlag{
				Name:     "config",
				Usage:    "Path to YAML config file",
				EnvVars:  []string{"MOCK_OIDC_CONFIG"},
				Required: true,
			},
			&cli.StringFlag{
				Name:    "address",
				Usage:   "HTTP address to listen on",
				Value:   ":4000",
				EnvVars: []string{"MOCK_OIDC_ADDRESS"},
			},
			&cli.StringFlag{
				Name:    "issuer",
				Usage:   "Issuer URL advertised in discovery and id_token claims",
				Value:   "http://localhost:4000",
				EnvVars: []string{"MOCK_OIDC_ISSUER"},
			},
			&cli.PathFlag{
				Name:    "private-key",
				Usage:   "Path to RSA private key (PEM). If absent, an ephemeral key is generated.",
				EnvVars: []string{"MOCK_OIDC_PRIVATE_KEY"},
			},
			&cli.PathFlag{
				Name:    "tls-cert",
				Usage:   "Path to TLS certificate (PEM). Both --tls-cert and --tls-key are required to enable TLS.",
				EnvVars: []string{"MOCK_OIDC_TLS_CERT"},
			},
			&cli.PathFlag{
				Name:    "tls-key",
				Usage:   "Path to TLS key (PEM). Both --tls-cert and --tls-key are required to enable TLS.",
				EnvVars: []string{"MOCK_OIDC_TLS_KEY"},
			},
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "Log level: debug, info, warn, error",
				Value:   "info",
				EnvVars: []string{"MOCK_OIDC_LOG_LEVEL"},
			},
			&cli.BoolFlag{
				Name:    "log-pretty",
				Usage:   "Pretty-print logs for development",
				Value:   true,
				EnvVars: []string{"MOCK_OIDC_LOG_PRETTY"},
			},
			&cli.BoolFlag{
				Name:    "with-otel-tracing",
				Usage:   "Enable OpenTelemetry trace export over OTLP/gRPC",
				EnvVars: []string{"MOCK_OIDC_ENABLE_OTEL_TRACES"},
			},
			&cli.BoolFlag{
				Name:    "with-otel-metrics",
				Usage:   "Enable OpenTelemetry metric export over OTLP/gRPC",
				EnvVars: []string{"MOCK_OIDC_ENABLE_OTEL_METRICS"},
			},
		},
		Action: run,
	}
}

func run(c *cli.Context) error {
	logger := slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
		RawLevel: c.String("log-level"),
		Pretty:   c.Bool("log-pretty"),
	})).With(slog.String("component", "mock-oidc"))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()

	otelShutdown, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
		ServiceName:    "mock-oidc",
		ServiceVersion: version,
		EnableTracing:  c.Bool("with-otel-tracing"),
		EnableMetrics:  c.Bool("with-otel-metrics"),
	})
	if err != nil {
		return fmt.Errorf("setup otel: %w", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer shutdownCancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			logger.ErrorContext(ctx, "otel shutdown failed", o11y.ErrAttr(err))
		}
	}()

	cfg, err := LoadConfig(c.Path("config"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	privateKey, err := LoadOrGeneratePrivateKey(c.Path("private-key"), logger)
	if err != nil {
		return fmt.Errorf("load private key: %w", err)
	}

	provider, err := NewProvider(cfg, logger, c.String("issuer"), privateKey)
	if err != nil {
		return fmt.Errorf("build provider: %w", err)
	}

	handler := otelhttp.NewHandler(
		NewServer(provider, logger).Handler(),
		"http",
		otelhttp.WithServerName("mock-oidc"),
	)

	srv := &http.Server{
		Addr:              c.String("address"),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	sigctx, sigcancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer sigcancel()

	stopSweeper := make(chan struct{})
	go provider.RunSweeper(stopSweeper)
	defer close(stopSweeper)

	group := pool.New()
	group.Go(func() {
		<-sigctx.Done()
		logger.InfoContext(ctx, "shutting down server")
		graceCtx, graceCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer graceCancel()
		if err := srv.Shutdown(graceCtx); err != nil {
			logger.ErrorContext(ctx, "shutdown failed", o11y.ErrAttr(err))
		}
	})

	tlsCert := c.Path("tls-cert")
	tlsKey := c.Path("tls-key")
	tlsEnabled := tlsCert != "" && tlsKey != ""
	if tlsCert != "" || tlsKey != "" {
		if !tlsEnabled {
			return fmt.Errorf("--tls-cert and --tls-key must both be provided to enable TLS")
		}
	}

	if tlsEnabled {
		logger.InfoContext(ctx, "server started with tls",
			slog.String("address", srv.Addr),
			slog.String("issuer", provider.Issuer()),
		)
		if err := srv.ListenAndServeTLS(tlsCert, tlsKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "server error", o11y.ErrAttr(err))
		}
	} else {
		logger.InfoContext(ctx, "server started",
			slog.String("address", srv.Addr),
			slog.String("issuer", provider.Issuer()),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "server error", o11y.ErrAttr(err))
		}
	}

	cancel()
	group.Wait()
	return nil
}
