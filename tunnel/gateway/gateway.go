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

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/hashicorp/yamux"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

const (
	routeTTL              = 30 * time.Second
	routeOperationTimeout = 5 * time.Second
)

// defaultMaxSessions caps live agent sessions per gateway process. Loopback
// benchmarks (2026-07) held 16k live sessions with flat ~4ms forward p50 and
// ~120KB heap per session; past resource exhaustion, agent reconnect storms
// killed established sessions too. 10k leaves headroom to shed instead.
const defaultMaxSessions = 10_000

// defaultMaxStreamsPerTunnel caps concurrent yamux substreams multiplexed over
// a single agent session. It bounds per-tunnel fan-out so one busy source can't
// starve the session. Callers that leave Config.MaxStreamsPerTunnel unset (0)
// fall back to this value.
const defaultMaxStreamsPerTunnel = 256

var errMissingForwardToken = errors.New("tunnel gateway forward token is required")

type Config struct {
	// AdvertiseAddr is the internal gram-server -> gateway address published in Redis.
	AdvertiseAddr       string
	MaxStreamsPerTunnel int
	// MaxSessions bounds live agent sessions; connects beyond it shed with 503
	// so load moves to sibling gateway pods via agent retry.
	MaxSessions  int
	ForwardToken string
}

// Gateway owns live agent yamux sessions and maps internal forwards to substreams.
type Gateway struct {
	cfg    Config
	keys   KeyResolver
	routes route.Store
	reg    *registry
	logger *slog.Logger
}

func New(cfg Config, keys KeyResolver, routes route.Store, logger *slog.Logger) (*Gateway, error) {
	cfg.ForwardToken = strings.TrimSpace(cfg.ForwardToken)
	if cfg.ForwardToken == "" {
		return nil, errMissingForwardToken
	}
	if cfg.MaxStreamsPerTunnel <= 0 {
		cfg.MaxStreamsPerTunnel = defaultMaxStreamsPerTunnel
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = defaultMaxSessions
	}
	return &Gateway{
		cfg:    cfg,
		keys:   keys,
		routes: routes,
		reg:    newRegistry(),
		logger: logger,
	}, nil
}

// PublicHandler excludes forwarding; only the internal listener can enter a tunnel.
func (g *Gateway) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/connect", g.handleConnect)
	mux.HandleFunc("/healthz", healthz)
	return mux
}

func (g *Gateway) ForwardHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/", g.handleForward)
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (g *Gateway) ActiveSessions() int { return g.reg.activeSessions() }

// SetAdvertiseAddr lets tests publish listener addresses known only after bind.
func (g *Gateway) SetAdvertiseAddr(addr string) { g.cfg.AdvertiseAddr = addr }

