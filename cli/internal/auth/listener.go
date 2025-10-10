package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	minPort         = 1024
	maxPort         = 65535
	callbackTimeout = 5 * time.Minute
)

// Listener manages an HTTP server that waits for OAuth callback.
type Listener struct {
	server   *http.Server
	listener net.Listener
	apiKey   chan string
	errChan  chan error
}

// NewListener creates a new callback listener on an available port.
func NewListener() (*Listener, error) {
	ln, err := findAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("failed to find available port: %w", err)
	}

	l := &Listener{
		server:   nil,
		listener: ln,
		apiKey:   make(chan string, 1),
		errChan:  make(chan error, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", l.handleCallback)

	l.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return l, nil
}

// URL returns the callback URL for this listener.
func (l *Listener) URL() string {
	addr, ok := l.listener.Addr().(*net.TCPAddr)
	if !ok {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d/callback", addr.Port)
}

// Start begins listening for callbacks.
func (l *Listener) Start() {
	go func() {
		if err := l.server.Serve(l.listener); err != nil && err != http.ErrServerClosed {
			l.errChan <- fmt.Errorf("server error: %w", err)
		}
	}()
}

// Wait blocks until an API key is received or timeout occurs.
func (l *Listener) Wait(ctx context.Context) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, callbackTimeout)
	defer cancel()

	select {
	case key := <-l.apiKey:
		return key, nil
	case err := <-l.errChan:
		return "", err
	case <-timeoutCtx.Done():
		return "", fmt.Errorf("timeout waiting for authentication callback")
	}
}

// Stop gracefully shuts down the server.
func (l *Listener) Stop(ctx context.Context) error {
	if l.server == nil {
		return nil
	}
	if err := l.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	return nil
}

func (l *Listener) handleCallback(w http.ResponseWriter, r *http.Request) {
	apiKey := r.URL.Query().Get("api_key")
	if apiKey == "" {
		http.Error(w, "missing api_key parameter", http.StatusBadRequest)
		return
	}

	l.apiKey <- apiKey

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "<html><body><h1>Authentication successful!</h1><p>You can close this window.</p></body></html>")
}

func findAvailablePort() (net.Listener, error) {
	// Let the OS pick an available port in the ephemeral range
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	// Verify it's in our acceptable range
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return nil, fmt.Errorf("unexpected address type")
	}
	if addr.Port < minPort || addr.Port > maxPort {
		_ = ln.Close()
		return nil, fmt.Errorf("port %d outside acceptable range", addr.Port)
	}

	return ln, nil
}
