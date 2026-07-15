// Anonymous public serving for tunneled MCP servers.
//
// Public tunneled endpoints have NO OAuth surface and NO user_sessions rows:
// Gram terminates MCP sessions itself. On a successful anonymous initialize
// it mints a Gram-owned session id, rewrites the backend's Mcp-Session-Id
// response header to it, and records a Redis-only mapping to the backend's
// session id plus the exact tunnel target (gateway address + agent session)
// that owns it. Session-bearing requests resolve that mapping and are pinned
// to the recorded target — never rendezvous-spilled to a sibling agent whose
// backend does not know the session. Stateless / draft-protocol backends that
// return no Mcp-Session-Id are served too, without a mapping — the path has no
// hard dependency on stateful sessions. Access is gated solely on the tunnel
// owner's durable allow_public consent (double opt-in with
// mcp_servers.visibility=public), enforced at validation and at serve time.
package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelsessions"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

// anonymousAffinityPrefix namespaces the consumer-session key derived from a
// Gram-minted anonymous session id, distinguishing it from the "auth" prefix
// used for token-derived affinity keys.
const anonymousAffinityPrefix = "anonsid"

// TunnelPublicConfig carries the operator-tunable knobs for anonymous public
// tunneled MCP serving. Zero values are replaced with the defaults below in
// newTunnelPublicRuntime.
type TunnelPublicConfig struct {
	// SessionTTL is the sliding lifetime of an anonymous session mapping.
	SessionTTL time.Duration
	// LiveSessionCap bounds concurrently tracked anonymous sessions per
	// tunnel.
	LiveSessionCap int
	// InitializeRate bounds anonymous initialize requests per tunnel.
	InitializeRate ratelimit.Rate
	// RequestRate bounds all anonymous requests per tunnel.
	RequestRate ratelimit.Rate
	// MaxRequestLifetime hard-bounds any single anonymous request, including
	// SSE streams — the proxy's idle timeout alone would let an active
	// stream outlive its session slot.
	MaxRequestLifetime time.Duration
}

func (c TunnelPublicConfig) withDefaults() TunnelPublicConfig {
	if c.SessionTTL <= 0 {
		c.SessionTTL = 24 * time.Hour
	}
	if c.LiveSessionCap <= 0 {
		c.LiveSessionCap = 100
	}
	if !c.InitializeRate.Valid() {
		c.InitializeRate = ratelimit.PerSecond(5).WithBurst(20)
	}
	if !c.RequestRate.Valid() {
		c.RequestRate = ratelimit.PerSecond(50).WithBurst(100)
	}
	if c.MaxRequestLifetime <= 0 {
		c.MaxRequestLifetime = time.Hour
	}
	return c
}

// tunnelPublicRuntime bundles the session store and rate limiters for the
// anonymous public tunnel path. Nil on a Service means the capability is not
// wired (no Redis) and every public tunneled request fails closed.
type tunnelPublicRuntime struct {
	cfg               TunnelPublicConfig
	sessions          *tunnelsessions.Store
	requestLimiter    *ratelimit.Limiter
	initializeLimiter *ratelimit.Limiter
}

func newTunnelPublicRuntime(redisClient *redis.Client, cfg TunnelPublicConfig) *tunnelPublicRuntime {
	if redisClient == nil {
		return nil
	}
	cfg = cfg.withDefaults()
	store := ratelimit.NewRedisStore(redisClient)
	return &tunnelPublicRuntime{
		cfg:               cfg,
		sessions:          tunnelsessions.NewStore(redisClient, cfg.SessionTTL, cfg.LiveSessionCap),
		requestLimiter:    ratelimit.New(store, "tunnel-public-requests", cfg.RequestRate),
		initializeLimiter: ratelimit.New(store, "tunnel-public-initialize", cfg.InitializeRate),
	}
}

// isTunneledPublic reports whether the mcp_server is a tunneled backend with
// public visibility — the anonymous serving mode. All issuer-gate skips,
// OAuth-surface 404s, and consent checks key off this predicate.
func isTunneledPublic(mcpServer *mcpserversrepo.McpServer) bool {
	return mcpServer.TunneledMcpServerID.Valid && mcpServer.Visibility == mcpservers.VisibilityPublic
}

// hashSessionID returns the loggable sha256 prefix of a session id. The raw
// id is bearer-like state and must never appear in logs, spans, or telemetry.
func hashSessionID(sid string) string {
	sum := sha256.Sum256([]byte(sid))
	return hex.EncodeToString(sum[:8])
}

