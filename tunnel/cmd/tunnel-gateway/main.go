// Command tunnel-gateway serves public agent connects and internal tunnel forwards.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/tunnel/gateway"
	"github.com/speakeasy-api/gram/tunnel/redisconn"
	"github.com/speakeasy-api/gram/tunnel/redisswitch"
	"github.com/speakeasy-api/gram/tunnel/route"
)

// partitionAttrKey matches the gram.partition attribute the Speakeasy server
// puts on its logs, so the gateway's logs filter the same way.
const partitionAttrKey = "gram.partition"

// knownPartitions mirrors the closed set in server/internal/partition. Go's
// internal visibility rule keeps this package from importing it, so the list
// is repeated here and the tests pin it.
var knownPartitions = []string{"us", "eu"}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	partition, err := parsePartition(os.Getenv("GRAM_PARTITION"))
	if err != nil {
		logger.ErrorContext(context.Background(), "invalid GRAM_PARTITION", slog.Any("error", err))
		os.Exit(2)
	}
	logger = logger.With(slog.String(partitionAttrKey, partition))

	publicListenAddr := envOr("TUNNEL_GATEWAY_PUBLIC_ADDR", envOr("TUNNEL_GATEWAY_ADDR", ":8090"))
	forwardListenAddr := envOr("TUNNEL_GATEWAY_FORWARD_ADDR", ":8091")
	// AdvertiseAddr is the internal gram-server -> gateway address, not the public agent URL.
	advertiseAddr := envOr("TUNNEL_GATEWAY_ADVERTISE_ADDR", "http://tunnel-gateway-forward:8091")
	// Require a forward token so internal forwarding cannot ship unauthenticated.
	forwardToken := strings.TrimSpace(os.Getenv("TUNNEL_GATEWAY_FORWARD_TOKEN"))
	if forwardToken == "" {
		logger.ErrorContext(context.Background(), "TUNNEL_GATEWAY_FORWARD_TOKEN is required")
		os.Exit(2)
	}
	// Zero falls back to the benchmarked default in gateway.New.
	maxSessions, err := parseMaxSessions(os.Getenv("TUNNEL_GATEWAY_MAX_SESSIONS"))
	if err != nil {
		logger.ErrorContext(context.Background(), "TUNNEL_GATEWAY_MAX_SESSIONS must be a non-negative integer")
		os.Exit(2)
	}

	routes, err := buildRouteStore(logger)
	if err != nil {
		logger.ErrorContext(context.Background(), "tunnel-gateway route store init failed", slog.Any("error", err))
		os.Exit(2)
	}
	keys, err := buildKeyResolver(context.Background())
	if err != nil {
		logger.ErrorContext(context.Background(), "tunnel-gateway key resolver init failed", slog.Any("error", err))
		os.Exit(2)
	}
	defer keys.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	collector, stopMetrics := startMetrics(ctx, logger)

	gw, err := gateway.New(gateway.Config{
		AdvertiseAddr: advertiseAddr,
		// MaxStreamsPerTunnel left unset: gateway.New applies its
		// defaultMaxStreamsPerTunnel so the cap has a single source of truth.
		MaxSessions:     maxSessions,
		ForwardToken:    forwardToken,
		AuthzPublicKeys: os.Getenv("GRAM_AUTHZ_PUBLIC_KEYS"),
		Metrics:         collector,
	}, keys, routes, logger)
	if err != nil {
		logger.ErrorContext(context.Background(), "tunnel-gateway init failed", slog.Any("error", err))
		os.Exit(2)
	}

	publicSrv := &http.Server{
		Addr:              publicListenAddr,
		Handler:           gw.PublicHandler(),
		ReadHeaderTimeout: 15 * time.Second,
	}
	forwardSrv := &http.Server{
		Addr:              forwardListenAddr,
		Handler:           gw.ForwardHandler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	go gw.RunMetrics(ctx)

	var shutdownOnce sync.Once
	shutdownDone := make(chan struct{})
	shutdown := func() {
		shutdownOnce.Do(func() {
			defer close(shutdownDone)
			shutCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			drainCtx, cancelDrain := context.WithTimeout(shutCtx, 10*time.Second)
			gw.Drain(drainCtx)
			cancelDrain()
			forwardCtx, cancelForward := context.WithTimeout(shutCtx, 10*time.Second)
			_ = forwardSrv.Shutdown(forwardCtx)
			cancelForward()
			closeSessionsCtx, cancelCloseSessions := context.WithTimeout(shutCtx, 4*time.Second)
			gw.CloseSessions(closeSessionsCtx)
			cancelCloseSessions()
			_ = publicSrv.Shutdown(shutCtx)
		})
	}
	go func() {
		<-ctx.Done()
		shutdown()
	}()

	errCh := make(chan error, 2)
	go serveHTTP(ctx, errCh, logger, "public", publicSrv)
	go serveHTTP(ctx, errCh, logger, "forward", forwardSrv)

	logger.InfoContext(ctx, "tunnel-gateway listening",
		slog.String("public_addr", publicListenAddr),
		slog.String("forward_addr", forwardListenAddr),
		slog.String("advertise", advertiseAddr))
	var serverErr error
	for range 2 {
		if err := <-errCh; err != nil && serverErr == nil {
			serverErr = err
			stop()
		}
	}
	<-shutdownDone
	metricsCtx, cancelMetrics := context.WithTimeout(context.Background(), 15*time.Second)
	if err := stopMetrics(metricsCtx); err != nil {
		logger.WarnContext(metricsCtx, "tunnel metrics shutdown incomplete", slog.Any("error", err))
	}
	cancelMetrics()
	if serverErr != nil {
		logger.ErrorContext(context.Background(), "tunnel-gateway server error", slog.Any("error", serverErr))
		os.Exit(1)
	}
}

func serveHTTP(ctx context.Context, errCh chan<- error, logger *slog.Logger, name string, srv *http.Server) {
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		errCh <- err
		return
	}
	logger.InfoContext(ctx, "tunnel-gateway listener stopped", slog.String("listener", name))
	errCh <- nil
}

func buildRouteStore(logger *slog.Logger) (route.Store, error) {
	addr := strings.TrimSpace(os.Getenv("TUNNEL_REDIS_ADDR"))
	if addr == "" {
		return nil, errors.New("TUNNEL_REDIS_ADDR is required")
	}
	client, err := redisswitch.NewClient(context.Background(), logger, &redis.Options{}, redisswitch.Binding{
		Role:    redisswitch.DefaultRole,
		Current: redisconn.PasswordEndpoint(addr, os.Getenv("TUNNEL_REDIS_PASSWORD")),
		Next:    redisswitch.NextFromEnv(),
		Tokens:  nil,
	})
	if err != nil {
		return nil, err
	}
	logger.InfoContext(context.Background(), "tunnel-gateway using redis route store", slog.String("addr", addr))
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

func parseMaxSessions(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, errors.New("max sessions must be non-negative")
	}
	return parsed, nil
}

// parsePartition returns the partition named by GRAM_PARTITION. It has no
// default: unset, empty or unknown is an error, as in the server, so a
// deployment that forgets it fails at startup.
func parsePartition(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("GRAM_PARTITION is required: set it to one of %s", strings.Join(knownPartitions, ", "))
	}
	if !slices.Contains(knownPartitions, raw) {
		return "", fmt.Errorf("unknown partition %q: must be one of %s", raw, strings.Join(knownPartitions, ", "))
	}
	return raw, nil
}
