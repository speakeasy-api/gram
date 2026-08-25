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
	"github.com/urfave/cli/v2/altsrc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

const adminBillingTelemetryEnabledFlag = "admin-billing-telemetry-enabled"

func newAdminStripeClient(
	ctx context.Context,
	logger *slog.Logger,
	guardianPolicy *guardian.Policy,
	c *cli.Context,
) stripeclient.Client {
	client, err := newStripeClient(ctx, logger, guardianPolicy, c)
	if err != nil {
		logger.WarnContext(ctx, "Stripe billing unavailable; continuing without Stripe", attr.SlogError(err))
		return nil
	}
	return client
}

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
			Name:    "temporal-address",
			Usage:   "Address of the Temporal server",
			EnvVars: []string{"TEMPORAL_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "temporal-namespace",
			Usage:   "Namespace of the Temporal server",
			EnvVars: []string{"TEMPORAL_NAMESPACE"},
		},
		&cli.StringFlag{
			Name:    "temporal-task-queue",
			Usage:   "Task queue of the Temporal server",
			EnvVars: []string{"TEMPORAL_TASK_QUEUE"},
			Value:   "main",
		},
		&cli.StringFlag{
			Name:    "temporal-client-cert",
			Usage:   "Client cert of the Temporal server",
			EnvVars: []string{"TEMPORAL_CLIENT_CERT"},
		},
		&cli.StringFlag{
			Name:    "temporal-client-key",
			Usage:   "Client key of the Temporal server",
			EnvVars: []string{"TEMPORAL_CLIENT_KEY"},
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
		&cli.StringSliceFlag{
			Name:    "admin-allowed-origins",
			Usage:   "Comma-separated browser origins permitted to make credentialed cross-origin requests to the admin API (e.g. https://admin.speakeasy.com).",
			EnvVars: []string{"GRAM_ADMIN_ALLOWED_ORIGINS"},
		},
		&cli.BoolFlag{
			Name:    "admin-cross-origin-cookies",
			Usage:   "When true, rewrite admin session cookies to SameSite=None; Secure so browsers send them on cross-origin fetches from the admin web UI. Required whenever the SPA origin differs from the admin API origin (local dev with separate Vite + admin ports, prod with admin.* SPA hitting gram-admin.*).",
			EnvVars: []string{"GRAM_ADMIN_CROSS_ORIGIN_COOKIES"},
		},
		&cli.StringFlag{
			Name:    "admin-cookie-domain",
			Usage:   "Optional Domain attribute applied to admin session cookies when admin-cross-origin-cookies is enabled (e.g. .speakeasy.com). Leave empty for localhost-only setups.",
			EnvVars: []string{"GRAM_ADMIN_COOKIE_DOMAIN"},
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
			Name: "workos-api-key",
			Usage: "WorkOS API key for user identity lookups and organization creation. " +
				"Falls back to the same secret the server and worker read, so a deployment that already sets one does not need a second.",
			EnvVars:  []string{"WORKOS_API_KEY", "GRAM_IDP_CLIENT_SECRET"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "workos-endpoint",
			Usage:    "Base URL for WorkOS API calls. Leave unset for production (defaults to https://api.workos.com); set to the dev-idp's mock-workos mode for fully-local development.",
			EnvVars:  []string{"WORKOS_API_URL"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "idp-client-id",
			Usage:    "OIDC client ID for the identity provider",
			EnvVars:  []string{"GRAM_IDP_CLIENT_ID"},
			Required: false,
		},
		// The server's own flag names and environment variables, so a deployment
		// already running gram-server needs no new secrets. The encryption key
		// is the application-wide one, not admin-encryption-key.
		&cli.StringFlag{
			Name:     "encryption-key",
			Usage:    "Key for App level AES encryption/decryption",
			EnvVars:  []string{"GRAM_ENCRYPTION_KEY"},
			Required: false,
		},
		&cli.StringFlag{
			Name:    "openrouter-provisioning-key",
			Usage:   "Provisioning key for OpenRouter to create new API keys for orgs - https://openrouter.ai/settings/provisioning-keys",
			EnvVars: []string{"OPENROUTER_PROVISIONING_KEY"},
		},
		&cli.StringFlag{
			Name:    "openrouter-dev-key",
			Usage:   "Dev API key for OpenRouter (primarily for local development) - https://openrouter.ai/settings/keys",
			EnvVars: []string{"OPENROUTER_DEV_KEY"},
		},
		&cli.StringFlag{
			Name:     "loops-api-key",
			Usage:    "Loops API key for trial lifecycle contact updates. Empty or 'unset' disables Loops writes.",
			EnvVars:  []string{"LOOPS_API_KEY"},
			Required: false,
		},
		&cli.StringFlag{
			Name: "stripe-api-key", Usage: "The Stripe API key", EnvVars: []string{"STRIPE_API_KEY"},
		},
		&cli.StringFlag{
			Name: "stripe-webhook-secret", Usage: "The Stripe webhook signing secret", EnvVars: []string{"STRIPE_WEBHOOK_SECRET"},
		},
		&cli.BoolFlag{
			Name:    adminBillingTelemetryEnabledFlag,
			Usage:   "Enable PAYG billing telemetry from ClickHouse",
			EnvVars: []string{"GRAM_ADMIN_BILLING_TELEMETRY_ENABLED"},
		},
		altsrc.NewStringFlag(&cli.StringFlag{
			Name: "stripe-price-id-tum", Aliases: []string{"stripe.price_id_tum"}, EnvVars: []string{"STRIPE_PRICE_ID_TUM"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name: "stripe-meter-id-tum", Aliases: []string{"stripe.meter_id_tum"}, EnvVars: []string{"STRIPE_METER_ID_TUM"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name: "stripe-meter-event-name", Aliases: []string{"stripe.meter_event_name"}, EnvVars: []string{"STRIPE_METER_EVENT_NAME"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name: "stripe-portal-configuration-id", Aliases: []string{"stripe.portal_configuration_id"}, EnvVars: []string{"STRIPE_PORTAL_CONFIGURATION_ID"},
		}),
	}
	flags = append(flags, clickHouseFlags()...)

	return &cli.Command{
		Name:  "admin",
		Usage: "Start the Gram admin server",
		Flags: flags,
		Action: func(c *cli.Context) error {
			siteURL, err := url.Parse(c.String("site-url"))
			if err != nil || siteURL.Host == "" || (siteURL.Scheme != "http" && siteURL.Scheme != "https") {
				return fmt.Errorf("invalid site-url: must be an absolute HTTP(S) URL")
			}
			if c.String("environment") == "prod" && siteURL.Scheme != "https" {
				return fmt.Errorf("invalid site-url: HTTPS is required in production")
			}

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

			temporalEnv, temporalShutdown, err := newTemporalClient(logger, meterProvider, temporalClientOptions{
				address:      c.String("temporal-address"),
				namespace:    c.String("temporal-namespace"),
				taskQueue:    c.String("temporal-task-queue"),
				certPEMBlock: []byte(c.String("temporal-client-cert")),
				keyPEMBlock:  []byte(c.String("temporal-client-key")),
			})
			if err != nil {
				return fmt.Errorf("failed to create temporal client: %w", err)
			}
			chatAnalysisSignaler := analysis.Signaler(admin.ChatAnalysisTriggerUnavailable{})
			var openRouterSpendCap admin.OpenRouterSpendCapScheduler
			temporalHealth := []*o11y.NamedResource[client.Client]{}
			if temporalEnv != nil {
				shutdownFuncs = append(shutdownFuncs, temporalShutdown)
				chatAnalysisSignaler = &background.TemporalChatAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger}
				openRouterSpendCap = &background.OpenRouterKeyRefresher{TemporalEnv: temporalEnv}
				temporalHealth = append(temporalHealth, &o11y.NamedResource[client.Client]{Name: "default", Resource: temporalEnv.Client()})
			}

			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer db.Close()

			err = o11y.StartObservers(meterProvider, db)
			if err != nil {
				return fmt.Errorf("failed to create observers: %w", err)
			}

			redisClient, err := newRedisClient(ctx, redisClientOptions{
				redisAddr:     c.String("redis-cache-addr"),
				redisPassword: c.String("redis-cache-password"),
				enableTracing: false,
			})
			if err != nil {
				return fmt.Errorf("failed to connect to redis: %w", err)
			}

			guardianPolicy, err := newGuardianPolicy(c, logger, tracerProvider, meterProvider, redisClient)
			if err != nil {
				return err
			}

			stripeClient := newAdminStripeClient(ctx, logger, guardianPolicy, c)
			var billingTelemetry *telemetryrepo.Queries
			if c.Bool(adminBillingTelemetryEnabledFlag) {
				chDB, chShutdown, err := newClickhouseClient(ctx, logger, c)
				if err != nil {
					logger.WarnContext(ctx, "billing usage telemetry unavailable; continuing without ClickHouse", attr.SlogError(err))
				} else {
					defer o11y.LogDefer(ctx, logger, func() error { return chShutdown(ctx) })
					billingTelemetry = telemetryrepo.New(chDB)
				}
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
			adminAllowedOrigins := c.StringSlice("admin-allowed-origins")
			adminCookieDomain := c.String("admin-cookie-domain")
			adminCrossOriginCookies := c.Bool("admin-cross-origin-cookies")

			if len(adminAllowedOrigins) == 0 {
				logger.WarnContext(ctx, "no admin allowed origins configured, so only same-host writes are accepted")
			}

			mux.Use(middleware.AdminCORS(adminAllowedOrigins))
			mux.Use(middleware.AdminOriginCheck(adminAllowedOrigins))
			mux.Use(func(h http.Handler) http.Handler {
				return otelhttp.NewHandler(h, "http", otelhttp.WithServerName("gram"))
			})
			mux.Use(middleware.RouteLabelerMiddleware)
			mux.Use(middleware.NewHTTPLoggingMiddleware(logger))
			mux.Use(middleware.NewRecovery(logger))
			mux.Use(middleware.AdminCookieAttributes(adminCrossOriginCookies, adminCookieDomain))
			mux.Use(admin.SessionMiddleware)

			adminWorkOSClient := newAdminWorkOSOrganizationCreator(ctx, logger, guardianPolicy, c)
			adminOpenRouter := newAdminOpenRouter(ctx, logger, tracerProvider, guardianPolicy, db, redisClient, c)
			productFeatures := productfeatures.NewClient(logger, tracerProvider, db, redisClient)
			loopsWorkflowClient := loops.NewWorkflowClient(ctx, logger, guardianPolicy, c.String("loops-api-key"))
			trialNotifier := trialemails.NewService(db, loopsWorkflowClient, logger, c.String("site-url"))

			billingOperations := usage.NewBillingOperations(logger, db, stripeClient, billingTelemetry, audit.NewLogger())
			admin.Attach(mux, admin.NewService(logger, tracerProvider, db, redisClient, adminOIDCClient, adminEncryption, adminAllowedOrigins, adminWorkOSClient, adminOpenRouter, trialNotifier, productFeatures, chatAnalysisSignaler, openRouterSpendCap, billingOperations, siteURL))

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
					temporalHealth,
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
