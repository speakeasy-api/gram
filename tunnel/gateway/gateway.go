package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
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
	// MaxStreamsPerTunnel caps concurrent substreams per agent session.
	MaxStreamsPerTunnel int
	ForwardToken        string
}

// Gateway terminates agent WebSocket upgrades, owns the yamux sessions, and
// maps internal forward requests onto substreams by tunnel ID.
type Gateway struct {
	cfg      Config
	keys     KeyResolver
	routes   route.Store
	reg      *registry
	logger   *slog.Logger
	upgrader websocket.Upgrader
}

// New builds a Gateway.
func New(cfg Config, keys KeyResolver, routes route.Store, logger *slog.Logger) *Gateway {
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

// PublicHandler returns the externally reachable agent routes. It intentionally
// does not mount the forward handler: public clients may connect agents, but
// only the internal listener may forward MCP traffic into a tunnel.
func (g *Gateway) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/connect", g.handleConnect)
	mux.HandleFunc("/healthz", healthz)
	return mux
}

// ForwardHandler returns the internal gram-server -> gateway forwarding routes.
func (g *Gateway) ForwardHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/", g.handleForward)
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
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
	authHeader := r.Header.Get("Authorization")
	presentedKeyHash := hashBearerKey(authHeader)
	tunnelID, ok, err := g.keys.Resolve(r.Context(), authHeader)
	if err != nil {
		g.logger.ErrorContext(r.Context(), "tunnel connect key lookup failed", slog.Any("error", err))
		http.Error(w, "tunnel key lookup failed", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		g.logger.WarnContext(r.Context(), "tunnel connect rejected", slog.String("reason", "auth"))
		http.Error(w, "unauthorized tunnel key", http.StatusUnauthorized)
		return
	}

	agentVersion := r.Header.Get(wire.HeaderAgentVersion)
	serviceID := strings.TrimSpace(r.Header.Get(wire.HeaderTunnelServiceID))
	serviceSlug := strings.TrimSpace(r.Header.Get(wire.HeaderTunnelServiceSlug))
	serviceVersion := strings.TrimSpace(r.Header.Get(wire.HeaderTunnelServiceVersion))
	if serviceID == "" || serviceSlug == "" || serviceVersion == "" {
		g.logger.WarnContext(r.Context(), "tunnel connect rejected",
			slog.String("reason", "missing-service-identity"), slog.String("tunnel_id", tunnelID))
		http.Error(w, "missing tunnel service identity", http.StatusBadRequest)
		return
	}
	metadata, err := parseServiceMetadata(r.Header.Get(wire.HeaderTunnelServiceMetadata))
	if err != nil {
		g.logger.WarnContext(r.Context(), "tunnel connect rejected",
			slog.String("reason", "metadata"), slog.String("tunnel_id", tunnelID), slog.Any("error", err))
		status := http.StatusBadRequest
		if errors.Is(err, errServiceMetadataTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}
	if recorder, ok := g.keys.(ConnectionRecorder); ok {
		if err := recorder.MarkConnected(r.Context(), tunnelID, presentedKeyHash, agentVersion); err != nil {
			g.logger.ErrorContext(r.Context(), "tunnel connect activation failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			http.Error(w, "tunnel activation failed", http.StatusServiceUnavailable)
			return
		}
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
	now := time.Now().UTC()
	remove := g.reg.add(tunnelID, sessionID, session, route.Connection{
		SessionID:              sessionID,
		ServiceID:              serviceID,
		ServiceSlug:            serviceSlug,
		ServiceVersion:         serviceVersion,
		AgentVersion:           agentVersion,
		ConnectedAt:            now,
		LastHeartbeatAt:        now,
		RemoteAddr:             r.RemoteAddr,
		ActiveSubstreams:       0,
		ActiveConsumerSessions: 0,
		Metadata:               metadata,
	})
	if err := g.routes.Publish(r.Context(), tunnelID, g.cfg.AdvertiseAddr, routeTTL); err != nil {
		g.logger.WarnContext(r.Context(), "tunnel route publish failed", slog.Any("error", err))
	}
	g.publishConnectionSnapshot(r.Context(), tunnelID, now)
	g.logger.InfoContext(r.Context(), "tunnel connected",
		slog.String("tunnel_id", tunnelID), slog.String("session_id", sessionID),
		slog.String("agent_version", agentVersion), slog.Int("active", g.reg.activeSessions()))

	// Greet the agent over a substream (best effort; informational).
	go g.sayHello(session, tunnelID, sessionID)

	// Refresh the route until the session dies.
	stop := make(chan struct{})
	go g.refreshSessionState(tunnelID, presentedKeyHash, session, stop)

	// Block until the session closes (agent disconnect / network drop).
	<-session.CloseChan()
	close(stop)
	remove()
	if g.reg.tunnelSessionCount(tunnelID) == 0 {
		if err := g.routes.Delete(context.Background(), tunnelID); err != nil {
			g.logger.WarnContext(context.Background(), "tunnel route delete failed", slog.Any("error", err))
		}
		g.deleteConnectionSnapshot(context.Background(), tunnelID)
	} else {
		g.publishConnectionSnapshot(context.Background(), tunnelID, time.Now().UTC())
	}
	g.logger.InfoContext(context.Background(), "tunnel disconnected",
		slog.String("tunnel_id", tunnelID), slog.String("session_id", sessionID),
		slog.Int("active", g.reg.activeSessions()))
}

func (g *Gateway) refreshSessionState(tunnelID, keyHash string, session *yamux.Session, stop <-chan struct{}) {
	t := time.NewTicker(routeTTL / 2)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if checker, ok := g.keys.(ActiveTunnelChecker); ok {
				active, err := checker.IsActive(context.Background(), tunnelID, keyHash)
				if err != nil {
					g.logger.WarnContext(context.Background(), "tunnel active check failed",
						slog.String("tunnel_id", tunnelID), slog.Any("error", err))
					continue
				}
				if !active {
					g.logger.InfoContext(context.Background(), "tunnel session no longer active",
						slog.String("tunnel_id", tunnelID))
					_ = session.Close()
					return
				}
			}
			if err := g.routes.Publish(context.Background(), tunnelID, g.cfg.AdvertiseAddr, routeTTL); err != nil {
				g.logger.WarnContext(context.Background(), "tunnel route refresh failed",
					slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			}
			g.publishConnectionSnapshot(context.Background(), tunnelID, time.Now().UTC())
		}
	}
}

func hashBearerKey(bearer string) string {
	key := strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	return wire.HashKey(key)
}

func (g *Gateway) publishConnectionSnapshot(ctx context.Context, tunnelID string, heartbeatAt time.Time) {
	store, ok := g.routes.(route.ConnectionSnapshotStore)
	if !ok {
		return
	}
	if err := store.PublishConnections(ctx, tunnelID, g.reg.connections(tunnelID, heartbeatAt), routeTTL); err != nil {
		g.logger.WarnContext(ctx, "tunnel connection snapshot publish failed",
			slog.String("tunnel_id", tunnelID), slog.Any("error", err))
	}
}

func (g *Gateway) deleteConnectionSnapshot(ctx context.Context, tunnelID string) {
	store, ok := g.routes.(route.ConnectionSnapshotStore)
	if !ok {
		return
	}
	if err := store.DeleteConnections(ctx, tunnelID); err != nil {
		g.logger.WarnContext(ctx, "tunnel connection snapshot delete failed",
			slog.String("tunnel_id", tunnelID), slog.Any("error", err))
	}
}

var errServiceMetadataTooLarge = errors.New("tunnel service metadata exceeds 1024 bytes serialized JSON")

func parseServiceMetadata(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}, nil
	}
	if len([]byte(raw)) > wire.MaxServiceMetadataBytes {
		return nil, errServiceMetadataTooLarge
	}

	var metadata map[string]string
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil, err
	}
	for key, value := range metadata {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			delete(metadata, key)
		}
	}
	return metadata, nil
}

