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
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgx-contrib/pgxotel"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/deployments"
	"github.com/speakeasy-api/gram/internal/environments"
	"github.com/speakeasy-api/gram/internal/keys"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/must"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/system"
	"github.com/speakeasy-api/gram/internal/tools"
	"github.com/speakeasy-api/gram/internal/toolsets"
)

func newStartCommand() *cli.Command {
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
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()
			logger := PullLogger(ctx).With(slog.String("app", "gram"))

			sigctx, sigcancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer sigcancel()

			poolcfg := must.Value(pgxpool.ParseConfig(c.String("database-url")))
			if c.Bool("observe") {
				shutdown, err := o11y.SetupOTelSDK(ctx)
				if err != nil {
					return err
				}
				defer func() {
					graceCtx, graceCancel := context.WithTimeout(ctx, 60*time.Second)
					defer graceCancel()
					if err := shutdown(graceCtx); err != nil {
						logger.ErrorContext(ctx, "failed to shutdown OpenTelemetry", slog.String("err", err.Error()))
					}
				}()

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

			mux := goahttp.NewMuxer()
			mux.Use(middleware.NewHTTPLoggingMiddleware(logger.With("component", "http")))
			mux.Use(middleware.SessionMiddleware)
			auth.Attach(mux, auth.NewService(logger.With("component", "auth"), db))
			assets.Attach(mux, assets.NewService(logger.With("component", "assets"), db, assetStorage))
			system.Attach(mux, system.NewService())
			deployments.Attach(mux, deployments.NewService(logger.With("component", "deployments"), db))
			toolsets.Attach(mux, toolsets.NewService(logger.With("component", "toolsets"), db))
			keys.Attach(mux, keys.NewService(logger.With("component", "keys"), db))
			environments.Attach(mux, environments.NewService(logger.With("component", "environments"), db))
			tools.Attach(mux, tools.NewService(logger.With("component", "tools"), db))

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

				graceCtx, graceCancel := context.WithTimeout(ctx, 60*time.Second)
				defer graceCancel()

				if err := srv.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "failed to shutdown development server", slog.String("err", err.Error()))
				}
			}()

			logger.InfoContext(ctx, "server started", slog.String("address", c.String("address")))
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.ErrorContext(ctx, "server error", slog.String("err", err.Error()))
			}

			return nil
		},
	}
}
