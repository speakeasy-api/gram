// Command dev-mcp is a local-development MCP server that exposes assistant
// management operations (CRUD, running turns, triggers) over stdio. It lets
// coding agents exercise the assistant runtime against a locally running
// server without driving the dashboard UI.
//
// It authenticates by walking the same OIDC login flow the dashboard uses;
// the local dev-idp auto-approves, so no interaction is needed.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

func main() {
	serverURL := flag.String("server-url", "https://localhost:8080", "Base URL of the locally running server")
	insecure := flag.Bool("insecure", false, "Skip TLS certificate verification for the server URL")
	flag.Parse()

	// stdout carries the MCP protocol; all logging goes to stderr.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := run(*serverURL, *insecure, logger); err != nil {
		logger.Error("dev-mcp exited", attr.SlogError(err))
		os.Exit(1)
	}
}

func run(serverURL string, insecure bool, logger *slog.Logger) error {
	base, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Errorf("parse server url: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	api := newAPIClient(base, insecure, logger)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "assistants-dev",
		Title:   "Assistant local dev tools",
		Version: "0.1.0",
	}, nil)
	registerTools(server, api)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("run mcp server: %w", err)
	}
	return nil
}
