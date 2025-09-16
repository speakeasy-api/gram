package api

import (
	"net/http"
	"time"
)

// goaSharedHTTPClient is a singleton HTTP client for the CLI. Backend API
// should use this client as its main transport.
var goaSharedHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}
