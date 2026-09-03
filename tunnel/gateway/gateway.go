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
	"sync"
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

	routeRefreshInterval time.Duration
}

// Gateway owns live agent yamux sessions and maps internal forwards to substreams.
type Gateway struct {
	cfg        Config
	keys       KeyResolver
	reg        *registry
	reconciler *routeReconciler
	logger     *slog.Logger
	drain      sync.Once
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
	if cfg.routeRefreshInterval <= 0 {
		cfg.routeRefreshInterval = routeTTL / 2
	}
	reg := newRegistry()
	return &Gateway{
		cfg:        cfg,
		keys:       keys,
		reg:        reg,
		reconciler: newRouteReconciler(reg, keys, routes, cfg.AdvertiseAddr, logger, cfg.routeRefreshInterval),
		logger:     logger,
		drain:      sync.Once{},
	}, nil
}

// PublicHandler excludes forwarding; only the internal listener can enter a tunnel.
func (g *Gateway) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/connect", g.handleConnect)
	mux.HandleFunc("/healthz", g.healthz)
	return mux
}

func (g *Gateway) ForwardHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", g.healthz)
	mux.HandleFunc("/", g.handleForward)
	return mux
}

// Liveness requires three 20s failures, longer than the 25s drain; returning
// 503 here safely removes a draining pod from Service endpoints immediately.
func (g *Gateway) healthz(w http.ResponseWriter, _ *http.Request) {
	if g.reg.isDraining() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (g *Gateway) ActiveSessions() int { return g.reg.activeSessions() }

// SetAdvertiseAddr lets tests publish listener addresses known only after bind.
func (g *Gateway) SetAdvertiseAddr(addr string) { g.reconciler.address.Store(addr) }

// Drain stops session admission and removes every route this gateway has owned.
// Existing sessions remain open until CloseSessions is called.
func (g *Gateway) Drain(ctx context.Context) {
	g.drain.Do(func() {
		tunnelIDs := g.reg.beginDrain()
		g.reconciler.requestDrain(ctx, tunnelIDs)
	})
	select {
	case <-g.reconciler.done:
	case <-ctx.Done():
	}
}

// CloseSessions closes all live agent sessions concurrently within ctx.
func (g *Gateway) CloseSessions(ctx context.Context) {
	sessions := g.reg.sessionsSnapshot()
	closed := make(chan struct{}, len(sessions))
	for _, session := range sessions {
		go func() {
			_ = session.Close()
			closed <- struct{}{}
		}()
	}
	for range sessions {
		select {
		case <-closed:
		case <-ctx.Done():
			return
		}
	}
}

func (g *Gateway) handleConnect(w http.ResponseWriter, r *http.Request) {
	if g.reg.isDraining() {
		http.Error(w, "tunnel gateway is draining", http.StatusServiceUnavailable)
		return
	}

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

	// This best-effort re-check only narrows the upgrade-to-MarkConnected drain race.
	// reg.add remains the authoritative session gate; losing the remaining race can
	// write one spurious durable activation that self-corrects when the agent re-homes.
	if g.reg.isDraining() {
		g.logger.InfoContext(r.Context(), "tunnel connect rejected",
			slog.String("reason", "draining"), slog.String("tunnel_id", tunnelID))
		_ = session.Close()
		return
	}

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
	remove := g.reg.add(tunnelID, sessionID, presentedKeyHash, session, g.newSessionProxy(tunnelID, session), route.Connection{
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
	if remove == nil {
		g.logger.InfoContext(r.Context(), "tunnel connect rejected",
			slog.String("reason", "draining"), slog.String("tunnel_id", tunnelID))
		return
	}
	g.reconciler.nudge(tunnelID)
	g.logger.InfoContext(r.Context(), "tunnel connected",
		slog.String("tunnel_id", tunnelID), slog.String("session_id", sessionID),
		slog.String("agent_version", agentVersion), slog.Int("active", g.reg.activeSessions()))

	go g.sayHello(session, tunnelID, sessionID)

	<-session.CloseChan()
	remove()
	g.reconciler.nudge(tunnelID)
	g.logger.InfoContext(context.Background(), "tunnel disconnected",
		slog.String("tunnel_id", tunnelID), slog.String("session_id", sessionID),
		slog.Int("active", g.reg.activeSessions()))
}

func routeOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, routeOperationTimeout)
}

func hashBearerKey(bearer string) string {
	key := strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	return wire.HashKey(key)
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
	exactSession := strings.TrimSpace(r.Header.Get(wire.HeaderTunnelAgentSession))
	entry, failure := g.reg.beginForward(tunnelID, consumerSession, exactSession, time.Now().UTC(), g.cfg.MaxStreamsPerTunnel)
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
	r.Header.Del(wire.HeaderTunnelAgentSession)
	// Report the agent session that actually serves this forward so
	// gram-server can pin session-bearing MCP traffic to it later. Set
	// before ServeHTTP: the reverse proxy adds upstream headers to the
	// existing header map without clearing it.
	w.Header().Set(wire.HeaderTunnelAgentSession, entry.id)
	// Publish the begin-forward snapshot asynchronously: mid-flight counter
	// freshness matters, but coalescing keeps Redis out of the forwarding path.
	g.reconciler.nudge(tunnelID)
	defer func() {
		g.reg.finishForward(entry, time.Now().UTC())
		g.reconciler.nudge(tunnelID)
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

// RevokeTunnel closes this gateway's sessions and globally removes every route
// and connection snapshot owner. Durable revocation stays in the key resolver.
func (g *Gateway) RevokeTunnel(ctx context.Context, tunnelID string) int {
	if revoker, ok := g.keys.(interface{ Revoke(string) }); ok {
		revoker.Revoke(tunnelID)
	}
	killed := g.reg.kill(tunnelID)
	g.reconciler.requestRevoke(ctx, tunnelID)
	return killed
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
