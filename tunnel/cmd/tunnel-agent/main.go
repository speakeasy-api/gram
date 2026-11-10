// Command tunnel-agent runs the customer-side outbound tunnel agent.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/speakeasy-api/gram/tunnel/agent"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

func main() {
	version := flag.Bool("version", false, "Print tunnel agent version")
	flag.Parse()
	if *version {
		fmt.Println(wire.AgentVersion)
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := agent.Config{
		GatewayURL:         os.Getenv("TUNNEL_GATEWAY_URL"),
		APIKey:             os.Getenv("TUNNEL_KEY"),
		LocalMCPURL:        os.Getenv("TUNNEL_LOCAL_MCP_URL"),
		ServiceVersion:     os.Getenv("TUNNEL_SERVICE_VERSION"),
		Metadata:           map[string]string{},
		DisableDiagnostics: os.Getenv("TUNNEL_DISABLE_DIAGNOSTICS") == "1",
	}
	if raw := os.Getenv("TUNNEL_METADATA"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.Metadata); err != nil {
			logger.Error("tunnel-agent invalid TUNNEL_METADATA; expected JSON object of string values", slog.Any("error", err))
			os.Exit(2)
		}
	}
	if cfg.GatewayURL == "" || cfg.APIKey == "" || cfg.LocalMCPURL == "" || cfg.ServiceVersion == "" {
		logger.Error("tunnel-agent missing config; require TUNNEL_GATEWAY_URL, TUNNEL_KEY, TUNNEL_LOCAL_MCP_URL, TUNNEL_SERVICE_VERSION")
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
		slog.String("agent_version", wire.AgentVersion),
		slog.String("gateway", wire.GatewayDisplay(cfg.GatewayURL)), slog.String("local_mcp", wire.TargetDisplay(cfg.LocalMCPURL)))
	if err := a.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("tunnel-agent exited", slog.Any("error", err))
		os.Exit(1)
	}
}