func (g *Gateway) handleConnect(w http.ResponseWriter, r *http.Request) {
	// Shed before key lookup so a connect storm cannot load the key resolver.
	if g.reg.activeSessions() >= g.cfg.MaxSessions {
		g.logger.WarnContext(r.Context(), "tunnel connect rejected",
			slog.String("reason", "max-sessions"), slog.Int("max_sessions", g.cfg.MaxSessions))
		http.Error(w, "tunnel gateway at session capacity", http.StatusServiceUnavailable)
		return
	}

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
	serviceVersion := strings.TrimSpace(r.Header.Get(wire.HeaderTunnelServiceVersion))
	if serviceVersion == "" {
		g.logger.WarnContext(r.Context(), "tunnel connect rejected",
			slog.String("reason", "missing-service-version"), slog.String("tunnel_id", tunnelID))
		http.Error(w, "missing tunnel service version", http.StatusBadRequest)
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
	// Agents are non-browser clients; origin checks are not meaningful.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		g.logger.WarnContext(r.Context(), "tunnel websocket upgrade failed", slog.Any("error", err))
		return // Upgrade already wrote a response.
	}

	conn := websocket.NetConn(r.Context(), ws, websocket.MessageBinary)
	ycfg := yamux.DefaultConfig()
	ycfg.EnableKeepAlive = true
	ycfg.KeepAliveInterval = 15 * time.Second
	ycfg.LogOutput = yamuxLogOutput
	// Gateway opens substreams; agent accepts and serves them.
	session, err := yamux.Client(conn, ycfg)
	if err != nil {
		g.logger.ErrorContext(r.Context(), "tunnel yamux client failed", slog.Any("error", err))
		_ = conn.Close()
		return
	}
	defer conn.Close()

	// Record durable activation only after the session is actually live: a
	// valid-key plain-HTTP probe (no upgrade) must not flip status to active
	// or advance last_seen_at for a tunnel that never connected. On failure,
	// close the session — the agent retries and re-activates.
	if recorder, ok := g.keys.(ConnectionRecorder); ok {
		if err := recorder.MarkConnected(r.Context(), tunnelID, presentedKeyHash, agentVersion); err != nil {
			g.logger.ErrorContext(r.Context(), "tunnel connect activation failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			_ = session.Close()
			return
		}
	}

	sessionID := uuid.NewString()
	now := time.Now().UTC()
	remove := g.reg.add(tunnelID, sessionID, session, g.newSessionProxy(tunnelID, session), route.Connection{
		GatewaySessionID:       sessionID,
		ServiceVersion:         serviceVersion,
		AgentVersion:           agentVersion,
		ConnectedAt:            now,
		LastHeartbeatAt:        now,
		RemoteAddr:             r.RemoteAddr,
		ActiveSubstreams:       0,
		ActiveConsumerSessions: 0,
		Metadata:               metadata,
	})
	stateCtx, cancelState := routeOperationContext(r.Context())
	if err := g.routes.Publish(stateCtx, tunnelID, g.cfg.AdvertiseAddr, routeTTL); err != nil {
		g.logger.WarnContext(r.Context(), "tunnel route publish failed", slog.Any("error", err))
	}
	g.publishConnectionSnapshot(stateCtx, tunnelID, now)
	cancelState()
	g.logger.InfoContext(r.Context(), "tunnel connected",
		slog.String("tunnel_id", tunnelID), slog.String("session_id", sessionID),
		slog.String("agent_version", agentVersion), slog.Int("active", g.reg.activeSessions()))

	go g.sayHello(session, tunnelID, sessionID)

	stop := make(chan struct{})
	go g.refreshSessionState(tunnelID, presentedKeyHash, session, stop)

	<-session.CloseChan()
	close(stop)
	remove()
	g.cleanupSessionState(tunnelID)
	g.logger.InfoContext(context.Background(), "tunnel disconnected",
		slog.String("tunnel_id", tunnelID), slog.String("session_id", sessionID),
		slog.Int("active", g.reg.activeSessions()))
}

// cleanupSessionState tears down route/snapshot state after a session closes.
// The count-check and Unpublish are not atomic with a concurrent connect's
// add+Publish, so a dying session could delete the route a replacement session
// just published; after unpublishing we re-check the registry and republish if
// a session appeared. Any connect that adds itself after the re-check performs
// its own Publish after our Unpublish, so every interleaving converges on a
// published route while live sessions exist.
func (g *Gateway) cleanupSessionState(tunnelID string) {
	stateCtx, cancelState := routeOperationContext(context.Background())
	defer cancelState()
	if g.reg.tunnelSessionCount(tunnelID) > 0 {
		g.publishConnectionSnapshot(stateCtx, tunnelID, time.Now().UTC())
		return
	}
	if err := g.routes.Unpublish(stateCtx, tunnelID, g.cfg.AdvertiseAddr); err != nil {
		g.logger.WarnContext(stateCtx, "tunnel route unpublish failed", slog.Any("error", err))
	}
	g.deleteConnectionSnapshot(stateCtx, tunnelID)
	if g.reg.tunnelSessionCount(tunnelID) > 0 {
		if err := g.routes.Publish(stateCtx, tunnelID, g.cfg.AdvertiseAddr, routeTTL); err != nil {
			g.logger.WarnContext(stateCtx, "tunnel route republish after reconnect race failed", slog.Any("error", err))
		}
		g.publishConnectionSnapshot(stateCtx, tunnelID, time.Now().UTC())
	}
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
				opCtx, cancel := routeOperationContext(context.Background())
				active, err := checker.IsActive(opCtx, tunnelID, keyHash)
				cancel()
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
			opCtx, cancel := routeOperationContext(context.Background())
			if err := g.routes.Publish(opCtx, tunnelID, g.cfg.AdvertiseAddr, routeTTL); err != nil {
				g.logger.WarnContext(opCtx, "tunnel route refresh failed",
					slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			}
			g.publishConnectionSnapshot(opCtx, tunnelID, time.Now().UTC())
			cancel()
		}
	}
}

func routeOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, routeOperationTimeout)
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
	if err := store.PublishConnections(ctx, tunnelID, g.cfg.AdvertiseAddr, g.reg.connections(tunnelID, heartbeatAt), routeTTL); err != nil {
		g.logger.WarnContext(ctx, "tunnel connection snapshot publish failed",
			slog.String("tunnel_id", tunnelID), slog.Any("error", err))
	}
}

