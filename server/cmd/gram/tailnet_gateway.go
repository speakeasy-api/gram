package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/netingress"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
)

const (
	// tailnetGatewayShutdownTimeout bounds OTel flush and health server shutdown
	// after the gateway has drained its hosts.
	tailnetGatewayShutdownTimeout = 30 * time.Second
)

func newTailnetGatewayCommand() *cli.Command {
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:     "environment",
			Usage:    "Deployment environment: local, dev, prod",
			EnvVars:  []string{"GRAM_ENVIRONMENT"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "database-url",
			Usage:    "Postgres connection string",
			EnvVars:  []string{"GRAM_DATABASE_URL"},
			Required: true,
		},
		&cli.BoolFlag{
			Name:    "unsafe-db-log",
			Usage:   "Log database queries (unsafe outside local development)",
			EnvVars: []string{"GRAM_UNSAFE_DB_LOG"},
		},
		&cli.StringFlag{
			Name:     "encryption-key",
			Usage:    "Application AES encryption key used for auth keys and node state",
			EnvVars:  []string{"GRAM_ENCRYPTION_KEY"},
			Required: true,
		},
		&cli.StringFlag{
			Name:    "ordinal",
			Usage:   "StatefulSet pod index; defaults to the trailing number of the pod name",
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_ORDINAL"},
		},
		&cli.StringFlag{
			Name:    "pod-name",
			Usage:   "Pod name recorded in heartbeats; defaults to the hostname",
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_POD_NAME"},
		},
		&cli.StringFlag{
			Name:    "shard",
			Usage:   "Gateway shard whose ingresses this pod hosts",
			Value:   tailnetgateway.DefaultShard,
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_SHARD"},
		},
		&cli.StringFlag{
			Name:    "state-dir",
			Usage:   "Scratch directory for tsnet; node keys live in Postgres",
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_STATE_DIR"},
		},
		&cli.StringFlag{
			Name:    "tag",
			Usage:   "Tailscale ACL tag advertised by every node",
			Value:   tailnetgateway.DefaultTag,
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_TAG"},
		},
		&cli.StringFlag{
			Name:     "upstream-url",
			Usage:    "Speakeasy private listener URL",
			EnvVars:  []string{"GRAM_TAILNET_GATEWAY_UPSTREAM_URL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "upstream-ca-file",
			Usage:    "PEM CA bundle for the Speakeasy private listener",
			EnvVars:  []string{"GRAM_TAILNET_GATEWAY_UPSTREAM_CA_FILE"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "token-path",
			Usage:    "Projected Kubernetes service-account token path",
			EnvVars:  []string{"GRAM_TAILNET_GATEWAY_TOKEN_PATH"},
			Required: true,
		},
		&cli.DurationFlag{
			Name:    "poll-interval",
			Usage:   "How often desired ingresses are reconciled and hosts heartbeat",
			Value:   tailnetgateway.DefaultPollInterval,
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_POLL_INTERVAL"},
		},
		&cli.DurationFlag{
			Name:    "drain-timeout",
			Usage:   "How long in-flight requests may finish during shutdown",
			Value:   tailnetgateway.DefaultDrainTimeout,
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_DRAIN_TIMEOUT"},
		},
		&cli.DurationFlag{
			Name:    "start-timeout",
			Usage:   "How long one node may take to register and connect",
			Value:   tailnetgateway.DefaultStartTimeout,
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_START_TIMEOUT"},
		},
		&cli.IntFlag{
			Name:    "max-hosts",
			Usage:   "Maximum tailnet nodes hosted by this pod",
			Value:   tailnetgateway.DefaultMaxHosts,
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_MAX_HOSTS"},
		},
		&cli.StringFlag{
			Name:    "control-url",
			Usage:   "Tailscale coordination server override (tests and headscale)",
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_CONTROL_URL"},
		},
		&cli.StringFlag{
			Name:    "control-address",
			Value:   ":8081",
			Usage:   "Pod-local health and readiness address",
			EnvVars: []string{"GRAM_TAILNET_GATEWAY_CONTROL_ADDRESS"},
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
		&cli.PathFlag{
			Name:    "config-file",
			Usage:   "Path to a JSON, TOML, or YAML config file",
			EnvVars: []string{"GRAM_CONFIG_FILE"},
		},
	}
	flags = append(flags, networkIngressQueueFlags()...)
	return &cli.Command{
		Name:  "tailnet-gateway",
		Usage: "Host private network ingresses on customer tailnets with tsnet",
		Flags: flags,
		Before: func(c *cli.Context) error {
			return loadConfigFromFile(c, flags)
		},
		Action: func(c *cli.Context) error {
			serviceName := "gram-tailnet-gateway"
			serviceEnv := c.String("environment")
			appinfo := o11y.PullAppInfo(c.Context)
			appinfo.Command = "tailnet-gateway"
			logger := PullLogger(c.Context).With(
				attr.SlogComponent("tailnet_gateway"),
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)
			slog.SetDefault(logger)
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()
			shutdownOTel, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
				ServiceName: serviceName, ServiceVersion: shortGitSHA(), GitSHA: GitSHA,
				EnableTracing: c.Bool("with-otel-tracing"), EnableMetrics: c.Bool("with-otel-metrics"),
			})
			if err != nil {
				return fmt.Errorf("setup opentelemetry sdk: %w", err)
			}
			defer func() {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), tailnetGatewayShutdownTimeout)
				defer shutdownCancel()
				_ = shutdownOTel(shutdownCtx)
			}()
			meterProvider := otel.GetMeterProvider()

			config, err := tailnetGatewayConfigFromCLI(c)
			if err != nil {
				return err
			}
			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{enableUnsafeLogging: c.Bool("unsafe-db-log")})
			if err != nil {
				return err
			}
			defer db.Close()
			encryptionClient, err := encryption.New(c.String("encryption-key"))
			if err != nil {
				return fmt.Errorf("create encryption client: %w", err)
			}
			gateway, err := tailnetgateway.New(logger, meterProvider, db, encryptionClient, config, &tailnetgateway.TsnetRuntime{Logger: logger})
			if err != nil {
				return fmt.Errorf("configure tailnet gateway: %w", err)
			}

			healthListener, err := net.Listen("tcp", c.String("control-address"))
			if err != nil {
				return fmt.Errorf("listen for tailnet gateway health checks: %w", err)
			}
			defer func() { _ = healthListener.Close() }()
			dbHealth := o11y.NewHealthCheckHandler(nil, []*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}}, nil, nil)
			healthMux := http.NewServeMux()
			healthMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
				if err := gateway.Healthy(); err != nil {
					http.Error(w, "tailnet gateway poll loop stalled", http.StatusServiceUnavailable)
					return
				}
				dbHealth.ServeHTTP(w, r)
			})
			healthMux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			healthMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
				if !gateway.Ready() {
					http.Error(w, "tailnet gateway is not ready", http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(http.StatusOK)
			})
			healthServer := &http.Server{
				Handler: healthMux, ReadHeaderTimeout: 10 * time.Second,
				BaseContext: func(net.Listener) context.Context { return ctx },
			}

			sigctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stop()
			serveErrors := make(chan error, 1)
			go func() { serveErrors <- healthServer.Serve(healthListener) }()
			logger.InfoContext(ctx, "tailnet gateway started",
				attr.SlogServerAddress(healthListener.Addr().String()),
				attr.SlogTailnetGatewaySlot(config.Ordinal))

			runErrors := make(chan error, 1)
			go func() { runErrors <- gateway.Run(sigctx) }()

			var serveErr error
			select {
			case err := <-serveErrors:
				if !errors.Is(err, http.ErrServerClosed) {
					serveErr = fmt.Errorf("serve tailnet gateway health checks: %w", err)
				}
				stop()
				serveErr = errors.Join(serveErr, <-runErrors)
			case err := <-runErrors:
				serveErr = err
			}

			shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), tailnetGatewayShutdownTimeout)
			defer shutdownCancel()
			return errors.Join(serveErr, healthServer.Shutdown(shutdownCtx))
		},
	}
}

