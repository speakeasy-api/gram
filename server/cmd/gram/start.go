package gram

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"
	goahttp "goa.design/goa/v3/http"

	gendeployments "github.com/speakeasy-api/gram/gen/deployments"
	httpdeployments "github.com/speakeasy-api/gram/gen/http/deployments/server"
	httpsystem "github.com/speakeasy-api/gram/gen/http/system/server"
	gensystem "github.com/speakeasy-api/gram/gen/system"
	"github.com/speakeasy-api/gram/internal/deployments"
	"github.com/speakeasy-api/gram/internal/log"
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
				Name:    "database-url",
				Usage:   "Database URL",
				EnvVars: []string{"GRAM_DATABASE_URL"},
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

			mux := goahttp.NewMuxer()
			requestDecoder := goahttp.RequestDecoder
			responseEncoder := goahttp.ResponseEncoder

			{
				systemService := system.NewService()
				systemEndpoints := gensystem.NewEndpoints(systemService)
				httpsystem.Mount(
					mux,
					httpsystem.New(systemEndpoints, mux, requestDecoder, responseEncoder, nil, nil),
				)
			}

			{
				deploymentsService := deployments.NewService(logger.With("component", "deployments"), nil)
				deploymentsEndpoints := gendeployments.NewEndpoints(deploymentsService)
				httpdeployments.Mount(
					mux,
					httpdeployments.New(deploymentsEndpoints, mux, requestDecoder, responseEncoder, nil, nil),
				)
			}

			srv := &http.Server{
				Addr:    c.String("address"),
				Handler: mux,
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