func (g *Gateway) deleteConnectionSnapshot(ctx context.Context, tunnelID string) {
	store, ok := g.routes.(route.ConnectionSnapshotStore)
	if !ok {
		return
	}
	if err := store.DeleteConnectionOwner(ctx, tunnelID, g.cfg.AdvertiseAddr); err != nil {
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

func (g *Gateway) handleForward(w http.ResponseWriter, r *http.Request) {
	presented := r.Header.Get(wire.HeaderTunnelForwardToken)
	if g.cfg.ForwardToken == "" {
		g.logger.ErrorContext(r.Context(), "tunnel forward rejected", slog.String("reason", "missing-forward-token-config"))
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !wire.ConstantTimeEqual(presented, g.cfg.ForwardToken) {
		g.logger.WarnContext(r.Context(), "tunnel forward rejected", slog.String("reason", "forward-token"))
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	r.Header.Del(wire.HeaderTunnelForwardToken)

	// Forwarding is internal-only; gram-server supplies the tunnel ID header.
	tunnelID := r.Header.Get(wire.HeaderTunnelID)
	if tunnelID == "" {
		http.Error(w, "missing tunnel id", http.StatusBadRequest)
		return
	}
	consumerSession := strings.TrimSpace(r.Header.Get(wire.HeaderTunnelConsumerSession))
	entry, failure := g.reg.beginForward(tunnelID, consumerSession, time.Now().UTC(), g.cfg.MaxStreamsPerTunnel)
	switch failure {
	case forwardReserved:
	case forwardBusy:
		// Live sessions exist but all are at their substream cap. The gateway
		// is healthy: callers must not unpublish its route; they may retry
		// another gateway or surface the 502.
		w.Header().Set(wire.HeaderTunnelError, wire.TunnelErrorTunnelBusy)
		http.Error(w, "tunnel is at capacity", http.StatusBadGateway)
		return
	default:
		// Distinguish known tunnel/no live session from auth failures.
		w.Header().Set(wire.HeaderTunnelError, wire.TunnelErrorNoLiveSession)
		http.Error(w, "tunnel has no live agent session", http.StatusBadGateway)
		return
	}
	r.Header.Del(wire.HeaderTunnelID)
	r.Header.Del(wire.HeaderTunnelConsumerSession)
	// Publish the begin-forward snapshot asynchronously: mid-flight counter
	// freshness matters (dashboards show active substreams during long-lived
	// streams), but the forward's latency path must not block on Redis. At
	// most one goroutine per in-flight forward, bounded by the substream cap
	// and the routeOperationTimeout.
	go func() {
		opCtx, cancel := routeOperationContext(context.Background())
		defer cancel()
		g.publishConnectionSnapshot(opCtx, tunnelID, time.Now().UTC())
	}()
	defer func() {
		g.reg.finishForward(entry, time.Now().UTC())
		opCtx, cancel := routeOperationContext(context.Background())
		defer cancel()
		g.publishConnectionSnapshot(opCtx, tunnelID, time.Now().UTC())
	}()

	entry.proxy.ServeHTTP(w, r)
}

// newSessionProxy builds the session-scoped reverse proxy stored on the
// registry entry. The transport opens a fresh yamux substream per request
// (keepalives disabled), so one proxy instance per session is semantically
// identical to one per forward, minus the per-request allocations.
func (g *Gateway) newSessionProxy(tunnelID string, session *yamux.Session) http.Handler {
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "tunnel" // ignored; substreamTransport dials the session
		},
		Transport:     substreamTransport(session),
		FlushInterval: -1, // stream SSE immediately
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, err error) {
			g.logger.Warn("tunnel forward failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", err))
			rw.Header().Set(wire.HeaderTunnelError, wire.TunnelErrorSubstreamFailed)
			rw.WriteHeader(http.StatusBadGateway)
		},
	}
}

// RevokeTunnel clears live routes/sessions; durable revocation stays in the key resolver.
func (g *Gateway) RevokeTunnel(ctx context.Context, tunnelID string) int {
	if revoker, ok := g.keys.(interface{ Revoke(string) }); ok {
		revoker.Revoke(tunnelID)
	}
	opCtx, cancel := routeOperationContext(ctx)
	defer cancel()
	_ = g.routes.Delete(opCtx, tunnelID)
	if store, ok := g.routes.(route.ConnectionSnapshotStore); ok {
		_ = store.DeleteConnections(opCtx, tunnelID)
	}
	return g.reg.kill(tunnelID)
}

// Disable keepalives so each request opens and closes its own yamux substream.
func substreamTransport(session *yamux.Session) *http.Transport {
	return &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return session.Open()
		},
	}
}