func tailnetGatewayConfigFromCLI(c *cli.Context) (tailnetgateway.Config, error) {
	podName := c.String("pod-name")
	if podName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return tailnetgateway.Config{}, fmt.Errorf("resolve pod name: %w", err)
		}
		podName = hostname
	}
	ordinal, err := tailnetgateway.ParsePodOrdinal(c.String("ordinal"), podName)
	if err != nil {
		return tailnetgateway.Config{}, fmt.Errorf("resolve tailnet gateway ordinal: %w", err)
	}
	upstream, err := url.Parse(c.String("upstream-url"))
	if err != nil {
		return tailnetgateway.Config{}, fmt.Errorf("parse private listener upstream: %w", err)
	}
	caPEM, err := os.ReadFile(c.String("upstream-ca-file"))
	if err != nil {
		return tailnetgateway.Config{}, fmt.Errorf("read private listener CA bundle: %w", err)
	}
	transport, err := netingress.NewAttestorTransport(caPEM)
	if err != nil {
		return tailnetgateway.Config{}, fmt.Errorf("configure private listener transport: %w", err)
	}
	stateDir := c.String("state-dir")
	if stateDir == "" {
		stateDir, err = os.MkdirTemp("", "gram-tailnet-gateway-")
		if err != nil {
			return tailnetgateway.Config{}, fmt.Errorf("create tailnet gateway state dir: %w", err)
		}
	}
	config := tailnetgateway.Config{
		Ordinal:            ordinal,
		PodName:            podName,
		Shard:              c.String("shard"),
		StateDir:           stateDir,
		Tag:                c.String("tag"),
		Upstream:           upstream,
		UpstreamTransport:  transport,
		TokenPath:          c.String("token-path"),
		PollInterval:       c.Duration("poll-interval"),
		DrainTimeout:       c.Duration("drain-timeout"),
		StartTimeout:       c.Duration("start-timeout"),
		MaxHosts:           c.Int("max-hosts"),
		ReconcileTaskQueue: c.String(networkIngressQueueFlag),
		ControlURL:         c.String("control-url"),
	}
	if err := config.Validate(); err != nil {
		return tailnetgateway.Config{}, fmt.Errorf("validate tailnet gateway configuration: %w", err)
	}
	return config, nil
}
