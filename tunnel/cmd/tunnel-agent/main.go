// Command tunnel-agent is the customer-side agent. Outbound-only: it dials the
// gateway WebSocket and reverse-proxies substream HTTP to one pinned local MCP
// URL. POC config comes from env.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/speakeasy-api/gram/tunnel/agent"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := agent.Config{
		GatewayURL:  os.Getenv("TUNNEL_GATEWAY_URL"),
		APIKey:      os.Getenv("TUNNEL_KEY"),
		LocalMCPURL: os.Getenv("TUNNEL_LOCAL_MCP_URL"),
	}
	if cfg.GatewayURL == "" || cfg.APIKey == "" || cfg.LocalMCPURL == "" {
		logger.Error("tunnel-agent missing config; require TUNNEL_GATEWAY_URL, TUNNEL_KEY, TUNNEL_LOCAL_MCP_URL")
		os.Exit(2)
	}

	a, err := agent.New(cfg, logger)
	if err != nil {
		logger.Error("tunnel-agent init failed", slog.Any("error", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("tunnel-agent starting",
		slog.String("gateway", cfg.GatewayURL), slog.String("local_mcp", cfg.LocalMCPURL))
	if err := a.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("tunnel-agent exited", slog.Any("error", err))
		os.Exit(1)
	}
}
