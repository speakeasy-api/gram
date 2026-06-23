// Command tunnel-gateway terminates agent WebSocket upgrades, owns the yamux
// sessions, and maps internal forward requests onto substreams by tunnel ID.
// POC: route store is Redis (if TUNNEL_REDIS_ADDR set) or in-memory; tunnel keys
// are seeded from TUNNEL_SEED_KEYS (no database).
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/tunnel/gateway"
	"github.com/speakeasy-api/gram/tunnel/route"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	listenAddr := envOr("TUNNEL_GATEWAY_ADDR", ":8090")
	// AdvertiseAddr is what gram-server uses to reach this pod; in k8s set it to
	// the pod IP + port via the downward API (see the manifest).
	advertiseAddr := envOr("TUNNEL_GATEWAY_ADVERTISE_ADDR", "tunnel-gateway:8090")

	routes := buildRouteStore(logger)
	keys := gateway.NewKeyStore(parseSeedKeys(os.Getenv("TUNNEL_SEED_KEYS")))

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

func buildRouteStore(logger *slog.Logger) route.Store {
	addr := os.Getenv("TUNNEL_REDIS_ADDR")
	if addr == "" {
		logger.Info("tunnel-gateway using in-memory route store")
		return route.NewMemory()
	}
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("TUNNEL_REDIS_PASSWORD"),
	})
	logger.Info("tunnel-gateway using redis route store", slog.String("addr", addr))
	return route.NewRedis(client)
}

// parseSeedKeys parses "tunnelID=gram_tunnel_xxx,tunnelID2=gram_tunnel_yyy".
func parseSeedKeys(raw string) map[string]string {
	out := map[string]string{}
	for pair := range strings.SplitSeq(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		id, key, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(id)] = strings.TrimSpace(key)
	}
	return out
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
