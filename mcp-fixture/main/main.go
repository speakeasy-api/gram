// Command mcp-fixture runs the throwaway local MCP fixture server. See the
// mcpfixture package for what it serves.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mcpfixture "github.com/speakeasy-api/gram/mcp-fixture"
	"github.com/speakeasy-api/gram/mcp-fixture/fixtureauth"
	"github.com/speakeasy-api/gram/tunnel/agent"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	address := flag.String("address", envOr("GRAM_MCP_FIXTURE_ADDRESS", "127.0.0.1:47321"), "listen address")
	baseURL := flag.String("base-url", os.Getenv("GRAM_MCP_FIXTURE_URL"), "external base URL (the OAuth issuer)")
	certFile := flag.String("ssl-cert-file", os.Getenv("GRAM_SSL_CERT_FILE"), "TLS certificate file")
	keyFile := flag.String("ssl-key-file", os.Getenv("GRAM_SSL_KEY_FILE"), "TLS key file")
	ttl := flag.Duration("access-token-ttl", durationEnv("GRAM_MCP_FIXTURE_ACCESS_TOKEN_TTL", time.Hour), "access token lifetime")
	suggested := flag.String("suggested-emails", envOr("GRAM_MCP_FIXTURE_SUGGESTED_EMAILS", "alex@fixture.test,sam@fixture.test"), "comma-separated emails offered on the sign-in page")
	// Both defaults read the stack's own settings rather than fixture-specific
	// copies: those are pinned per worktree, while a copy declared in mise.toml
	// keeps the primary worktree's port until git:worksync re-emits it.
	devIDPURL := flag.String("devidp-url", os.Getenv("GRAM_DEVIDP_EXTERNAL_URL"), "dev-idp external URL; serves the Enterprise Managed Authorization endpoint (empty: not served)")
	tunnelGateway := flag.String("tunnel-gateway-url", tunnelGatewayURL(os.Getenv("TUNNEL_GATEWAY_PUBLIC_PORT")), "local tunnel gateway to serve the open endpoint through (empty: no tunnel)")
	flag.Parse()

	useTLS, err := tlsMode(*certFile, *keyFile)
	if err != nil {
		logger.Error("invalid TLS configuration", "error", err)
		os.Exit(1)
	}
	if *baseURL == "" {
		*baseURL = defaultBaseURL(*address, useTLS)
	}
	if err := checkBaseURL(*baseURL, useTLS); err != nil {
		logger.Error("invalid base URL", "error", err)
		os.Exit(1)
	}

	fixture := mcpfixture.New(mcpfixture.Config{
		BaseURL:         *baseURL,
		AccessTokenTTL:  *ttl,
		SuggestedEmails: splitList(*suggested),
		HTTPClient:      &http.Client{Timeout: 10 * time.Second},
		Logger:          logger,
		DevIDPURL:       *devIDPURL,
	})
	srv := &http.Server{
		Addr:              *address,
		Handler:           fixture.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	if *tunnelGateway != "" {
		runTunnelAgent(ctx, logger, *tunnelGateway, *baseURL)
	}

	logger.Info("mcp fixture listening", "address", *address, "base_url", *baseURL)
	if useTLS {
		err = srv.ListenAndServeTLS(*certFile, *keyFile)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("mcp fixture stopped", "error", err)
		os.Exit(1)
	}
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// tunnelGatewayURL is the local tunnel gateway's agent endpoint on port, or
// empty (no tunnel) when the port is not set.
func tunnelGatewayURL(port string) string {
	if port == "" {
		return ""
	}
	return "ws://" + net.JoinHostPort("localhost", port) + "/connect"
}

func durationEnv(name string, def time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func splitList(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// tlsMode reports whether to serve TLS. Both files or neither: one alone is a
// misconfiguration, not a reason to fall back to plain HTTP.
func tlsMode(certFile, keyFile string) (bool, error) {
	if (certFile == "") != (keyFile == "") {
		return false, errors.New("set both --ssl-cert-file and --ssl-key-file, or neither")
	}
	return certFile != "", nil
}

// defaultBaseURL builds a client-facing URL for a listen address, mapping
// wildcard and port-only addresses to localhost.
func defaultBaseURL(address string, useTLS bool) string {
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return scheme + "://" + address
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

// checkBaseURL rejects an issuer whose scheme does not match the listener, so
// the OAuth endpoints the fixture advertises are the ones it serves.
func checkBaseURL(raw string, useTLS bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not an absolute URL", raw)
	}
	want := "http"
	if useTLS {
		want = "https"
	}
	if u.Scheme != want {
		return fmt.Errorf("base URL %q uses %s, but the listener serves %s", raw, u.Scheme, want)
	}
	if u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("base URL %q must be an origin, without a path, query or fragment", raw)
	}
	return nil
}

// runTunnelAgent serves the fixture's open endpoint through the local tunnel
// gateway with the checked-in key, as a customer's agent on a private network
// would. The agent retries until the gateway is up and the seed has stored
// the key's hash.
func runTunnelAgent(ctx context.Context, logger *slog.Logger, gatewayURL, baseURL string) {
	a, err := agent.New(agent.Config{
		GatewayURL:     gatewayURL,
		APIKey:         fixtureauth.TunnelKey,
		LocalMCPURL:    strings.TrimRight(baseURL, "/") + fixtureauth.PathOpenMCP,
		ServiceVersion: "mcp-fixture",
		Metadata:       map[string]string{"source": "mcp-fixture"},
		// Diagnostics are for real customer networks.
		DisableDiagnostics: true,
	}, logger.With("component", "tunnel-agent"))
	if err != nil {
		logger.Error("tunnel agent disabled", "error", err)
		return
	}
	go func() {
		if err := a.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("tunnel agent stopped", "error", err)
		}
	}()
}
