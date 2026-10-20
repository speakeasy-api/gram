// Command tunnel-agent runs the customer-side outbound tunnel agent.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
		StdioCredentials: nil,
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
	switch mode := os.Getenv("TUNNEL_STDIO_CREDENTIALS"); mode {
	case "":
	case "user":
		creds := &agent.CredentialsConfig{
			Issuer:         os.Getenv("TUNNEL_IDENTITY_ISSUER"),
			Audience:       os.Getenv("TUNNEL_IDENTITY_AUDIENCE"),
			OrganizationID: os.Getenv("TUNNEL_IDENTITY_ORGANIZATION_ID"),
			JWKSURL:        os.Getenv("TUNNEL_IDENTITY_JWKS_URL"),
			AllowInsecure:  false,
			Root:           os.Getenv("TUNNEL_STDIO_CREDENTIALS_DIR"),
			MaxAge:         0,
		}
		if raw := os.Getenv("TUNNEL_IDENTITY_ALLOW_INSECURE"); raw != "" {
			allow, err := strconv.ParseBool(raw)
			if err != nil {
				logger.Error("tunnel-agent invalid TUNNEL_IDENTITY_ALLOW_INSECURE; expected true or false")
				os.Exit(2)
			}
			creds.AllowInsecure = allow
		}
		if raw := os.Getenv("TUNNEL_STDIO_CREDENTIALS_MAX_AGE"); raw != "" {
			d, err := time.ParseDuration(raw)
			if err != nil || d <= 0 {
				logger.Error("tunnel-agent invalid TUNNEL_STDIO_CREDENTIALS_MAX_AGE; expected a positive duration such as 1h")
				os.Exit(2)
			}
			creds.MaxAge = d
		}
		cfg.StdioCredentials = creds
	default:
		logger.Error("tunnel-agent invalid TUNNEL_STDIO_CREDENTIALS; the only supported value is user")
		os.Exit(2)
	}
	if raw := os.Getenv("TUNNEL_METADATA"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.Metadata); err != nil {
			logger.Error("tunnel-agent invalid TUNNEL_METADATA; expected JSON object of string values", slog.Any("error", err))
			os.Exit(2)
		}
	}
	if cfg.GatewayURL == "" || cfg.APIKey == "" || cfg.ServiceVersion == "" || (strings.TrimSpace(cfg.LocalMCPURL) == "") == (strings.TrimSpace(cfg.LocalMCPCommand) == "") {
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

	// Never log the command; it commonly carries credentials.
	upstream := slog.String("local_mcp", cfg.LocalMCPURL)
	if cfg.LocalMCPCommand != "" {
		upstream = slog.String("local_mcp", "stdio")
	}
	logger.Info("tunnel-agent starting", slog.String("gateway", cfg.GatewayURL), upstream, slog.Bool("stdio_credentials", cfg.StdioCredentials != nil))
	if err := a.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("tunnel-agent exited", slog.Any("error", err))
		os.Exit(1)
	}
}
