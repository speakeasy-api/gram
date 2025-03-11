package gram

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

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
				Name:  "address",
				Value: ":8080",
				Usage: "HTTP address to listen on",
			},
		},
		Action: func(c *cli.Context) error {
			ctx := c.Context
			logger := log.From(ctx)

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
				deploymentsService := deployments.NewService(nil)
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

			shutdownCtx, shutdown := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
			defer shutdown()

			go func() {
				select {
				case <-ctx.Done():
				case <-shutdownCtx.Done():
				}

				logger.InfoContext(ctx, "shutting down development server")

				graceCtx, graceCancel := context.WithTimeout(context.Background(), 60*time.Second)
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
