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
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sourcegraph/conc/pool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/devidp/keystore"
	"github.com/speakeasy-api/gram/server/internal/devidp/modes/localspeakeasy"
	"github.com/speakeasy-api/gram/server/internal/devidp/modes/oauth2"
	"github.com/speakeasy-api/gram/server/internal/devidp/modes/oauth21"
	devidpworkos "github.com/speakeasy-api/gram/server/internal/devidp/modes/workos"
	"github.com/speakeasy-api/gram/server/internal/devidp/service"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func newDevIdpCommand() *cli.Command {
	var shutdownFuncs []func(context.Context) error

	flags := []cli.Flag{
		&cli.PathFlag{
			Name:     "config-file",
			Usage:    "Path to a config file to load. Supported formats are JSON, TOML and YAML.",
			EnvVars:  []string{"GRAM_CONFIG_FILE"},
			Required: false,
		},
		&cli.StringFlag{
			Name:    "address",
			Value:   ":35291",
			Usage:   "HTTP address to listen on for the dev-idp server",
			EnvVars: []string{"GRAM_DEVIDP_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "control-address",
			Value:   ":35292",
			Usage:   "HTTP address to listen on for the dev-idp control server",
			EnvVars: []string{"GRAM_DEVIDP_CONTROL_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "external-url",
			Usage:   "Public base URL the modes embed in discovery docs and redirect URIs. Derived from --address when unset.",
			EnvVars: []string{"GRAM_DEVIDP_EXTERNAL_URL"},
		},
		&cli.StringFlag{
			Name:     "database-url",
			Usage:    "dev-idp's own Postgres database URL. Atlas declarative apply will reshape it to match SDL — never point this at production.",
			EnvVars:  []string{"GRAM_DEVIDP_DATABASE_URL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:    "speakeasy-secret-key",
			Value:   "test-secret",
			Usage:   "The legacy local-speakeasy header secret. Reuses SPEAKEASY_SECRET_KEY so the start/worker procs share the value.",
			EnvVars: []string{"SPEAKEASY_SECRET_KEY"},
		},
		&cli.StringFlag{
			Name:    "rsa-private-key",
			Usage:   "PEM-encoded RSA private key (PKCS#8 or PKCS#1). When omitted, dev-idp generates a fresh ephemeral keypair on boot.",
			EnvVars: []string{"GRAM_DEVIDP_RSA_PRIVATE_KEY"},
		},
		&cli.StringFlag{
			Name:    "workos-api-key",
			Usage:   "When set, mounts the /workos/ mode (a thin proxy over the live WorkOS REST API).",
			EnvVars: []string{"WORKOS_API_KEY"},
		},
		&cli.StringFlag{
			Name:    "workos-host",
			Value:   "https://api.workos.com",
			Usage:   "Base URL of the WorkOS API. Override for staging / sandbox / a recorded fixture host.",
			EnvVars: []string{"WORKOS_HOST"},
		},
		&cli.StringFlag{
			Name:    "environment",
			Usage:   "The current server environment", // local, dev, prod — drives the guardian policy used by the workos mode
			Value:   "local",
			EnvVars: []string{"GRAM_ENVIRONMENT"},
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
	}

	return &cli.Command{
		Name:  "dev-idp",
		Usage: "Start the local-development IDP (local-speakeasy + workos + oauth2-1 + oauth2 modes)",
		Flags: flags,
		Action: func(c *cli.Context) error {
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

			serviceName := "gram-dev-idp"
			serviceEnv := c.String("environment")
			appinfo := o11y.PullAppInfo(c.Context)
			appinfo.Command = "dev-idp"
			logger := PullLogger(c.Context).With(
				attr.SlogComponent("dev-idp"),
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)

			shutdown, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
				ServiceName:    serviceName,
				ServiceVersion: shortGitSHA(),
				GitSHA:         GitSHA,
				EnableTracing:  c.Bool("with-otel-tracing"),
				EnableMetrics:  c.Bool("with-otel-metrics"),
			})
			if err != nil {
				return fmt.Errorf("failed to setup opentelemetry sdk: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)
			tracerProvider := otel.GetTracerProvider()
			meterProvider := otel.GetMeterProvider()
			slog.SetDefault(logger)

			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: false,
			})
			if err != nil {
				return err
			}
			if err := db.Ping(ctx); err != nil {
				logger.ErrorContext(ctx, "failed to ping dev-idp database", attr.SlogError(err))
				return fmt.Errorf("dev-idp database ping failed: %w", err)
			}
			defer db.Close()

			ks, err := keystore.New([]byte(c.String("rsa-private-key")), logger)
			if err != nil {
				return fmt.Errorf("init dev-idp keystore: %w", err)
			}

			externalURL := c.String("external-url")
			if externalURL == "" {
				externalURL = deriveExternalURL(c.String("address"))
			}

			// Goa management API and the per-mode protocol handlers live on
			// different mux types: the AttachXxx helpers expect a
			// goahttp.Muxer (method+pattern routing), while each mode
			// exposes an http.Handler covering many paths. We compose them
			// by registering Goa endpoints onto an inner goahttp.Muxer,
			// then delegating to it from an outer stdlib http.ServeMux as
			// the catch-all — Go 1.22+ ServeMux's specificity rules send
			// /<mode>/* to the mode handlers and everything else
			// (including /rpc/* and /healthz) to the Goa muxer.
			goaMux := goahttp.NewMuxer()
			goaMux.Use(func(h http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
						w.WriteHeader(http.StatusOK)
						return
					}
					h.ServeHTTP(w, r)
				})
			})
			goaMux.Use(func(h http.Handler) http.Handler {
				return otelhttp.NewHandler(h, "http", otelhttp.WithServerName("gram-dev-idp"))
			})
			goaMux.Use(middleware.RouteLabelerMiddleware)
			goaMux.Use(middleware.NewHTTPLoggingMiddleware(logger))
			goaMux.Use(middleware.NewRecovery(logger))

			service.AttachOrganizations(goaMux, service.NewOrganizationsService(logger, tracerProvider, db))
			service.AttachUsers(goaMux, service.NewUsersService(logger, tracerProvider, db))
			service.AttachMemberships(goaMux, service.NewMembershipsService(logger, tracerProvider, db))
			service.AttachOrganizationRoles(goaMux, service.NewOrganizationRolesService(logger, tracerProvider, db))
			service.AttachInvitations(goaMux, service.NewInvitationsService(logger, tracerProvider, db))
			service.AttachDevIdp(goaMux, service.NewDevIdpService(logger, tracerProvider, db))

			outer := http.NewServeMux()

			mockHandler := localspeakeasy.NewHandler(
				localspeakeasy.Config{SecretKey: c.String("speakeasy-secret-key")},
				logger, tracerProvider, db,
			)
			outer.Handle(localspeakeasy.Prefix+"/", http.StripPrefix(localspeakeasy.Prefix, mockHandler.Handler()))

			oauth21Handler := oauth21.NewHandler(
				oauth21.Config{ExternalURL: externalURL},
				ks, logger, tracerProvider, db,
			)
			outer.Handle(oauth21.Prefix+"/", http.StripPrefix(oauth21.Prefix, oauth21Handler.Handler()))

			oauth2Handler := oauth2.NewHandler(
				oauth2.Config{ExternalURL: externalURL},
				ks, logger, tracerProvider, db,
			)
			outer.Handle(oauth2.Prefix+"/", http.StripPrefix(oauth2.Prefix, oauth2Handler.Handler()))

			// workos mode is opt-in: only mounted when an API key is
			// configured. When absent the prefix simply doesn't route.
			if apiKey := c.String("workos-api-key"); apiKey != "" {
				guardianPolicy, err := newGuardianPolicy(c, tracerProvider)
				if err != nil {
					return fmt.Errorf("init guardian policy for workos mode: %w", err)
				}
				wsClient := workos.NewClient(guardianPolicy, apiKey, workos.ClientOpts{
					Endpoint:   c.String("workos-host"),
					HTTPClient: nil,
				})
				wsHandler := devidpworkos.NewHandler(wsClient, logger, tracerProvider, db)
				outer.Handle(devidpworkos.Prefix+"/", http.StripPrefix(devidpworkos.Prefix, wsHandler.Handler()))
				logger.InfoContext(ctx, "dev-idp /workos/ mode mounted")
			}

			outer.Handle("/", goaMux)

			srv := &http.Server{
				Addr:              c.String("address"),
				Handler:           outer,
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

				logger.InfoContext(ctx, "shutting down dev-idp server")

				graceCtx, graceCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer graceCancel()

				if err := srv.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "failed to shutdown dev-idp server", attr.SlogError(err))
				}
			})

			{
				controlServer := control.Server{
					Address:          c.String("control-address"),
					Logger:           logger.With(attr.SlogComponent("control")),
					DisableProfiling: false,
				}

				listenAddr := srv.Addr
				if listenAddr == "" {
					listenAddr = ":35291"
				}
				host, port, _ := net.SplitHostPort(listenAddr)
				if host == "" {
					host = "localhost"
				}
				healthzEndpoint := &o11y.HTTPEndpoint{
					URL: &url.URL{
						Scheme: "http",
						Host:   net.JoinHostPort(host, port),
						Path:   "/healthz",
					},
					TLSCertificate: nil,
				}

				shutdown, err := controlServer.Start(c.Context, o11y.NewHealthCheckHandler(
					[]*o11y.NamedResource[*o11y.HTTPEndpoint]{{Name: "api", Resource: healthzEndpoint}},
					[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}},
					nil, // dev-idp has no redis dependency
					[]*o11y.NamedResource[client.Client]{},
				))
				if err != nil {
					return fmt.Errorf("failed to start dev-idp control server: %w", err)
				}
				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			logger.InfoContext(ctx, "dev-idp server started",
				attr.SlogServerAddress(c.String("address")),
				attr.SlogURLFull(externalURL),
			)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.ErrorContext(ctx, "dev-idp server error", attr.SlogError(err))
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

// deriveExternalURL turns a listener address ("host:port" or ":port") into
// an externally usable base URL. Bare-port addresses (`:35291`) become
// `http://localhost:35291`. Falls back to the address verbatim if it can't
// be parsed.
func deriveExternalURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + strings.TrimPrefix(addr, "http://")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}
