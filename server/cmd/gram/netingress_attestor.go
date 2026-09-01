package gram

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/netingress"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const attestorShutdownTimeout = 60 * time.Second

func newNetingressAttestorCommand() *cli.Command {
	return &cli.Command{
		Name:  "netingress-attestor",
		Usage: "Run a per-ingress private network attestation proxy",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "address",
				Value:   ":8080",
				Usage:   "HTTP address for private ingress traffic",
				EnvVars: []string{"GRAM_NETINGRESS_ATTESTOR_ADDRESS"},
			},
			&cli.StringFlag{
				Name:    "health-address",
				Value:   ":8081",
				Usage:   "Pod-local HTTP health address",
				EnvVars: []string{"GRAM_NETINGRESS_ATTESTOR_HEALTH_ADDRESS"},
			},
			&cli.StringFlag{
				Name:     "upstream-url",
				Usage:    "Gram private listener URL",
				EnvVars:  []string{"GRAM_NETINGRESS_UPSTREAM_URL"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "upstream-ca-file",
				Usage:    "PEM CA bundle for the Gram private listener",
				EnvVars:  []string{"GRAM_NETINGRESS_UPSTREAM_CA_FILE"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "expected-host",
				Usage:    "Private ingress FQDN accepted from Tailscale",
				EnvVars:  []string{"GRAM_NETINGRESS_EXPECTED_HOST"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "token-path",
				Usage:    "Projected Kubernetes service-account token path",
				EnvVars:  []string{"GRAM_NETINGRESS_TOKEN_PATH"},
				Required: true,
			},
			&cli.BoolFlag{
				Name:    "with-otel-tracing",
				Usage:   "Enable OpenTelemetry traces",
				EnvVars: []string{"GRAM_ENABLE_OTEL_TRACES"},
			},
			&cli.BoolFlag{
				Name:    "with-otel-metrics",
				Usage:   "Enable OpenTelemetry metrics",
				EnvVars: []string{"GRAM_ENABLE_OTEL_METRICS"},
			},
		},
		Action: func(c *cli.Context) error {
			serviceName := "gram-netingress-attestor"
			logger := PullLogger(c.Context).With(
				attr.SlogComponent("netingress_attestor"),
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
			)
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()
			shutdownOTel, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
				ServiceName:    serviceName,
				ServiceVersion: shortGitSHA(),
				GitSHA:         GitSHA,
				EnableTracing:  c.Bool("with-otel-tracing"),
				EnableMetrics:  c.Bool("with-otel-metrics"),
			})
			if err != nil {
				return fmt.Errorf("setup opentelemetry sdk: %w", err)
			}
			defer func() {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), attestorShutdownTimeout)
				defer shutdownCancel()
				_ = shutdownOTel(shutdownCtx)
			}()
			telemetry := netingress.NewTelemetry(logger, otel.GetMeterProvider())
			upstream, err := url.Parse(c.String("upstream-url"))
			if err != nil {
				return fmt.Errorf("parse private listener upstream: %w", err)
			}
			caPEM, err := os.ReadFile(c.String("upstream-ca-file"))
			if err != nil {
				return fmt.Errorf("read private listener CA bundle: %w", err)
			}
			transport, err := netingress.NewAttestorTransport(caPEM)
			if err != nil {
				return fmt.Errorf("configure private listener transport: %w", err)
			}
			handler, err := netingress.NewAttestorHandler(netingress.AttestorConfig{
				Upstream:     upstream,
				ExpectedHost: c.String("expected-host"),
				TokenPath:    c.String("token-path"),
				Transport:    transport,
				Logger:       logger,
				Telemetry:    telemetry,
			})
			if err != nil {
				return fmt.Errorf("configure private ingress attestor: %w", err)
			}

			trafficListener, err := net.Listen("tcp", c.String("address"))
			if err != nil {
				return fmt.Errorf("listen for private ingress traffic: %w", err)
			}
			defer func() { _ = trafficListener.Close() }()
			healthListener, err := net.Listen("tcp", c.String("health-address"))
			if err != nil {
				return fmt.Errorf("listen for private ingress health checks: %w", err)
			}
			defer func() { _ = healthListener.Close() }()

			healthMux := http.NewServeMux()
			healthMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			trafficServer := newAttestorHTTPServer(c.Context, handler)
			healthServer := newAttestorHTTPServer(c.Context, healthMux)

			sigctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer stop()
			serverErrors := make(chan error, 2)
			go func() { serverErrors <- trafficServer.Serve(trafficListener) }()
			go func() { serverErrors <- healthServer.Serve(healthListener) }()
			logger.InfoContext(c.Context, "private ingress attestor started",
				attr.SlogServerAddress(trafficListener.Addr().String()))

			var serveErr error
			select {
			case <-sigctx.Done():
			case err := <-serverErrors:
				if !errors.Is(err, http.ErrServerClosed) {
					serveErr = fmt.Errorf("serve private ingress attestor: %w", err)
				}
			}

			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Context), attestorShutdownTimeout)
			defer cancel()
			return errors.Join(serveErr, trafficServer.Shutdown(shutdownCtx), healthServer.Shutdown(shutdownCtx))
		},
	}
}

func newAttestorHTTPServer(ctx context.Context, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       620 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
}
