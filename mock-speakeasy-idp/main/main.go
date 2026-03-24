// Command mock-speakeasy-idp runs the mock Speakeasy IDP as a standalone HTTP
// server for local development.
package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"
)

func main() {
	port := 35291
	if v := os.Getenv("MOCK_IDP_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid MOCK_IDP_PORT: %s\n", v)
			os.Exit(1)
		}
		port = p
	}

	host := os.Getenv("MOCK_IDP_HOST")

	cfg := mockidp.DefaultConfig()
	// When OIDC mode is active but no explicit OIDC_EXTERNAL_URL is set,
	// derive it from the mock IDP listen address.
	if cfg.Oidc.IsOidcMode() && cfg.Oidc.ExternalURL == "" {
		cfg.Oidc.ExternalURL = fmt.Sprintf("http://%s:%d", host, port)
	}
	handler := mockidp.Handler(cfg)

	mockidp.LogConfig(cfg, port)

	addr := fmt.Sprintf("%s:%d", host, port)
	if err := http.ListenAndServe(addr, handler); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
