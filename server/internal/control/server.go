package control

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type Server struct {
	Address          string
	Logger           *slog.Logger
	DisableProfiling bool
}

func (s *Server) Start(ctx context.Context, healthCheck http.Handler) (shutdown func(context.Context) error, err error) {
	mux := http.NewServeMux()

	if !s.DisableProfiling {
		mux.HandleFunc("GET /debug/pprof/", pprof.Index)
		mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	}

	mux.Handle("POST /panic", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("forced panic")
	}))

	mux.Handle("GET /healthz", healthCheck)
	mux.Handle("GET /livez", healthCheck)

	srv := &http.Server{
		Addr:              s.Address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	shutdown = func(ctx context.Context) error {
		return srv.Shutdown(ctx)
	}

	go func() {
		s.Logger.InfoContext(ctx, "control server started", attr.SlogServerAddress(s.Address))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.Logger.ErrorContext(ctx, "control server error", attr.SlogError(err))
		}
	}()

	return shutdown, nil
}
