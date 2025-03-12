package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
)

type Server struct {
	Address          string
	Logger           *slog.Logger
	DisableProfiling bool
}

func (s *Server) Start(ctx context.Context) (shutdown func(context.Context) error, err error) {
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

	mux.Handle("GET /health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status": "control server is healthy"}`)
	}))

	srv := &http.Server{
		Addr:    s.Address,
		Handler: mux,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	shutdown = func(ctx context.Context) error {
		return srv.Shutdown(ctx)
	}

	go func() {
		s.Logger.InfoContext(ctx, "control server started", slog.String("address", s.Address))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.Logger.ErrorContext(ctx, "control server error", slog.String("err", err.Error()))
		}
	}()

	return shutdown, nil
}
