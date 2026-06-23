// Package agent is the customer-side tunnel agent: it dials a single outbound
// WebSocket to the gateway, runs a yamux *server* over it (the gateway opens
// substreams, the agent accepts and serves them), and reverse-proxies each
// substream's HTTP exchange to one pinned local MCP URL. It requires only
// outbound connectivity and reconnects with jittered backoff.
package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"github.com/speakeasy-api/gram/tunnel/wire"
)

// Config configures an Agent.
type Config struct {
	// GatewayURL is the ws:// or wss:// connect endpoint, e.g.
	// wss://tunnel.gram.local/connect.
	GatewayURL string
	// APIKey is the per-tunnel key (gram_tunnel_...).
	APIKey string
	// LocalMCPURL is the single upstream the agent forwards to. The agent hard-
	// pins this; the control plane cannot redirect it (SSRF mitigation).
	LocalMCPURL string
	// MinBackoff / MaxBackoff bound the reconnect backoff.
	MinBackoff time.Duration
	MaxBackoff time.Duration
}

// Agent maintains the tunnel connection.
type Agent struct {
	cfg     Config
	target  *url.URL
	handler http.Handler
	logger  *slog.Logger
}

// New validates config and builds an Agent.
func New(cfg Config, logger *slog.Logger) (*Agent, error) {
	target, err := url.Parse(cfg.LocalMCPURL)
	if err != nil {
		return nil, err
	}
	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = 500 * time.Millisecond
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Second
	}

	a := &Agent{cfg: cfg, target: target, logger: logger}
	a.handler = a.buildHandler(target)
	return a, nil
}

// Run connects and serves until ctx is cancelled, reconnecting on failure with
// jittered exponential backoff. Jitter is mandatory: an ingress reload severs
// the whole fleet at once, so unjittered retries would stampede.
func (a *Agent) Run(ctx context.Context) error {
	backoff := a.cfg.MinBackoff
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		start := time.Now()
		if err := a.connectOnce(ctx); err != nil && ctx.Err() == nil {
			a.logger.WarnContext(ctx, "tunnel agent session ended", slog.Any("error", err))
		}
		// A long-lived session resets backoff; a fast failure grows it.
		if time.Since(start) > 30*time.Second {
			backoff = a.cfg.MinBackoff
		}
		wait := fullJitter(backoff)
		a.logger.InfoContext(ctx, "tunnel agent reconnecting", slog.Duration("in", wait))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		backoff = min(backoff*2, a.cfg.MaxBackoff)
	}
}

func (a *Agent) connectOnce(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	header.Set(wire.HeaderAgentVersion, wire.AgentVersion)

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	ws, resp, err := dialer.DialContext(ctx, a.cfg.GatewayURL, header)
	if err != nil {
		if resp != nil {
			return &dialError{status: resp.StatusCode, err: err}
		}
		return err
	}
	a.logger.InfoContext(ctx, "tunnel agent connected", slog.String("gateway", a.cfg.GatewayURL))

	conn := wire.NewWSConn(ws)
	ycfg := yamux.DefaultConfig()
	ycfg.EnableKeepAlive = true
	ycfg.KeepAliveInterval = 15 * time.Second
	// Silence yamux's default stderr logger: on reconnect storms it spams a line
	// per dropped connection, and the synchronized writes starve the hot path.
	ycfg.LogOutput = io.Discard
	// Agent is the yamux server: it Accepts substreams the gateway opens.
	session, err := yamux.Server(conn, ycfg)
	if err != nil {
		_ = ws.Close()
		return err
	}
	defer session.Close()

	// http.Serve over the session: every accepted substream is one HTTP
	// exchange (control frames included, by path prefix).
	srv := &http.Server{
		Handler:           a.handler,
		ReadHeaderTimeout: 30 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	return srv.Serve(session)
}

// buildHandler routes control paths to the agent itself and everything else to
// the pinned local MCP upstream.
func (a *Agent) buildHandler(target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1 // stream SSE immediately
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		a.logger.Warn("tunnel agent upstream error", slog.Any("error", err))
		w.WriteHeader(http.StatusBadGateway)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(wire.ControlHelloPath, func(w http.ResponseWriter, r *http.Request) {
		var hello wire.HelloFrame
		_ = json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&hello)
		a.logger.Info("tunnel hello received",
			slog.String("tunnel_id", hello.TunnelID), slog.String("session_id", hello.SessionID))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc(wire.ControlDrainPath, func(w http.ResponseWriter, _ *http.Request) {
		a.logger.Info("tunnel drain received; will reconnect after in-flight work")
		w.WriteHeader(http.StatusOK)
		// The gateway closes the session after draining; Serve returns and the
		// Run loop reconnects (landing on a surviving pod).
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, wire.ControlPathPrefix) {
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
	return mux
}

func fullJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(d)) + 1)
}

type dialError struct {
	status int
	err    error
}

func (e *dialError) Error() string {
	return "tunnel dial rejected: status " + http.StatusText(e.status) + ": " + e.err.Error()
}
