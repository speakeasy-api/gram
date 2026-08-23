// Command network-gateway runs one embedded overlay-network node per enabled
// network ingress and reverse-proxies each org's MCP surface into gram-server
// behind the X-Gram-Netingress-* forward-token trust boundary.
package main

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/netgateway/gateway"
	"github.com/speakeasy-api/gram/netgateway/provider/tailscale"
)

// defaultMaxNodes caps nodes per replica. The Phase 0 spike measured ~5MB
// marginal RSS per idle tsnet node, so the default is memory-conservative
// with ample headroom for traffic buffers.
const defaultMaxNodes = 64

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	forwardToken := strings.TrimSpace(os.Getenv("NETWORK_GATEWAY_FORWARD_TOKEN"))
	if forwardToken == "" {
		fail(logger, "NETWORK_GATEWAY_FORWARD_TOKEN is required")
	}

	upstreamRaw := strings.TrimSpace(os.Getenv("NETWORK_GATEWAY_UPSTREAM_URL"))
	if upstreamRaw == "" {
		fail(logger, "NETWORK_GATEWAY_UPSTREAM_URL is required")
	}
	upstream, err := url.Parse(upstreamRaw)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		fail(logger, "NETWORK_GATEWAY_UPSTREAM_URL must be an absolute URL")
	}

	encKey := strings.TrimSpace(os.Getenv("GRAM_ENCRYPTION_KEY"))
	if encKey == "" {
		fail(logger, "GRAM_ENCRYPTION_KEY is required")
	}
	enc, err := gateway.NewDecryptor(encKey)
	if err != nil {
		fail(logger, "GRAM_ENCRYPTION_KEY is invalid", slog.Any("error", err))
	}

	dbURL := strings.TrimSpace(os.Getenv("NETWORK_GATEWAY_DATABASE_URL"))
	if dbURL == "" {
		fail(logger, "NETWORK_GATEWAY_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fail(logger, "network-gateway database init failed", slog.Any("error", err))
	}
	if err := pool.Ping(ctx); err != nil {
		fail(logger, "network-gateway database ping failed", slog.Any("error", err))
	}
	store := gateway.NewPostgresStore(pool, enc)
	defer store.Close()

	redisAddr := strings.TrimSpace(os.Getenv("NETWORK_GATEWAY_REDIS_ADDR"))
	if redisAddr == "" {
		fail(logger, "NETWORK_GATEWAY_REDIS_ADDR is required")
	}
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("NETWORK_GATEWAY_REDIS_PASSWORD"),
	})

	replicaID := strings.TrimSpace(os.Getenv("NETWORK_GATEWAY_REPLICA_ID"))
	if replicaID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			fail(logger, "resolve replica id", slog.Any("error", err))
		}
		replicaID = hostname
	}

	maxNodes := defaultMaxNodes
	if raw := strings.TrimSpace(os.Getenv("NETWORK_GATEWAY_MAX_NODES")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			fail(logger, "NETWORK_GATEWAY_MAX_NODES must be a positive integer")
		}
		maxNodes = parsed
	}

	tsProvider := &tailscale.Provider{
		// Empty means the public Tailscale control plane; set for tests or a
		// future Headscale variant.
		ControlURL: strings.TrimSpace(os.Getenv("NETWORK_GATEWAY_CONTROL_URL")),
		APIBase:    "",
		Logger:     logger,
	}

	supervisor := gateway.NewSupervisor(gateway.SupervisorConfig{
		Source:       store,
		Lease:        gateway.NewRedisLease(redisClient, replicaID),
		Providers:    map[string]gateway.Provider{tsProvider.Name(): tsProvider},
		Upstream:     upstream,
		ForwardToken: forwardToken,
		MaxNodes:     maxNodes,
		Logger:       logger,
	})

	logger.InfoContext(ctx, "network-gateway supervising ingresses",
		slog.String("upstream", upstream.String()),
		slog.String("replica_id", replicaID),
		slog.Int("max_nodes", maxNodes))
	supervisor.Run(ctx)

	if err := redisClient.Close(); err != nil {
		logger.WarnContext(context.Background(), "network-gateway redis close failed", slog.Any("error", err))
	}
	logger.InfoContext(context.Background(), "network-gateway stopped")
}

func fail(logger *slog.Logger, msg string, args ...any) {
	logger.ErrorContext(context.Background(), msg, args...)
	os.Exit(2)
}