func (g *Gateway) sayHello(session *yamux.Session, tunnelID, sessionID string) {
	body, _ := json.Marshal(wire.HelloFrame{
		Type:      "hello",
		TunnelID:  tunnelID,
		SessionID: sessionID,
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
	if g.cfg.ForwardToken != "" {
		presented := r.Header.Get(wire.HeaderTunnelForwardToken)
		if !wire.ConstantTimeEqual(presented, g.cfg.ForwardToken) {
			g.logger.WarnContext(r.Context(), "tunnel forward rejected", slog.String("reason", "forward-token"))
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	r.Header.Del(wire.HeaderTunnelForwardToken)

	tunnelID := r.Header.Get(wire.HeaderTunnelID)
	if tunnelID == "" {
		http.Error(w, "missing tunnel id", http.StatusBadRequest)
		return
	}
	consumerSession := strings.TrimSpace(r.Header.Get(wire.HeaderTunnelConsumerSession))
	entry, ok := g.reg.beginForward(tunnelID, consumerSession, time.Now().UTC(), g.cfg.MaxStreamsPerTunnel)
	if !ok {
		// Distinct 502 variant: tunnel known but no live agent session.
		w.Header().Set("X-Gram-Tunnel-Error", "no-live-session")
		http.Error(w, "tunnel has no live agent session", http.StatusBadGateway)
		return
	}
	r.Header.Del(wire.HeaderTunnelID)
	r.Header.Del(wire.HeaderTunnelConsumerSession)
	g.publishConnectionSnapshot(r.Context(), tunnelID, time.Now().UTC())
	defer func() {
		g.reg.finishForward(entry, time.Now().UTC())
		g.publishConnectionSnapshot(context.Background(), tunnelID, time.Now().UTC())
	}()

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "tunnel" // ignored; substreamTransport dials the session
		},
		Transport:     substreamTransport(entry.session),
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

// RevokeTunnel kills all sessions for a tunnel and clears its route. Durable
// key state is owned by the resolver backing the gateway.
func (g *Gateway) RevokeTunnel(ctx context.Context, tunnelID string) int {
	if revoker, ok := g.keys.(interface{ Revoke(string) }); ok {
		revoker.Revoke(tunnelID)
	}
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
