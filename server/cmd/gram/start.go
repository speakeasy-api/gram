package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sourcegraph/conc/pool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/chat"
	"github.com/speakeasy-api/gram/internal/control"
	"github.com/speakeasy-api/gram/internal/deployments"
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/environments"
	"github.com/speakeasy-api/gram/internal/instances"
	"github.com/speakeasy-api/gram/internal/integrations"
	"github.com/speakeasy-api/gram/internal/keys"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/packages"
	"github.com/speakeasy-api/gram/internal/projects"
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/internal/tools"
	"github.com/speakeasy-api/gram/internal/toolsets"
)

func newStartCommand() *cli.Command {
	var shutdownFuncs []func(context.Context) error

	return &cli.Command{
		Name:  "start",
		Usage: "Start the Gram API server",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "address",
				Value:   ":8080",
				Usage:   "HTTP address to listen on",
				EnvVars: []string{"GRAM_SERVER_ADDRESS"},
			},
			&cli.StringFlag{
				Name:     "environment",
				Usage:    "The current server environment", // local, dev, prod
				Required: true,
				EnvVars:  []string{"GRAM_ENVIRONMENT"},
			},
			&cli.StringFlag{
				Name:    "control-address",
				Value:   ":8081",
				Usage:   "HTTP address to listen on",
				EnvVars: []string{"GRAM_CONTROL_ADDRESS"},
			},
			&cli.StringFlag{
				Name:    "unsafe-local-env-path",
				Usage:   "The path to the local environment file used for session auth in local development",
				EnvVars: []string{"GRAM_UNSAFE_LOCAL_ENV_PATH"},
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
			&cli.StringFlag{
				Name:     "speakeasy-server-address",
				Usage:    "Speakeasy server address",
				EnvVars:  []string{"SPEAKEASY_SERVER_ADDRESS"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "speakeasy-secret-key",
				Usage:    "Speakeasy secret key",
				EnvVars:  []string{"SPEAKEASY_SECRET_KEY"},
				Required: true,
			},
			&cli.BoolFlag{
				Name:    "observe",
				Usage:   "Enable OpenTelemetry observability",
				EnvVars: []string{"GRAM_ENABLE_OTEL"},
			},
			&cli.StringFlag{
				Name:     "assets-backend",
				Usage:    "The backend to use for managing assets",
				EnvVars:  []string{"GRAM_ASSETS_BACKEND"},
				Required: true,
				Action: func(c *cli.Context, val string) error {
					if val != "fs" && val != "gcs" {
						return fmt.Errorf("invalid assets backend: %s", val)
					}
					return nil
				},
			},
			&cli.StringFlag{
				Name:     "assets-uri",
				Usage:    "The location of the assets backend to connect to",
				EnvVars:  []string{"GRAM_ASSETS_URI"},
				Required: true,
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
				Name:     "encryption-key",
				Usage:    "Key for App level AES encryption/decyryption",
				Required: true,
				EnvVars:  []string{"GRAM_ENCRYPTION_KEY"},
			},
			&cli.StringFlag{
				Name:     "openai-api-key",
				Usage:    "API key for the OpenAI API",
				EnvVars:  []string{"GRAM_OPENAI_API_KEY"},
				Required: true,
				Action: func(c *cli.Context, val string) error {
					if strings.TrimSpace(val) == "" {
						return errors.New("OpenAI API key cannot be empty")
					}
					return nil
				},
			},
			&cli.StringFlag{
				Name:    "openrouter-dev-key",
				Usage:   "Dev API key for OpenRouter (primarily for local development) - https://openrouter.ai/settings/keys",
				EnvVars: []string{"OPENROUTER_DEV_KEY"},
			},
			&cli.StringFlag{
				Name:    "openrouter-provisioning-key",
				Usage:   "Provisioning key for OpenRouter to create new API keys for orgs - https://openrouter.ai/settings/provisioning-keys",
				EnvVars: []string{"OPENROUTER_PROVISIONING_KEY"},
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
			&cli.BoolFlag{
				Name:    "dev-single-process",
				Usage:   "Run the server and worker in a single process for local development",
				EnvVars: []string{"GRAM_SINGLE_PROCESS"},
				Value:   false,
			},
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()
			logger := PullLogger(ctx)

			if c.Bool("observe") {
				shutdown, err := o11y.SetupOTelSDK(ctx)
				if err != nil {
					return err
				}
				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			db, err := newDBClient(ctx, logger, c.String("database-url"), dbClientOptions{
				enableTracing:       c.Bool("observe"),
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return err
			}
			defer db.Close()

			assetStorage, shutdown, err := newAssetStorage(ctx, assetStorageOptions{
				assetsBackend: c.String("assets-backend"),
				assetsURI:     c.String("assets-uri"),
			})
			if err != nil {
				return err
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			redisClient, err := newRedisClient(ctx, redisClientOptions{
				redisAddr:     c.String("redis-cache-addr"),
				redisPassword: c.String("redis-cache-password"),
				enableTracing: c.Bool("observe"),
			})
			if err != nil {
				return err
			}

			var openRouter openrouter.Provisioner
			if c.String("environment") == "local" {
				openRouter = openrouter.NewDevelopment(c.String("openrouter-dev-key"))
			} else {
				openRouter = openrouter.New(logger, db, c.String("environment"), c.String("openrouter-provisioning-key"))
			}

			localEnvPath := c.String("unsafe-local-env-path")
			var sessionManager *sessions.Manager
			if localEnvPath == "" {
				sessionManager = sessions.NewManager(logger.With(slog.String("component", "sessions")), redisClient, cache.SuffixNone, c.String("speakeasy-server-address"), c.String("speakeasy-secret-key"))
			} else {
				logger.WarnContext(ctx, "enabling unsafe session store", slog.String("path", localEnvPath))
				s, err := sessions.NewUnsafeManager(logger.With(slog.String("component", "sessions")), redisClient, cache.Suffix("gram-local"), localEnvPath)
				if err != nil {
					return err
				}

				sessionManager = s
			}

			encryptionClient, err := encryption.New(c.String("encryption-key"))
			if err != nil {
				return err
			}

			var temporalClient client.Client
			temporalAddress := c.String("temporal-address")
			temporalNamespace := c.String("temporal-namespace")
			temporalEnabled := temporalAddress != "" && temporalNamespace != ""

			if temporalEnabled {
				temporalClient, err = client.Dial(client.Options{
					HostPort:  temporalAddress,
					Namespace: temporalNamespace,
					Logger:    logger.With(slog.String("component", "temporal")),
				})
				if err != nil {
					return fmt.Errorf("failed to create temporal client: %w", err)
				}

				shutdownFuncs = append(shutdownFuncs, func(context.Context) error {
					temporalClient.Close()
					return nil
				})
			} else {
				logger.WarnContext(ctx, "temporal disabled")
			}

			{
				controlServer := control.Server{
					Address:          c.String("control-address"),
					Logger:           logger.With(slog.String("component", "control")),
					DisableProfiling: false,
				}

				temporals := []*o11y.NamedResource[client.Client]{}
				if temporalClient != nil {
					temporals = append(temporals, &o11y.NamedResource[client.Client]{Name: "default", Resource: temporalClient})
				}

				shutdown, err := controlServer.Start(c.Context, o11y.NewHealthCheckHandler(
					[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}},
					[]*o11y.NamedResource[*redis.Client]{{Name: "default", Resource: redisClient}},
					temporals,
				))
				if err != nil {
					return err
				}

				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			var serverURL string
			switch c.String("environment") {
			case "local", "minikube":
				serverURL = fmt.Sprintf("http://localhost%s", c.String("address"))
			case "dev":
				serverURL = "https://dev.getgram.ai"
			case "prod":
				serverURL = "https://app.getgram.ai"
			default:
				return fmt.Errorf("invalid environment: %s", c.String("environment"))
			}

			chatService := chat.NewService(logger.With(slog.String("component", "chat")), db, sessionManager, c.String("openai-api-key"), openRouter)

			mux := goahttp.NewMuxer()

			mux.Use(middleware.DevCORSMiddleware)
			mux.Use(middleware.NewHTTPLoggingMiddleware(logger.With(slog.String("component", "http"))))
			mux.Use(middleware.SessionMiddleware)
			mux.Use(middleware.AdminOverrideMiddleware)
			mux.Handle("POST", "/chat/completions", func(w http.ResponseWriter, r *http.Request) {
				chatService.HandleCompletion(w, r)
			})

			auth.Attach(mux, auth.NewService(logger.With(slog.String("component", "auth")), db, sessionManager, auth.AuthConfigurations{
				SpeakeasyServerAddress: c.String("speakeasy-server-address"),
				GramServerURL:          serverURL,
				SignInRedirectURL:      auth.FormSignInRedirectURL(c.String("environment")),
			}))
			projects.Attach(mux, projects.NewService(logger.With(slog.String("component", "projects")), db, sessionManager))
			packages.Attach(mux, packages.NewService(logger.With(slog.String("component", "packages")), db, sessionManager))
			integrations.Attach(mux, integrations.NewService(logger.With(slog.String("component", "integrations")), db, sessionManager))
			assets.Attach(mux, assets.NewService(logger.With(slog.String("component", "assets")), db, sessionManager, assetStorage))
			deployments.Attach(mux, deployments.NewService(logger.With(slog.String("component", "deployments")), db, temporalClient, sessionManager, assetStorage))
			toolsets.Attach(mux, toolsets.NewService(logger.With(slog.String("component", "toolsets")), db, sessionManager))
			keys.Attach(mux, keys.NewService(logger.With(slog.String("component", "keys")), db, sessionManager, c.String("environment")))
			environments.Attach(mux, environments.NewService(logger.With(slog.String("component", "environments")), db, sessionManager, encryptionClient))
			tools.Attach(mux, tools.NewService(logger.With(slog.String("component", "tools")), db, sessionManager))
			instances.Attach(mux, instances.NewService(logger.With(slog.String("component", "instances")), db, sessionManager, encryptionClient))

			srv := &http.Server{
				Addr:              c.String("address"),
				Handler:           otelhttp.NewHandler(mux, "/"),
				ReadHeaderTimeout: 10 * time.Second,
				BaseContext: func(net.Listener) context.Context {
					return ctx
				},
			}

			sigctx, sigcancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer sigcancel()

			group := pool.New()

			if temporalClient != nil && c.Bool("dev-single-process") {
				workerInterruptCh := make(chan any)
				group.Go(func() {
					<-sigctx.Done()
					close(workerInterruptCh)
				})
				group.Go(func() {
					temporalWorker := newTemporalWorker(temporalClient, logger.With(slog.String("component", "temporal")), db, assetStorage)
					if err := temporalWorker.Run(workerInterruptCh); err != nil {
						logger.ErrorContext(ctx, "temporal worker failed", slog.String("error", err.Error()))
					}
				})
			}

			group.Go(func() {
				<-sigctx.Done()

				logger.InfoContext(ctx, "shutting down server")

				graceCtx, graceCancel := context.WithTimeout(ctx, 10*time.Second)
				defer graceCancel()

				if err := srv.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "failed to shutdown development server", slog.String("error", err.Error()))
				}
			})

			logger.InfoContext(ctx, "server started", slog.String("address", c.String("address")))
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.ErrorContext(ctx, "server error", slog.String("error", err.Error()))
			}

			cancel()
			group.Wait()

			return nil
		},
		After: func(c *cli.Context) error {
			return runShutdown(PullLogger(c.Context), c.Context, shutdownFuncs)
		},
	}
}
