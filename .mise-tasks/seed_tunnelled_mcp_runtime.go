package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

const defaultLocalTunnelKey = "gram_tunnel_localpostgresmcpseedkey000000000000000000000000000000"

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "seed tunnelled MCP runtime: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if len(os.Args) != 2 {
		return errors.New("usage: seed_tunnelled_mcp_runtime <tunnelled_mcp_server_id>")
	}

	serverID := strings.TrimSpace(os.Args[1])
	if _, err := uuid.Parse(serverID); err != nil {
		return fmt.Errorf("parse tunnelled MCP server id: %w", err)
	}

	tunnelKey := envOr("TUNNEL_LOCAL_KEY", defaultLocalTunnelKey)
	if !wire.HasKeyPrefix(tunnelKey) {
		return fmt.Errorf("TUNNEL_LOCAL_KEY must start with %s", wire.KeyPrefix)
	}

	dbURL := os.Getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return errors.New("GRAM_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	keyPrefix := tunnelKey
	prefixLen := len(wire.KeyPrefix) + 5
	if len(keyPrefix) > prefixLen {
		keyPrefix = keyPrefix[:prefixLen]
	}

	tag, err := pool.Exec(ctx, `
UPDATE tunnelled_mcp_servers
SET
  key_hash = $2,
  key_prefix = $3,
  status = 'active',
  agent_version = $4,
  last_seen_at = now() - interval '9 seconds',
  updated_at = now()
WHERE id = $1::uuid AND deleted IS FALSE
`, serverID, hashKey(tunnelKey), keyPrefix, wire.AgentVersion)
	if err != nil {
		return fmt.Errorf("update tunnelled MCP server: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("expected to update one tunnelled MCP server, updated %d", tag.RowsAffected())
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     envOr("GRAM_REDIS_CACHE_ADDR", "127.0.0.1:5445"),
		Password: os.Getenv("GRAM_REDIS_CACHE_PASSWORD"),
	})
	defer rdb.Close()

	now := time.Now().UTC()
	if err := route.NewRedis(rdb).PublishConnections(ctx, serverID, []route.Connection{
		{
			SessionID:              "gram-postgres-mcp-tunnel",
			ServiceID:              "local-postgres-mcp",
			ServiceSlug:            "postgres-mcp",
			ServiceVersion:         "0.3.0",
			AgentVersion:           wire.AgentVersion,
			ConnectedAt:            now.Add(-5 * time.Minute),
			LastHeartbeatAt:        now.Add(-9 * time.Second),
			RemoteAddr:             "127.0.0.1:local",
			ActiveSubstreams:       0,
			ActiveConsumerSessions: 0,
			Metadata: map[string]string{
				"environment": "local",
				"database":    "gram",
				"server":      "postgres-mcp",
				"transport":   "streamable-http",
			},
		},
	}, 0); err != nil {
		return fmt.Errorf("seed tunnel connection snapshot: %w", err)
	}

	fmt.Printf("Seeded tunnelled MCP runtime state for %s (key_prefix=%s)\n", serverID, keyPrefix)
	return nil
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