// requireTunneledPublicConsent fail-closed gates anonymous public serving on
// the tunnel owner's allow_public consent (double opt-in with
// visibility=public). Every rejection surfaces as 404 so unauthenticated
// callers cannot distinguish a gated endpoint from a missing one; the
// distinct causes are logged. A nil runtime (no Redis wired) also fails
// closed — the abuse controls that bound anonymous traffic cannot run
// without it.
func (s *Service) requireTunneledPublicConsent(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
) error {
	if s.tunnelPublic == nil {
		return oops.E(oops.CodeNotFound, nil, "not found").LogWarn(ctx, logger.With(attr.SlogErrorMessage("public tunnel runtime unavailable")))
	}

	source, err := tunneledmcprepo.New(s.db).GetServerByID(ctx, tunneledmcprepo.GetServerByIDParams{
		ID:        mcpServer.TunneledMcpServerID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load tunneled mcp server").LogError(ctx, logger)
	}
	if !source.AllowPublic {
		return oops.E(oops.CodeNotFound, nil, "not found").LogWarn(ctx, logger.With(attr.SlogErrorMessage("tunnel source does not allow public serving")))
	}
	return nil
}

// serveTunneledPublicBackend is the anonymous serving path for a tunneled
// mcp_server with public visibility. The caller has already passed the
// consent gate in serveResolvedMCPEndpoint; it is re-run here as
// defense-in-depth so no other route into this function can serve without
// consent.
func (s *Service) serveTunneledPublicBackend(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
) error {
	ctx := r.Context()

	if err := s.requireTunneledPublicConsent(ctx, logger, endpoint, mcpServer); err != nil {
		return err
	}
	rt := s.tunnelPublic

	// Hard-bound the whole exchange, including SSE streams: the proxy's idle
	// timeout alone would let an active stream outlive its session slot.
	ctx, cancel := context.WithTimeout(ctx, rt.cfg.MaxRequestLifetime)
	defer cancel()
	r = r.WithContext(ctx)

	tunnelID := mcpServer.TunneledMcpServerID.UUID.String()

	res, err := rt.requestLimiter.Allow(ctx, tunnelID)
	if err != nil {
		// Limiter store outage is an availability failure (503), not an
		// upstream/tunnel fault: fail closed — an anonymous surface without
		// its abuse controls must not serve.
		return oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
	}
	if !res.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())+1))
		return oops.E(oops.CodeRateLimitExceeded, nil, "too many requests to this MCP server").LogWarn(ctx, logger)
	}

	// Identity probe + project context, shared with the other public
	// backends: anonymous callers pass through untouched; Gram-authenticated
	// callers get their context stamped. Invalid supplied credentials are
	// rejected (parity with public remote/toolset backends).
	ctx, err = s.prepareProxyBackendContext(ctx, w, r, logger, endpoint, mcpServer)
	if err != nil {
		return err
	}
	r = r.WithContext(ctx)

	sid := strings.TrimSpace(r.Header.Get(proxy.McpSessionIDHeader))
	if sid != "" {
		return s.serveTunneledPublicSession(w, r, logger, endpoint, mcpServer, tunnelID, sid)
	}
	return s.serveTunneledPublicInit(w, r, logger, endpoint, mcpServer, tunnelID)
}

// stripPublicResponseHeaders removes headers that must never reach an
// anonymous caller: the customer backend's own WWW-Authenticate challenge
// (this endpoint deliberately has no authorization server — relaying the
// challenge would misdirect clients at an unreachable one).
func stripPublicResponseHeaders(resp *http.Response) {
	resp.Header.Del("WWW-Authenticate")
}

// reservationCleanupTimeout bounds the detached Redis cleanup for an
// uncommitted reservation so a Redis stall cannot pin the request goroutine.
const reservationCleanupTimeout = 5 * time.Second

