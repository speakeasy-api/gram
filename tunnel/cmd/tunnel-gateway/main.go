// Command tunnel-gateway terminates agent WebSocket upgrades, owns the yamux
// sessions, and maps internal forward requests onto substreams by tunnel ID.
// The standalone process uses Postgres for key resolution and Redis for live
// routes and connection snapshots.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/tunnel/gateway"
	"github.com/speakeasy-api/gram/tunnel/route"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	listenAddr := envOr("TUNNEL_GATEWAY_ADDR", ":8090")
	// AdvertiseAddr is what gram-server uses to reach this pod; in k8s set it to
	// the pod IP + port via the downward API. Public agent traffic is expected to
	// reach this service through TLS-terminating ingress; this address is the
	// internal gram-server -> gateway route.
	advertiseAddr := envOr("TUNNEL_GATEWAY_ADVERTISE_ADDR", "http://tunnel-gateway:8090")

	routes, err := buildRouteStore(logger)
	if err != nil {
		logger.Error("tunnel-gateway route store init failed", slog.Any("error", err))
		os.Exit(2)
	}
	keys, err := buildKeyResolver(context.Background())
	if err != nil {
		logger.Error("tunnel-gateway key resolver init failed", slog.Any("error", err))
		os.Exit(2)
	}
	defer keys.Close()

	gw := gateway.New(gateway.Config{
		AdvertiseAddr:       advertiseAddr,
		MaxStreamsPerTunnel: 256,
	}, keys, routes, logger)

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           gw.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	logger.Info("tunnel-gateway listening",
		slog.String("addr", listenAddr), slog.String("advertise", advertiseAddr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("tunnel-gateway server error", slog.Any("error", err))
		os.Exit(1)
	}
}

func buildRouteStore(logger *slog.Logger) (route.Store, error) {
	addr := strings.TrimSpace(os.Getenv("TUNNEL_REDIS_ADDR"))
	if addr == "" {
		return nil, errors.New("TUNNEL_REDIS_ADDR is required")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("TUNNEL_REDIS_PASSWORD"),
	})
	logger.Info("tunnel-gateway using redis route store", slog.String("addr", addr))
	return route.NewRedis(client), nil
}

func buildKeyResolver(ctx context.Context) (*gateway.PostgresKeyResolver, error) {
	dbURL := strings.TrimSpace(os.Getenv("TUNNEL_DATABASE_URL"))
	if dbURL == "" {
		return nil, errors.New("TUNNEL_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return gateway.NewPostgresKeyResolver(pool), nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
