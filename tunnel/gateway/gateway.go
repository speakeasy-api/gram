package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

// routeTTL is the lifetime of a published route; refreshed at half this.
const routeTTL = 30 * time.Second

// Config configures a Gateway.
type Config struct {
	// AdvertiseAddr is the internal host:port other pods (gram-server) reach
	// this gateway on to forward requests (published into the route store).
	AdvertiseAddr string
	// MinAgentVersion is the floor enforced at connect.
	MinAgentVersion string
	// MaxStreamsPerTunnel caps concurrent substreams per agent session.
	MaxStreamsPerTunnel int
}

// Gateway terminates agent WebSocket upgrades, owns the yamux sessions, and
// maps internal forward requests onto substreams by tunnel ID.
type Gateway struct {
	cfg      Config
	keys     *KeyStore
	routes   route.Store
	reg      *registry
	logger   *slog.Logger
	upgrader websocket.Upgrader
}

// New builds a Gateway.
func New(cfg Config, keys *KeyStore, routes route.Store, logger *slog.Logger) *Gateway {
	if cfg.MinAgentVersion == "" {
		cfg.MinAgentVersion = wire.MinSupportedAgentVersion
	}
	if cfg.MaxStreamsPerTunnel <= 0 {
		cfg.MaxStreamsPerTunnel = 256
	}
	return &Gateway{
		cfg:    cfg,
		keys:   keys,
		routes: routes,
		reg:    newRegistry(),
		logger: logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  32 * 1024,
			WriteBufferSize: 32 * 1024,
			// Agents are non-browser clients; origin checks are not meaningful.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

// Handler returns the gateway's HTTP routes: /connect for agent upgrades, and a
// catch-all forward handler (keyed by the tunnel-ID header) for internal
// pod-to-pod requests from gram-server.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/connect", g.handleConnect)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", g.handleForward)
	return mux
}

// ActiveSessions exposes the registered session count for metrics/tests.
func (g *Gateway) ActiveSessions() int { return g.reg.activeSessions() }

// SetAdvertiseAddr overrides the internal forward address published into the
// route store. Used when the listen address is only known after binding (tests,
// downward-API wiring).
func (g *Gateway) SetAdvertiseAddr(addr string) { g.cfg.AdvertiseAddr = addr }

// handleConnect authenticates the agent, upgrades to WebSocket, runs a yamux
// client session over it, registers the session, and keeps the route fresh
// until the session closes.
func (g *Gateway) handleConnect(w http.ResponseWriter, r *http.Request) {
	tunnelID, ok := g.keys.Resolve(r.Header.Get("Authorization"))
	if !ok {
		g.logger.WarnContext(r.Context(), "tunnel connect rejected", slog.String("reason", "auth"))
		http.Error(w, "unauthorized tunnel key", http.StatusUnauthorized)
		return
	}

	agentVersion := r.Header.Get(wire.HeaderAgentVersion)
	if err := wire.CheckMinVersion(agentVersion, g.cfg.MinAgentVersion); err != nil {
		g.logger.WarnContext(r.Context(), "tunnel connect rejected",
			slog.String("reason", "version"), slog.String("tunnel_id", tunnelID), slog.Any("error", err))
		http.Error(w, err.Error(), http.StatusUpgradeRequired)
		return
	}

	ws, err := g.upgrader.Upgrade(w, r, nil)
	if err != nil {
		g.logger.WarnContext(r.Context(), "tunnel websocket upgrade failed", slog.Any("error", err))
		return // Upgrade already wrote a response.
	}

	conn := wire.NewWSConn(ws)
	ycfg := yamux.DefaultConfig()
	ycfg.EnableKeepAlive = true
	ycfg.KeepAliveInterval = 15 * time.Second
	ycfg.LogOutput = yamuxLogOutput
	// Gateway opens substreams; agent accepts and serves them.
	session, err := yamux.Client(conn, ycfg)
	if err != nil {
		g.logger.ErrorContext(r.Context(), "tunnel yamux client failed", slog.Any("error", err))
		_ = ws.Close()
		return
	}

	sessionID := uuid.NewString()
	remove := g.reg.add(tunnelID, sessionID, session)
	if err := g.routes.Publish(r.Context(), tunnelID, g.cfg.AdvertiseAddr, routeTTL); err != nil {
		g.logger.WarnContext(r.Context(), "tunnel route publish failed", slog.Any("error", err))
	}
	g.logger.InfoContext(r.Context(), "tunnel connected",
		slog.String("tunnel_id", tunnelID), slog.String("session_id", sessionID),
		slog.String("agent_version", agentVersion), slog.Int("active", g.reg.activeSessions()))

	// Greet the agent over a substream (best effort; informational).
	go g.sayHello(session, tunnelID, sessionID)

	// Refresh the route until the session dies.
	stop := make(chan struct{})
	go g.refreshRoute(tunnelID, stop)

	// Block until the session closes (agent disconnect / network drop).
	<-session.CloseChan()
	close(stop)
	remove()
	if err := g.routes.Delete(context.Background(), tunnelID); err != nil {
		g.logger.WarnContext(context.Background(), "tunnel route delete failed", slog.Any("error", err))
	}
	g.logger.InfoContext(context.Background(), "tunnel disconnected",
		slog.String("tunnel_id", tunnelID), slog.String("session_id", sessionID),
		slog.Int("active", g.reg.activeSessions()))
}

func (g *Gateway) refreshRoute(tunnelID string, stop <-chan struct{}) {
	t := time.NewTicker(routeTTL / 2)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if err := g.routes.Publish(context.Background(), tunnelID, g.cfg.AdvertiseAddr, routeTTL); err != nil {
				g.logger.WarnContext(context.Background(), "tunnel route refresh failed",
					slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			}
		}
	}
}