// serveTunneledPublicInit serves anonymous requests that carry no Gram
// session id: initialize plus all traffic to stateless/draft-protocol
// backends. Admission (initialize rate limit + capacity reservation) runs as
// plain pre-proxy code so availability failures surface as real HTTP status
// codes (503/429 + Retry-After) rather than a JSON-RPC 200 envelope. Only a
// positively-identified initialize reserves a slot; every other POST
// (stateless follow-up traffic, notifications) proxies straight through,
// bounded by the all-request limiter already applied by the caller — so the
// path has no hard dependency on stateful sessions.
func (s *Service) serveTunneledPublicInit(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	tunnelID string,
) error {
	ctx := r.Context()
	rt := s.tunnelPublic
	mcpServerID := mcpServer.ID.String()

	// Peek the JSON-RPC method, restoring the body for the proxy. A parse
	// failure or non-initialize method simply skips reservation; the proxy
	// then handles the request (or rejects a malformed/batch body) itself.
	isInit := r.Method == http.MethodPost && peekIsInitialize(r)

	var sid string
	reserved := false
	if isInit {
		res, err := rt.initializeLimiter.Allow(ctx, tunnelID)
		if err != nil {
			return oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
		}
		if !res.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())+1))
			return oops.E(oops.CodeRateLimitExceeded, nil, "too many initialize requests to this MCP server").LogWarn(ctx, logger)
		}

		sid, err = tunnelsessions.MintSessionID()
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "mint anonymous tunnel session id").LogError(ctx, logger)
		}

		if err := rt.sessions.Reserve(ctx, tunnelID, mcpServerID, sid); err != nil {
			var capErr *tunnelsessions.CapacityError
			if errors.As(err, &capErr) {
				w.Header().Set("Retry-After", strconv.Itoa(int(capErr.RetryAfter.Seconds())+1))
				return oops.E(oops.CodeRateLimitExceeded, nil, "this MCP server is at its anonymous session capacity").LogWarn(ctx, logger)
			}
			return oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
		}
		reserved = true
	}

	p, err := s.tunnelManager.buildProxy(ctx, r, logger, endpoint, mcpServer, "", "")
	if err != nil {
		if reserved {
			s.rollbackReservation(ctx, logger, tunnelID, mcpServerID, sid)
		}
		return err
	}

	committed := false
	p.UpstreamResponseInterceptor = func(ctx context.Context, resp *http.Response) error {
		stripPublicResponseHeaders(resp)
		if !reserved {
			return nil
		}
		ok, err := s.commitAnonymousSession(ctx, logger, endpoint, mcpServer, p, tunnelID, mcpServerID, sid, resp)
		if err != nil {
			return err
		}
		committed = ok
		return nil
	}

	err = serveProxyBackend(w, r, p)
	// A reservation that never committed — forward error, non-2xx response,
	// stateless (no session header) response, or a stream that died before
	// commit — must release its capacity slot rather than leak it until TTL.
	if reserved && !committed {
		s.rollbackReservation(ctx, logger, tunnelID, mcpServerID, sid)
	}
	if err != nil {
		return fmt.Errorf("serve public tunneled backend: %w", err)
	}
	return nil
}

// commitAnonymousSession records the Redis mapping for a successful,
// session-bearing anonymous initialize and rewrites the response's
// Mcp-Session-Id to the Gram-owned id. Returns (committed, err): committed is
// false with a nil error for the paths that legitimately mint no session
// (non-2xx initialize, stateless backend that returned no session header) so
// the caller releases the reservation. A non-nil error aborts the relay
// pre-flush — the fail-closed contract for a session-bearing initialize whose
// state cannot be recorded.
func (s *Service) commitAnonymousSession(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	p *proxy.Proxy,
	tunnelID, mcpServerID, sid string,
	resp *http.Response,
) (bool, error) {
	logger = logger.With(attr.SlogTunnelAnonymousSessionHash(hashSessionID(sid)))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, nil
	}

	backendSids := resp.Header.Values(proxy.McpSessionIDHeader)
	if len(backendSids) == 0 {
		// Stateless / draft-protocol backend: no session to track. Gram never
		// synthesizes a session header the backend did not produce.
		return false, nil
	}
	if len(backendSids) > 1 {
		return false, oops.E(oops.CodeGatewayError, nil, "MCP server returned an invalid session").LogError(ctx, logger.With(attr.SlogErrorMessage("multiple Mcp-Session-Id response headers")))
	}
	backendSid := backendSids[0]
	if !isValidBackendSessionID(backendSid) {
		return false, oops.E(oops.CodeGatewayError, nil, "MCP server returned an invalid session").LogError(ctx, logger.With(attr.SlogErrorMessage("malformed Mcp-Session-Id response header")))
	}

	agentSession := strings.TrimSpace(resp.Header.Get(wire.HeaderTunnelAgentSession))
	if agentSession == "" {
		// The serving gateway predates exact-target support. Fail closed:
		// without the agent session the mapping cannot pin the session to the
		// agent that owns it, and a later rendezvous re-pin would hand the
		// session id to a sibling backend.
		return false, oops.E(oops.CodeUnavailable, nil, "service temporarily unavailable").LogError(ctx, logger.With(attr.SlogErrorMessage("tunnel gateway did not report an agent session")))
	}

	// Recheck consent immediately before recording the session: a Purge
	// (consent withdrawn) may have run after this request's Reserve, in which
	// case the mapping must not be created. Commit additionally refuses if the
	// reservation's live-set member is gone (ErrReservationLost).
	if err := s.requireTunneledPublicConsent(ctx, logger, endpoint, mcpServer); err != nil {
		return false, err
	}

	session := tunnelsessions.Session{
		BackendSessionID: backendSid,
		GatewayAddr:      p.RemoteURL,
		AgentSessionID:   agentSession,
	}
	err := s.tunnelPublic.sessions.Commit(ctx, tunnelID, mcpServerID, sid, session)
	switch {
	case errors.Is(err, tunnelsessions.ErrReservationLost):
		return false, oops.E(oops.CodeNotFound, nil, "session not found").LogWarn(ctx, logger.With(attr.SlogErrorMessage("reservation purged mid-initialize")))
	case err != nil:
		return false, oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
	}

	resp.Header.Set(proxy.McpSessionIDHeader, sid)
	logger.InfoContext(ctx, "anonymous tunnel session established")
	return true, nil
}

