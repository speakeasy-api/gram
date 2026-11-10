// Command mcp-fixture runs the throwaway local MCP fixture server. See the
// mcpfixture package for what it serves.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mcpfixture "github.com/speakeasy-api/gram/mcp-fixture"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	address := flag.String("address", envOr("GRAM_MCP_FIXTURE_ADDRESS", "127.0.0.1:47321"), "listen address")
	baseURL := flag.String("base-url", os.Getenv("GRAM_MCP_FIXTURE_URL"), "external base URL (the OAuth issuer)")
	certFile := flag.String("ssl-cert-file", os.Getenv("GRAM_SSL_CERT_FILE"), "TLS certificate file")
	keyFile := flag.String("ssl-key-file", os.Getenv("GRAM_SSL_KEY_FILE"), "TLS key file")
	ttl := flag.Duration("access-token-ttl", durationEnv("GRAM_MCP_FIXTURE_ACCESS_TOKEN_TTL", time.Hour), "access token lifetime")
	suggested := flag.String("suggested-emails", envOr("GRAM_MCP_FIXTURE_SUGGESTED_EMAILS", "alex@fixture.test,sam@fixture.test"), "comma-separated emails offered on the sign-in page")
	flag.Parse()

	if *baseURL == "" {
		scheme := "https"
		if *certFile == "" {
			scheme = "http"
		}
		*baseURL = scheme + "://" + *address
	}

	fixture := mcpfixture.New(mcpfixture.Config{
		BaseURL:         *baseURL,
		AccessTokenTTL:  *ttl,
		SuggestedEmails: splitList(*suggested),
		HTTPClient:      &http.Client{Timeout: 10 * time.Second},
		Logger:          logger,
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

	logger.Info("mcp fixture listening", "address", *address, "base_url", *baseURL)
	var err error
	if *certFile != "" && *keyFile != "" {
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
