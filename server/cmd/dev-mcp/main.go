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
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

func main() {
	defaultServerURL := os.Getenv("GRAM_SERVER_URL")
	if defaultServerURL == "" {
		defaultServerURL = "https://localhost:8080"
	}
	serverURL := flag.String("server-url", defaultServerURL, "Base URL of the locally running server")
	siteURL := flag.String("site-url", os.Getenv("GRAM_SITE_URL"), "Dashboard URL used as the refresh Origin (defaults to server URL)")
	insecure := flag.Bool("insecure", false, "Skip TLS certificate verification for the server URL")
	flag.Parse()

	// stdout carries the MCP protocol; all logging goes to stderr.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := run(*serverURL, *siteURL, *insecure, logger); err != nil {
		logger.Error("dev-mcp exited", attr.SlogError(err))
		os.Exit(1)
	}
}

func run(serverURL, siteURL string, insecure bool, logger *slog.Logger) error {
	base, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Errorf("parse server url: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if siteURL == "" {
		siteURL = serverURL
	}
	site, err := url.Parse(siteURL)
	if err != nil || site.Host == "" || (site.Scheme != "https" && site.Scheme != "http") {
		return fmt.Errorf("invalid site URL")
	}
	// Match browser Origin serialization for explicit default ports.
	if (site.Scheme == "https" && site.Port() == "443") || (site.Scheme == "http" && site.Port() == "80") {
		site.Host = site.Hostname()
		if strings.Contains(site.Host, ":") {
			site.Host = "[" + site.Host + "]"
		}
	}
	api := newAPIClient(base, site.Scheme+"://"+site.Host, insecure, logger)

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
