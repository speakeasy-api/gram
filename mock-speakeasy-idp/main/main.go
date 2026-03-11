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
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid PORT: %s\n", v)
			os.Exit(1)
		}
		port = p
	}

	cfg := mockidp.DefaultConfig()
	handler := mockidp.Handler(cfg)

	mockidp.LogConfig(cfg, port)

	addr := fmt.Sprintf(":%d", port)
	if err := http.ListenAndServe(addr, handler); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