// rollbackReservation releases an uncommitted capacity slot on a bounded,
// detached context so a Redis stall cannot pin the request goroutine.
func (s *Service) rollbackReservation(ctx context.Context, logger *slog.Logger, tunnelID, mcpServerID, sid string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reservationCleanupTimeout)
	defer cancel()
	if err := s.tunnelPublic.sessions.Rollback(cleanupCtx, tunnelID, mcpServerID, sid); err != nil {
		logger.ErrorContext(ctx, "release anonymous tunnel session reservation", attr.SlogError(err))
	}
}

// peekIsInitialize reads the request body (bounded), restores it for the
// proxy, and reports whether it is a single JSON-RPC "initialize" request.
// Malformed, batched, or oversized bodies read as not-initialize; the proxy
// then applies its own parsing and rejection semantics.
func peekIsInitialize(r *http.Request) bool {
	if r.Body == nil {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, proxy.DefaultMaxBufferedBodyBytes))
	_ = r.Body.Close()
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var probe struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.Method == "initialize"
}

// isValidBackendSessionID enforces the MCP spec's constraint that a session
// id contains only visible ASCII, plus a size bound so a misbehaving backend
// cannot bloat Redis.
func isValidBackendSessionID(sid string) bool {
	if sid == "" || len(sid) > tunnelsessions.MaxBackendSessionIDLength {
		return false
	}
	for _, c := range []byte(sid) {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

// serveTunneledPublicSession serves an anonymous request that carries a
// Gram-owned session id: resolve the Redis mapping, pin the forward to the
// exact recorded gateway + agent session, and translate the session header in
// both directions. A lost mapping or dead target surfaces as HTTP 404 so MCP
// clients re-initialize; the cross-gateway retryer is never used because the
// backend session exists on exactly one agent.
func (s *Service) serveTunneledPublicSession(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	tunnelID string,
	sid string,
) error {
	ctx := r.Context()
	rt := s.tunnelPublic
	mcpServerID := mcpServer.ID.String()

	if !tunnelsessions.IsSessionID(sid) {
		// Not a Gram-minted id — never valid on this endpoint, and not safe
		// to use as Redis key material.
		return oops.E(oops.CodeNotFound, nil, "session not found").LogWarn(ctx, logger)
	}
	logger = logger.With(attr.SlogTunnelAnonymousSessionHash(hashSessionID(sid)))

	// DELETE resolves without extending the session's life; POST/GET slide
	// the TTL forward.
	refresh := r.Method != http.MethodDelete
	session, err := rt.sessions.Resolve(ctx, tunnelID, mcpServerID, sid, refresh)
	switch {
	case errors.Is(err, tunnelsessions.ErrNotFound):
		return oops.E(oops.CodeNotFound, nil, "session not found").LogWarn(ctx, logger)
	case err != nil:
		// Redis unavailable: fail closed but do NOT delete the mapping —
		// the session may still be live.
		return oops.E(oops.CodeGatewayError, err, "service temporarily unavailable").LogError(ctx, logger)
	}

	// The recorded gateway must still be a live route owner. Gone from the
	// candidate set means the gateway (and with it the agent session) is
	// dead: drop the mapping and tell the client to re-initialize.
	m := s.tunnelManager
	candidates, err := m.routes.Candidates(ctx, tunnelID)
	if err != nil {
		return oops.E(oops.CodeGatewayError, err, "list tunnel routes").LogError(ctx, logger)
	}
	live := false
	for _, candidate := range candidates {
		candidateURL, urlErr := tunnelrouting.GatewayURL(candidate)
		if urlErr == nil && candidateURL == session.GatewayAddr {
			live = true
			break
		}
	}
	if !live {
		if delErr := rt.sessions.Delete(ctx, tunnelID, mcpServerID, sid); delErr != nil {
			logger.ErrorContext(ctx, "drop anonymous tunnel session for dead gateway", attr.SlogError(delErr))
		}
		return oops.E(oops.CodeNotFound, nil, "session not found").LogWarn(ctx, logger.With(attr.SlogErrorMessage("recorded tunnel gateway is no longer live")))
	}

	headers := tunnelrouting.Headers(tunnelID, m.forwardToken, tunnelrouting.HashedClientAffinityKey(anonymousAffinityPrefix, sid))
	headers = append(headers,
		proxy.ConfiguredHeader{
			IsRequired:             true,
			Name:                   wire.HeaderTunnelAgentSession,
			StaticValue:            session.AgentSessionID,
			ValueFromRequestHeader: "",
		},
		// Forward the backend's own session id in place of the Gram-owned
		// one; configured headers win over copied request headers.
		proxy.ConfiguredHeader{
			IsRequired:             true,
			Name:                   proxy.McpSessionIDHeader,
			StaticValue:            session.BackendSessionID,
			ValueFromRequestHeader: "",
		},
	)

	p := m.proxyManager.BuildTarget(
		logger,
		proxy.ServerIdentity{
			RemoteMCPServerID:   "",
			TunneledMCPServerID: tunnelID,
			McpServerID:         mcpServerID,
		},
		session.GatewayAddr,
		headers,
		mcpServer.Visibility,
		endpoint.ProjectID.String(),
		"",
		"",
	)
	if len(m.gatewayCIDRs) > 0 {
		p.GuardianClientOptions = []guardian.ClientOption{guardian.WithAllowedCIDRBlocks(m.gatewayCIDRs...)}
	}

	isDelete := r.Method == http.MethodDelete
	p.UpstreamResponseInterceptor = func(ctx context.Context, resp *http.Response) error {
		stripPublicResponseHeaders(resp)

		// The exact agent session is gone: the backend session died with it.
		// Translate the gateway's 502 into the MCP-spec 404 so the client
		// re-initializes, and drop the mapping.
		if resp.StatusCode == http.StatusBadGateway && resp.Header.Get(tunnelrouting.ErrorHeader) == wire.TunnelErrorNoLiveSession {
			if delErr := rt.sessions.Delete(ctx, tunnelID, mcpServerID, sid); delErr != nil {
				logger.ErrorContext(ctx, "drop anonymous tunnel session for dead agent", attr.SlogError(delErr))
			}
			return oops.E(oops.CodeNotFound, nil, "session not found").LogWarn(ctx, logger.With(attr.SlogErrorMessage("tunnel agent session is gone")))
		}

		// The backend no longer knows the session (404), or the client
		// terminated it (successful DELETE): drop the mapping. Everything
		// else (405, busy, 5xx) preserves it.
		terminated := resp.StatusCode == http.StatusNotFound ||
			(isDelete && resp.StatusCode >= 200 && resp.StatusCode < 300)
		if terminated {
			if delErr := rt.sessions.Delete(ctx, tunnelID, mcpServerID, sid); delErr != nil {
				logger.ErrorContext(ctx, "drop terminated anonymous tunnel session", attr.SlogError(delErr))
			}
		}

		// Never leak the backend's session id: rewrite any echoed session
		// header back to the Gram-owned id.
		if resp.Header.Get(proxy.McpSessionIDHeader) != "" {
			resp.Header.Set(proxy.McpSessionIDHeader, sid)
		}
		return nil
	}

	if err := serveProxyBackend(w, r, p); err != nil {
		return fmt.Errorf("serve public tunneled session: %w", err)
	}
	return nil
}
