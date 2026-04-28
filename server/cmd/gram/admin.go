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
	"github.com/redis/go-redis/v9"
	"github.com/sourcegraph/conc/pool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

func newAdminCommand() *cli.Command {
	var shutdownFuncs []func(context.Context) error

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "address",
			Value:   ":8084",
			Usage:   "HTTP address to listen on",
			EnvVars: []string{"GRAM_ADMIN_SERVER_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "control-address",
			Value:   ":8085",
			Usage:   "HTTP address to listen on",
			EnvVars: []string{"GRAM_ADMIN_CONTROL_ADDRESS"},
		},
		&cli.StringFlag{
			Name:     "admin-server-url",
			Usage:    "The URL of the admin server",
			EnvVars:  []string{"GRAM_ADMIN_SERVER_URL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "environment",
			Usage:    "The current server environment", // local, dev, prod
			Required: true,
			EnvVars:  []string{"GRAM_ENVIRONMENT"},
		},
		&cli.StringFlag{
			Name:     "ssl-key-file",
			Usage:    "The SSL key file path to use for the server",
			Required: false,
			EnvVars:  []string{"GRAM_SSL_KEY_FILE"},
		},
		&cli.StringFlag{
			Name:     "ssl-cert-file",
			Usage:    "The SSL certifate file path to use for the server",
			Required: false,
			EnvVars:  []string{"GRAM_SSL_CERT_FILE"},
		},
		&cli.StringFlag{
			Name:     "site-url",
			Usage:    "The URL of the site",
			EnvVars:  []string{"GRAM_SITE_URL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "database-url",
			Usage:    "Database URL",
			EnvVars:  []string{"GRAM_DATABASE_URL"},
			Required: true,
		},
		&cli.BoolFlag{
			Name:    "unsafe-db-log",
			Usage:   "Turn on unsafe database logging. WARNING: This will log all database queries and data to the console.",
			EnvVars: []string{"GRAM_UNSAFE_DB_LOG"},
			Value:   false,
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
		&cli.StringFlag{
			Name:    "redis-cache-addr",
			Usage:   "Address of the redis cache server",
			EnvVars: []string{"GRAM_REDIS_CACHE_ADDR"},
		},
		&cli.StringFlag{
			Name:    "redis-cache-password",
			Usage:   "Password for the redis cache server",
			EnvVars: []string{"GRAM_REDIS_CACHE_PASSWORD"},
		},
		&cli.StringFlag{
			Name:     "admin-encryption-key",
			Usage:    "Key for App level AES encryption/decyryption",
			Required: true,
			EnvVars:  []string{"GRAM_ADMIN_ENCRYPTION_KEY"},
		},
		&cli.StringFlag{
			Name:    "admin-oidc-client-id",
			Usage:   "OAuth 2.0 client ID for the admin login flow",
			EnvVars: []string{"GRAM_ADMIN_OIDC_CLIENT_ID"},
		},
		&cli.StringFlag{
			Name:    "admin-oidc-client-secret",
			Usage:   "OAuth 2.0 client secret for the admin login flow",
			EnvVars: []string{"GRAM_ADMIN_OIDC_CLIENT_SECRET"},
		},
		&cli.StringSliceFlag{
			Name:    "admin-allowed-hds",
			Usage:   "Comma-separated Google Workspace hosted domains allowed to authenticate against the admin service",
			Value:   cli.NewStringSlice("speakeasyapi.dev", "speakeasy.com"),
			EnvVars: []string{"GRAM_ADMIN_ALLOWED_HDS"},
		},
		&cli.StringFlag{
			Name:    "admin-oidc-emulator-url",
			Usage:   "Base URL for the OAuth 2.0 and OIDC emulator",
			EnvVars: []string{"GRAM_ADMIN_OIDC_EMULATOR_URL"},
		},
		&cli.StringSliceFlag{
			Name:     "disallowed-cidr-blocks",
			Usage:    "List of CIDR blocks to block for SSRF protection",
			EnvVars:  []string{"GRAM_DISALLOWED_CIDR_BLOCKS"},
			Required: false,
		},
		&cli.PathFlag{
			Name:     "config-file",
			Usage:    "Path to a config file to load. Supported formats are JSON, TOML and YAML.",
			EnvVars:  []string{"GRAM_CONFIG_FILE"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "workos-api-key",
			Usage:    "WorkOS API key for user identity lookups",
			EnvVars:  []string{"WORKOS_API_KEY"},
			Required: false,
		},
	}

	return &cli.Command{
		Name:  "admin",
		Usage: "Start the Gram admin server",
		Flags: flags,
		Action: func(c *cli.Context) error {
			serviceName := "gram-admin"
			serviceEnv := c.String("environment")
			appinfo := o11y.PullAppInfo(c.Context)
			appinfo.Command = "admin"
			logger := PullLogger(c.Context).With(
				attr.SlogComponent("admin"),
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)

			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

			shutdown, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
				ServiceName:    serviceName,
				ServiceVersion: shortGitSHA(),
				GitSHA:         GitSHA,
				EnableTracing:  c.Bool("with-otel-tracing"),
				EnableMetrics:  c.Bool("with-otel-metrics"),
			})
			if err != nil {
				return fmt.Errorf("setup opentelemetry sdk: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			tracerProvider := otel.GetTracerProvider()
			meterProvider := otel.GetMeterProvider()
			slog.SetDefault(logger)

			guardianPolicy, err := newGuardianPolicy(c, tracerProvider)
			if err != nil {
				return err
			}

			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			// Ping the database to ensure connectivity
			if err := db.Ping(ctx); err != nil {
				logger.ErrorContext(ctx, "failed to ping database", attr.SlogError(err))
				return fmt.Errorf("database ping failed: %w", err)
			}
			defer db.Close()

			err = o11y.StartObservers(meterProvider, db)
			if err != nil {
				return fmt.Errorf("failed to create observers: %w", err)
			}

			redisClient, err := newRedisClient(ctx, redisClientOptions{
				redisAddr:     c.String("redis-cache-addr"),
				redisPassword: c.String("redis-cache-password"),
			})
			if err != nil {
				return fmt.Errorf("failed to connect to redis: %w", err)
			}

			adminEncryption, err := encryption.New(c.String("admin-encryption-key"))
			if err != nil {
				return fmt.Errorf("failed to create admin encryption client: %w", err)
			}

			adminServerURL, err := url.Parse(c.String("admin-server-url"))
			if err != nil {
				return fmt.Errorf("failed to parse admin server url: %w", err)
			}

			adminOIDCClient, err := newAdminOIDCClient(ctx, c, tracerProvider, guardianPolicy, adminServerURL)
			if err != nil {
				return fmt.Errorf("failed to create admin OIDC client: %w", err)
			}

			mux := goahttp.NewMuxer()
			mux.Use(func(h http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
						w.WriteHeader(http.StatusOK)
						return
					}

					h.ServeHTTP(w, r)
				})
			})
			mux.Use(func(h http.Handler) http.Handler {
				return otelhttp.NewHandler(h, "http", otelhttp.WithServerName("gram"))
			})
			mux.Use(middleware.RouteLabelerMiddleware)
			mux.Use(middleware.NewHTTPLoggingMiddleware(logger))
			mux.Use(middleware.NewRecovery(logger))
			mux.Use(admin.SessionMiddleware)

			admin.Attach(mux, admin.NewService(logger, tracerProvider, db, redisClient, adminOIDCClient, adminEncryption))

			srv := &http.Server{
				Addr:              c.String("address"),
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
				BaseContext: func(net.Listener) context.Context {
					return ctx
				},
			}

			sigctx, sigcancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer sigcancel()

			group := pool.New()

			group.Go(func() {
				<-sigctx.Done()

				logger.InfoContext(ctx, "shutting down server")

				graceCtx, graceCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer graceCancel()

				if err := srv.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "failed to shutdown development server", attr.SlogError(err))
				}
			})

			tlsEnabled := c.String("ssl-key-file") != "" && c.String("ssl-cert-file") != ""

			{
				controlServer := control.Server{
					Address:          c.String("control-address"),
					Logger:           logger.With(attr.SlogComponent("control")),
					DisableProfiling: false,
				}

				listenAddr := srv.Addr
				if listenAddr == "" {
					listenAddr = c.String("address")
				}
				host, port, _ := net.SplitHostPort(listenAddr)
				if host == "" {
					host = "localhost"
				}
				healthzEndpoint := &o11y.HTTPEndpoint{
					URL: &url.URL{
						Scheme: conv.Ternary(tlsEnabled, "https", "http"),
						Host:   net.JoinHostPort(host, port),
						Path:   "/healthz",
					},
					TLSCertificate: nil,
				}
				if tlsEnabled {
					cert, err := os.ReadFile(c.String("ssl-cert-file"))
					if err != nil {
						return fmt.Errorf("failed to read TLS certificate for health check: %w", err)
					}
					healthzEndpoint.TLSCertificate = cert
				}
				shutdown, err := controlServer.Start(c.Context, o11y.NewHealthCheckHandler(
					[]*o11y.NamedResource[*o11y.HTTPEndpoint]{{Name: "api", Resource: healthzEndpoint}},
					[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}},
					[]*o11y.NamedResource[*redis.Client]{{Name: "default", Resource: redisClient}},
					[]*o11y.NamedResource[client.Client]{},
				))
				if err != nil {
					return fmt.Errorf("failed to start control server: %w", err)
				}
				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			if tlsEnabled {
				logger.InfoContext(ctx, "server started with tls", attr.SlogServerAddress(c.String("address")))
				if err := srv.ListenAndServeTLS(c.String("ssl-cert-file"), c.String("ssl-key-file")); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.ErrorContext(ctx, "server error", attr.SlogError(err))
				}
			} else {
				logger.InfoContext(ctx, "server started", attr.SlogServerAddress(c.String("address")))
				if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.ErrorContext(ctx, "server error", attr.SlogError(err))
				}
			}

			cancel()
			group.Wait()

			return nil
		},
		Before: func(ctx *cli.Context) error {
			return loadConfigFromFile(ctx, flags)
		},
		After: func(c *cli.Context) error {
			return runShutdown(PullLogger(c.Context), c.Context, shutdownFuncs)
		},
	}
}
