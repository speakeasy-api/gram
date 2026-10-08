// Command tunnel-agent runs the customer-side outbound tunnel agent.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/speakeasy-api/gram/tunnel/agent"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := agent.Config{
		GatewayURL:       os.Getenv("TUNNEL_GATEWAY_URL"),
		APIKey:           os.Getenv("TUNNEL_KEY"),
		LocalMCPURL:      os.Getenv("TUNNEL_LOCAL_MCP_URL"),
		LocalMCPCommand:  os.Getenv("TUNNEL_LOCAL_MCP_COMMAND"),
		StdioMaxSessions: 0,
		StdioIdleTimeout: 0,
		ServiceVersion:   os.Getenv("TUNNEL_SERVICE_VERSION"),
		Metadata:         map[string]string{},
		MinBackoff:       0,
		MaxBackoff:       0,
	}
	if raw := os.Getenv("TUNNEL_STDIO_MAX_SESSIONS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			logger.Error("tunnel-agent invalid TUNNEL_STDIO_MAX_SESSIONS; expected a positive integer")
			os.Exit(2)
		}
		cfg.StdioMaxSessions = n
	}
	if raw := os.Getenv("TUNNEL_STDIO_IDLE_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			logger.Error("tunnel-agent invalid TUNNEL_STDIO_IDLE_TIMEOUT; expected a positive duration such as 30m")
			os.Exit(2)
		}
		cfg.StdioIdleTimeout = d
	}
	if raw := os.Getenv("TUNNEL_METADATA"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.Metadata); err != nil {
			logger.Error("tunnel-agent invalid TUNNEL_METADATA; expected JSON object of string values", slog.Any("error", err))
			os.Exit(2)
		}
	}
	if cfg.GatewayURL == "" || cfg.APIKey == "" || cfg.ServiceVersion == "" || (cfg.LocalMCPURL == "") == (cfg.LocalMCPCommand == "") {
		logger.Error("tunnel-agent missing config; require TUNNEL_GATEWAY_URL, TUNNEL_KEY, TUNNEL_SERVICE_VERSION, and exactly one of TUNNEL_LOCAL_MCP_URL or TUNNEL_LOCAL_MCP_COMMAND")
		os.Exit(2)
	}

	a, err := agent.New(cfg, logger)
	if err != nil {
		logger.Error("tunnel-agent init failed", slog.Any("error", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The stdio command is not logged: it commonly carries credentials.
	upstream := slog.String("local_mcp", cfg.LocalMCPURL)
	if cfg.LocalMCPCommand != "" {
		upstream = slog.String("local_mcp", "stdio")
	}
	logger.Info("tunnel-agent starting", slog.String("gateway", cfg.GatewayURL), upstream)
	if err := a.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("tunnel-agent exited", slog.Any("error", err))
		os.Exit(1)
	}
}
