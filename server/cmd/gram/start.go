package gram

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgx-contrib/pgxotel"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/chat"
	"github.com/speakeasy-api/gram/internal/control"
	"github.com/speakeasy-api/gram/internal/deployments"
	"github.com/speakeasy-api/gram/internal/environments"
	"github.com/speakeasy-api/gram/internal/instances"
	"github.com/speakeasy-api/gram/internal/keys"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/must"
	"github.com/speakeasy-api/gram/internal/o11y"
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
				Name:    "observe",
				Usage:   "Enable OpenTelemetry observability",
				EnvVars: []string{"GRAM_ENABLE_OTEL"},
			},
			&cli.BoolFlag{
				Name:    "trace-queries",
				Usage:   "Trace database queries",
				EnvVars: []string{"GRAM_TRACE_QUERIES"},
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
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()
			logger := PullLogger(ctx)

			sigctx, sigcancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer sigcancel()

			poolcfg := must.Value(pgxpool.ParseConfig(c.String("database-url")))
			if c.Bool("observe") {
				shutdown, err := o11y.SetupOTelSDK(ctx)
				if err != nil {
					return err
				}
				shutdownFuncs = append(shutdownFuncs, shutdown)

				poolcfg.ConnConfig.Tracer = &pgxotel.QueryTracer{
					Name: "pgx",
				}
			}

			db, err := pgxpool.NewWithConfig(ctx, poolcfg)
			if err != nil {
				return err
			}
			defer db.Close()

			var assetStorage assets.BlobStore
			{
				assetsBackend := c.String("assets-backend")
				assetsURI := c.String("assets-uri")
				switch assetsBackend {
				case "fs":
					assetsURI = filepath.Clean(assetsURI)
					if err := os.MkdirAll(assetsURI, 0755); err != nil && !errors.Is(err, fs.ErrExist) {
						return err
					}

					root, err := os.OpenRoot(assetsURI)
					if err != nil {
						return err
					}
					defer root.Close()

					fstore := &assets.FSBlobStore{Root: root}
					assetStorage = fstore
				case "gcs":
					gcsStore, err := assets.NewGCSBlobStore(ctx, assetsURI)
					if err != nil {
						return err
					}
					assetStorage = gcsStore
				default:
					return fmt.Errorf("invalid assets backend: %s", assetsBackend)
				}
			}

			var redisClient *redis.Client
			{
				redisAddr := c.String("redis-cache-addr")
				redisPassword := c.String("redis-cache-password")

				db := 0 // we always use default DB
				redisClient = redis.NewClient(&redis.Options{
					Addr:         redisAddr,
					Password:     redisPassword,
					DB:           db,
					DialTimeout:  1 * time.Second,
					ReadTimeout:  300 * time.Millisecond,
					WriteTimeout: 1 * time.Second,
				})

				if err := redisClient.Ping(context.Background()).Err(); err != nil {
					logger.Error("redis connection failed", slog.String("error", err.Error()))
					panic(err)
				}

				attrs := redisotel.WithAttributes(
					semconv.DBSystemRedis,
					semconv.DBRedisDBIndex(db),
				)
				if err := redisotel.InstrumentTracing(redisClient, redisotel.WithDBStatement(false), attrs); err != nil {
					panic(err)
				}
			}

			localEnvPath := c.String("unsafe-local-env-path")
			var sesh *sessions.Sessions
			if localEnvPath == "" {
				sesh = sessions.NewSessionAuth(logger.With("component", "sessions"), redisClient, cache.SuffixNone)
			} else {
				logger.WarnContext(ctx, "enabling unsafe session store", slog.String("path", localEnvPath))
				s, err := sessions.NewUnsafeSessionAuth(logger.With("component", "sessions"), redisClient, cache.Suffix("gram-local"), localEnvPath)
				if err != nil {
					return err
				}

				sesh = s
			}

			{
				controlServer := control.Server{
					Address: c.String("control-address"),
					Logger:  logger.With(slog.String("component", "control")),
				}

				shutdown, err := controlServer.Start(c.Context, o11y.NewHealthCheckHandler(db, redisClient))
				if err != nil {
					return err
				}

				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			mux := goahttp.NewMuxer()

			mux.Use(middleware.CORSMiddleware)
			mux.Use(middleware.NewHTTPLoggingMiddleware(logger.With("component", "http")))
			mux.Use(middleware.SessionMiddleware)

			chatService := chat.NewService(logger.With("component", "chat"), db, sesh, c.String("openai-api-key"))
			mux.Handle("POST", "/chat/completions", func(w http.ResponseWriter, r *http.Request) {
				chatService.HandleCompletion(w, r)
			})
			auth.Attach(mux, auth.NewService(logger.With("component", "auth"), db, sesh))
			assets.Attach(mux, assets.NewService(logger.With("component", "assets"), db, sesh, assetStorage))
			deployments.Attach(mux, deployments.NewService(logger.With("component", "deployments"), db, sesh, assetStorage))
			toolsets.Attach(mux, toolsets.NewService(logger.With("component", "toolsets"), db, sesh))
			keys.Attach(mux, keys.NewService(logger.With("component", "keys"), db, sesh))
			environments.Attach(mux, environments.NewService(logger.With("component", "environments"), db, sesh))
			tools.Attach(mux, tools.NewService(logger.With("component", "tools"), db, sesh))
			instances.Attach(mux, instances.NewService(logger.With("component", "instances"), db, sesh))

			srv := &http.Server{
				Addr:    c.String("address"),
				Handler: otelhttp.NewHandler(mux, "/"),
				BaseContext: func(net.Listener) context.Context {
					return ctx
				},
			}

			go func() {
				<-sigctx.Done()

				logger.InfoContext(ctx, "shutting down server")

				graceCtx, graceCancel := context.WithTimeout(ctx, 10*time.Second)
				defer graceCancel()

				if err := srv.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "failed to shutdown development server", slog.String("error", err.Error()))
				}
			}()

			logger.InfoContext(ctx, "server started", slog.String("address", c.String("address")))
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.ErrorContext(ctx, "server error", slog.String("error", err.Error()))
			}

			return nil
		},
		After: func(c *cli.Context) error {
			ctx := context.Background()
			logger := PullLogger(c.Context)

			var wg sync.WaitGroup
			wg.Add(len(shutdownFuncs))

			done := make(chan struct{})

			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			for _, shutdown := range shutdownFuncs {
				go func(shutdown func(context.Context) error) {
					defer wg.Done()
					if err := shutdown(ctx); err != nil {
						logger.ErrorContext(ctx, "failed to shutdown component", slog.String("error", err.Error()))
					}
				}(shutdown)
			}

			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
			case <-ctx.Done():
				return errors.New("failed to shutdown all components")
			}

			logger.InfoContext(c.Context, "all components shutdown")
			return nil
		},
	}
}
