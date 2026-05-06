// Command marketplace runs the Gram marketplace proxy.
//
// It exposes two surfaces backed by a private GitHub repo provisioned for a
// Gram project (via plugin_github_connections):
//
//   - GET /m/{token}/marketplace.json — URL-based Claude Code marketplace
//   - /p/{token}.git/...               — git Smart HTTP proxy for plugin sources
//
// The token is the marketplace_token column on plugin_github_connections,
// minted at marketplace setup. The proxy resolves the token to a connection,
// mints a GitHub App installation token for the linked installation, and
// fronts the upstream private repo's contents over Smart HTTP.
//
// Configuration via env vars:
//
//	GRAM_DATABASE_URL                   - Postgres connection string (required)
//	GRAM_PLUGINS_GITHUB_APP_ID          - GitHub App ID (required)
//	GRAM_PLUGINS_GITHUB_PRIVATE_KEY     - GitHub App private key PEM (required)
//	GRAM_MARKETPLACE_LISTEN             - listen address (default :8080)
//	GRAM_MARKETPLACE_PUBLIC_URL         - public base URL embedded in the
//	                                      rendered marketplace.json
//	                                      (default http://localhost:8080)
//	GRAM_MARKETPLACE_MIRROR_DIR         - where to keep bare mirrors
//	                                      (default ./tmp/mirrors)
//	GRAM_MARKETPLACE_FETCH_AGE          - max mirror staleness before refetch
//	                                      (default 30s)
package main

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

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := loadConfig()
	if err != nil {
		logger.Error("config", slog.Any("error", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		logger.Error("connect database", slog.Any("error", err))
		os.Exit(1)
	}
	defer pool.Close()

	gh, err := ghclient.NewClient(cfg.githubAppID, []byte(cfg.githubPrivateKey), &guardian.HTTPClient{Timeout: 30 * time.Second})
	if err != nil {
		logger.Error("create github client", slog.Any("error", err))
		os.Exit(1)
	}

	resolver := marketplace.NewDBResolver(pool, gh)
	mirror := marketplace.NewMirror(cfg.mirrorDir, logger)
	server := marketplace.NewServer(resolver, mirror, cfg.publicURL, cfg.fetchAge, logger)

	httpServer := &http.Server{
		Addr:              cfg.listen,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("marketplace proxy listening",
		slog.String("addr", cfg.listen),
		slog.String("public_url", cfg.publicURL),
		slog.String("mirror_dir", cfg.mirrorDir),
	)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server", slog.Any("error", err))
		os.Exit(1)
	}
}

type config struct {
	databaseURL      string
	githubAppID      int64
	githubPrivateKey string
	listen           string
	publicURL        string
	mirrorDir        string
	fetchAge         time.Duration
}

func loadConfig() (config, error) {
	cfg := config{
		listen:    envOr("GRAM_MARKETPLACE_LISTEN", ":8080"),
		publicURL: envOr("GRAM_MARKETPLACE_PUBLIC_URL", "http://localhost:8080"),
		mirrorDir: envOr("GRAM_MARKETPLACE_MIRROR_DIR", "./tmp/mirrors"),
		fetchAge:  parseDurationOr(os.Getenv("GRAM_MARKETPLACE_FETCH_AGE"), 30*time.Second),
	}

	cfg.databaseURL = os.Getenv("GRAM_DATABASE_URL")
	if cfg.databaseURL == "" {
		return cfg, errors.New("GRAM_DATABASE_URL must be set")
	}

	appID, err := parseInt64(os.Getenv("GRAM_PLUGINS_GITHUB_APP_ID"))
	if err != nil {
		return cfg, errors.New("GRAM_PLUGINS_GITHUB_APP_ID must be set to a numeric App ID")
	}
	cfg.githubAppID = appID

	cfg.githubPrivateKey = os.Getenv("GRAM_PLUGINS_GITHUB_PRIVATE_KEY")
	if cfg.githubPrivateKey == "" {
		return cfg, errors.New("GRAM_PLUGINS_GITHUB_PRIVATE_KEY must be set (PEM-encoded)")
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDurationOr(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

func parseInt64(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("non-numeric")
		}
		n = n*10 + int64(r-'0')
	}
	return n, nil
}