func (g *Gateway) sayHello(session *yamux.Session, tunnelID, sessionID string) {
	body, _ := json.Marshal(wire.HelloFrame{
		Type:       "hello",
		TunnelID:   tunnelID,
		SessionID:  sessionID,
		MinVersion: g.cfg.MinAgentVersion,
	})
	client := &http.Client{Transport: substreamTransport(session), Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, "http://tunnel"+wire.ControlHelloPath, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		g.logger.Warn("tunnel hello failed", slog.String("tunnel_id", tunnelID), slog.Any("error", err))
		return
	}
	_ = resp.Body.Close()
}

// handleForward maps an internal request onto a substream of the named tunnel.
// The tunnel ID arrives in a header set by gram-server (never by an external
// caller); see the tenant-isolation invariant in the design.
func (g *Gateway) handleForward(w http.ResponseWriter, r *http.Request) {
	tunnelID := r.Header.Get(wire.HeaderTunnelID)
	if tunnelID == "" {
		http.Error(w, "missing tunnel id", http.StatusBadRequest)
		return
	}
	session, ok := g.reg.pick(tunnelID)
	if !ok {
		// Distinct 502 variant: tunnel known but no live agent session.
		w.Header().Set("X-Gram-Tunnel-Error", "no-live-session")
		http.Error(w, "tunnel has no live agent session", http.StatusBadGateway)
		return
	}
	r.Header.Del(wire.HeaderTunnelID)

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "tunnel" // ignored; substreamTransport dials the session
		},
		Transport:     substreamTransport(session),
		FlushInterval: -1, // stream SSE immediately
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, err error) {
			g.logger.Warn("tunnel forward failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			rw.Header().Set("X-Gram-Tunnel-Error", "substream-failed")
			rw.WriteHeader(http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

// RevokeTunnel kills all sessions for a tunnel and clears its route. Exposed for
// a revoke endpoint / test.
func (g *Gateway) RevokeTunnel(ctx context.Context, tunnelID string) int {
	g.keys.Revoke(tunnelID)
	_ = g.routes.Delete(ctx, tunnelID)
	return g.reg.kill(tunnelID)
}

// substreamTransport returns an http.RoundTripper that opens a fresh yamux
// substream per request. Keepalives are disabled so DialContext (and therefore
// a new substream) is invoked for every request, and the substream is torn down
// when the exchange completes.
func substreamTransport(session *yamux.Session) *http.Transport {
	return &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return session.Open()
		},
	}
}
