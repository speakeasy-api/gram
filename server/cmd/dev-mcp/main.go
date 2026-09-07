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
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
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
	base, origin, err := clientURLs(serverURL, siteURL)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	api := newAPIClient(base, origin, insecure, logger)

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

// clientURLs validates configuration before any HTTP client is constructed.
// Insecure mode relaxes certificate verification, never the HTTPS requirement.
func clientURLs(serverURL, siteURL string) (*url.URL, string, error) {
	base, err := url.Parse(serverURL)
	if err != nil {
		return nil, "", fmt.Errorf("parse server url: %w", err)
	}
	if _, err := canonicalOrigin(base); err != nil || base.Scheme != "https" {
		return nil, "", fmt.Errorf("server URL must be an absolute HTTPS URL")
	}
	if siteURL == "" {
		siteURL = serverURL
	}
	site, err := url.Parse(siteURL)
	if err != nil {
		return nil, "", fmt.Errorf("invalid site URL: %w", err)
	}
	origin, err := canonicalOrigin(site)
	if err != nil {
		return nil, "", fmt.Errorf("invalid site URL: %w", err)
	}
	return base, origin, nil
}

// canonicalOrigin matches browser Origin serialization for host casing and
// numeric ports, including zero-padded default ports. Paths are not part of it.
func canonicalOrigin(u *url.URL) (string, error) {
	scheme, host := strings.ToLower(u.Scheme), strings.ToLower(u.Hostname())
	if (scheme != "https" && scheme != "http") || host == "" || u.User != nil {
		return "", fmt.Errorf("expected an absolute HTTP(S) URL without user information")
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return "", fmt.Errorf("invalid port: %w", err)
		}
		port = strconv.FormatUint(n, 10)
		if (scheme == "https" && n == 443) || (scheme == "http" && n == 80) {
			port = ""
		}
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host, nil
}
