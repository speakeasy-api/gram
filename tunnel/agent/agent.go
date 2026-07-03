// Package agent reverse-proxies a pinned local MCP server over one outbound yamux/WebSocket tunnel.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"

	"github.com/speakeasy-api/gram/tunnel/wire"
)

// Agent lifecycle timing. Values here interact: keep yamux keepalive under the
// gateway's session/idle timeouts, and keep the stable-session threshold above
// the max backoff so flapping connections never reset to the minimum delay.
const (
	// defaultMinBackoff is the initial reconnect delay after a failed or short-lived session.
	defaultMinBackoff = 500 * time.Millisecond
	// defaultMaxBackoff caps exponential reconnect backoff.
	defaultMaxBackoff = 30 * time.Second
	// stableSessionThreshold resets backoff to the minimum once a session survives this long.
	stableSessionThreshold = 30 * time.Second
	// gatewayDialTimeout bounds the websocket dial and handshake to the gateway.
	gatewayDialTimeout = 15 * time.Second
	// yamuxKeepAliveInterval paces tunnel liveness pings; must stay under gateway idle timeouts.
	yamuxKeepAliveInterval = 15 * time.Second
	// substreamReadHeaderTimeout bounds header reads on gateway-opened substreams.
	substreamReadHeaderTimeout = 30 * time.Second
	// shutdownTimeout bounds graceful HTTP shutdown when the run context is cancelled.
	shutdownTimeout = 10 * time.Second
)

// maxHelloFrameBytes bounds the control hello read; hello carries small JSON metadata, never MCP payloads.
const maxHelloFrameBytes = 4 << 10

type Config struct {
	GatewayURL string
	APIKey     string
	// LocalMCPURL is pinned at startup; the gateway cannot redirect agent traffic.
	LocalMCPURL    string
	ServiceVersion string
	Metadata       map[string]string
	MinBackoff     time.Duration
	MaxBackoff     time.Duration
}

type Agent struct {
	cfg     Config
	target  *url.URL
	handler http.Handler
	logger  *slog.Logger
}

func New(cfg Config, logger *slog.Logger) (*Agent, error) {
	if strings.TrimSpace(cfg.ServiceVersion) == "" {
		return nil, errors.New("tunnel service version is required")
	}
	gatewayURL, err := normalizeGatewayURL(cfg.GatewayURL)
	if err != nil {
		return nil, err
	}
	cfg.GatewayURL = gatewayURL
	target, err := url.Parse(cfg.LocalMCPURL)
	if err != nil {
		return nil, err
	}
	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = defaultMinBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = defaultMaxBackoff
	}

	a := &Agent{cfg: cfg, target: target, logger: logger}
	a.handler = a.buildHandler(target)
	return a, nil
}

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
		// Reset backoff after a stable session; quick failures continue backing off.
		if time.Since(start) > stableSessionThreshold {
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
	header.Set(wire.HeaderTunnelServiceVersion, strings.TrimSpace(a.cfg.ServiceVersion))
	if len(a.cfg.Metadata) > 0 {
		metadata, err := json.Marshal(a.cfg.Metadata)
		if err != nil {
			return err
		}
		if len(metadata) > wire.MaxServiceMetadataBytes {
			return fmt.Errorf("tunnel metadata exceeds %d bytes serialized JSON", wire.MaxServiceMetadataBytes)
		}
		header.Set(wire.HeaderTunnelServiceMetadata, string(metadata))
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, gatewayDialTimeout)
	ws, resp, err := websocket.Dial(dialCtx, a.cfg.GatewayURL, &websocket.DialOptions{
		HTTPHeader: header,
	})
	cancelDial()
	if err != nil {
		if resp != nil {
			return &dialError{status: resp.StatusCode, err: err}
		}
		return err
	}
	a.logger.InfoContext(ctx, "tunnel agent connected", slog.String("gateway", a.cfg.GatewayURL))

	conn := websocket.NetConn(ctx, ws, websocket.MessageBinary)
	ycfg := yamux.DefaultConfig()
	ycfg.EnableKeepAlive = true
	ycfg.KeepAliveInterval = yamuxKeepAliveInterval
	// Disable yamux stderr logging; reconnect storms otherwise serialize on noisy writes.
	ycfg.LogOutput = io.Discard
	// Agent is the yamux server because the gateway opens per-request substreams.
	session, err := yamux.Server(conn, ycfg)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer conn.Close()
	defer session.Close()

	// Each yamux substream carries one HTTP exchange.
	srv := &http.Server{
		Handler:           a.handler,
		ReadHeaderTimeout: substreamReadHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
			_ = session.Close()
		case <-done:
		}
	}()
	err = srv.Serve(session)
	close(done)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func normalizeGatewayURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Hostname() == "" {
		return "", errors.New("TUNNEL_GATEWAY_URL must include a host")
	}
	switch u.Scheme {
	case "wss":
		return u.String(), nil
	case "https":
		u.Scheme = "wss"
		return u.String(), nil
	case "ws":
		if isLocalGatewayHost(u.Hostname()) {
			return u.String(), nil
		}
		return "", errors.New("TUNNEL_GATEWAY_URL must use wss:// unless it targets localhost or host.docker.internal")
	default:
		return "", errors.New("TUNNEL_GATEWAY_URL must use wss:// or https://")
	}
}

func isLocalGatewayHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "host.docker.internal":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *Agent) buildHandler(target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1 // stream SSE immediately
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalPath := req.URL.Path
		baseDirector(req)
		req.Host = target.Host
		if target.Path != "" && (originalPath == "" || originalPath == "/") {
			req.URL.Path = target.Path
			req.URL.RawPath = target.RawPath
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		a.logger.Warn("tunnel agent upstream error", slog.Any("error", err))
		w.WriteHeader(http.StatusBadGateway)
	}

	// Control paths terminate on the agent; all other requests hit the pinned MCP upstream.
	mux := http.NewServeMux()
	mux.HandleFunc(wire.ControlHelloPath, func(w http.ResponseWriter, r *http.Request) {
		var hello wire.HelloFrame
		_ = json.NewDecoder(io.LimitReader(r.Body, maxHelloFrameBytes)).Decode(&hello)
		a.logger.Info("tunnel hello received",
			slog.String("tunnel_id", hello.TunnelID), slog.String("session_id", hello.SessionID))
		w.WriteHeader(http.StatusOK)
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
