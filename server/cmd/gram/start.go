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
	"github.com/speakeasy-api/gram/internal/environments"
	"github.com/speakeasy-api/gram/internal/keys"
	"github.com/speakeasy-api/gram/internal/toolsets"
	"github.com/urfave/cli/v2"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/deployments"
	"github.com/speakeasy-api/gram/internal/log"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/system"
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
			logger := log.From(ctx).With(slog.String("service", "gram"))

			db, err := pgxpool.New(ctx, c.String("database-url"))
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
			mux.Use(middleware.RequestLoggingMiddleware)
			mux.Use(middleware.GramSessionMiddleware)
			auth.Attach(mux, auth.NewService(logger.With("component", "auth"), db))
			assets.Attach(mux, assets.NewService(logger.With("component", "assets"), db, assetStorage))
			system.Attach(mux, system.NewService())
			deployments.Attach(mux, deployments.NewService(logger.With("component", "deployments"), db))
			toolsets.Attach(mux, toolsets.NewService(logger.With("component", "toolsets"), db))
			keys.Attach(mux, keys.NewService(logger.With("component", "keys"), db))
			environments.Attach(mux, environments.NewService(logger.With("component", "environments"), db))

			srv := &http.Server{
				Addr:    c.String("address"),
				Handler: mux,
				BaseContext: func(net.Listener) context.Context {
					return ctx
				},
			}

			sigctx, sigcancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer sigcancel()

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
