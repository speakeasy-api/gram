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
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelsessions"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/publiclimits"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

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
	// RequestRate bounds all anonymous requests per tunnel when no limit is
	// stored on its row. Production wiring leaves it zero so the enforced
	// default is publiclimits.DefaultRequestRatePerSecond / DefaultRequestBurst,
	// the same values the management API reports as effective; a non-zero
	// value is a test seam and would make the API under-report the limit.
	RequestRate ratelimit.Rate
	// MaxRequestLifetime hard-bounds any single anonymous request, including
	// SSE streams — the proxy's idle timeout alone would let an active
	// stream outlive its session slot.
	MaxRequestLifetime time.Duration
}

// Built-in admission limits for anonymous public tunneled serving. Each is a
// per-tunnel backstop protecting the tunnel gateway and the customer backend
// from a single anonymous surface; one bucket is shared by every caller of a
// tunnel, so these are not per-client fairness limits. The request limit is
// the deployment default the management API reports; a source owner raises
// it by storing a limit on the tunneled_mcp_servers row (see
// limitersForServer). The initialize limit paces session reservations only.
const (
	defaultPublicRequestRatePerSecond    = publiclimits.DefaultRequestRatePerSecond
	defaultPublicRequestBurst            = publiclimits.DefaultRequestBurst
	defaultPublicInitializeRatePerSecond = 5
	defaultPublicInitializeBurst         = 20
)

func (c TunnelPublicConfig) withDefaults() TunnelPublicConfig {
	if c.SessionTTL <= 0 {
		c.SessionTTL = 24 * time.Hour
	}
	if c.LiveSessionCap <= 0 {
		c.LiveSessionCap = 10000
	}
	c.InitializeRate = normalizeRate(c.InitializeRate, defaultPublicInitializeRatePerSecond, defaultPublicInitializeBurst)
	c.RequestRate = normalizeRate(c.RequestRate, defaultPublicRequestRatePerSecond, defaultPublicRequestBurst)
	if c.MaxRequestLifetime <= 0 {
		c.MaxRequestLifetime = time.Hour
	}
	return c
}

// normalizeRate fills a partially specified Rate. No sustained rate means the
// built-in default; a sustained rate without a burst gets a burst of twice
// the per-interval token count (two seconds of traffic for the per-second
// rates every caller builds) so a single-knob override still admits short
// spikes.
func normalizeRate(r ratelimit.Rate, defaultPerSecond, defaultBurst int) ratelimit.Rate {
	if r.Tokens <= 0 || r.Interval <= 0 {
		return ratelimit.PerSecond(defaultPerSecond).WithBurst(defaultBurst)
	}
	if r.Burst <= 0 {
		r.Burst = 2 * r.Tokens
	}
	return r
}

// tunnelPublicLimiters is the pair of admission limiters one tunnel is
// checked against, bound to the Redis keys they meter. Stored per-tunnel
// rates get their own key suffix so a policy change starts a fresh bucket
// instead of reinterpreting the previous rate's arrival time.
type tunnelPublicLimiters struct {
	request       *ratelimit.Limiter
	requestKey    string
	initialize    *ratelimit.Limiter
	initializeKey string
}

func (l tunnelPublicLimiters) allowRequest(ctx context.Context) (ratelimit.Result, error) {
	res, err := l.request.Allow(ctx, l.requestKey)
	if err != nil {
		return res, fmt.Errorf("public tunnel request limiter: %w", err)
	}
	return res, nil
}

func (l tunnelPublicLimiters) allowInitialize(ctx context.Context) (ratelimit.Result, error) {
	res, err := l.initialize.Allow(ctx, l.initializeKey)
	if err != nil {
		return res, fmt.Errorf("public tunnel initialize limiter: %w", err)
	}
	return res, nil
}

// limiterKey identifies one constructed Limiter: the same name and rate
// always resolve to the same instance so instruments are created once.
type limiterKey struct {
	name string
	rate ratelimit.Rate
}

// tunnelPublicRuntime bundles the session store and rate limiters for the
// anonymous public tunnel path.
type tunnelPublicRuntime struct {
	cfg           TunnelPublicConfig
	sessions      *tunnelsessions.Store
	store         ratelimit.Store
	meterProvider metric.MeterProvider
	defaults      tunnelPublicLimiters
	// limiters caches per-rate Limiters for tunnels with stored limits. Its
	// size is bounded by the number of distinct operator-curated rates.
	limiters sync.Map
	metrics  *mcpmetrics.Metrics
}

const (
	tunnelPublicRequestLimiterName    = "tunnel-public-requests"
	tunnelPublicInitializeLimiterName = "tunnel-public-initialize"
)

func newTunnelPublicRuntime(redisClient *redis.Client, meterProvider metric.MeterProvider, metrics *mcpmetrics.Metrics, cfg TunnelPublicConfig) *tunnelPublicRuntime {
	if redisClient == nil {
		return nil
	}
	cfg = cfg.withDefaults()
	store := ratelimit.NewRedisStore(redisClient)
	return &tunnelPublicRuntime{
		cfg:           cfg,
		sessions:      tunnelsessions.NewStore(redisClient, cfg.SessionTTL, cfg.LiveSessionCap),
		store:         store,
		meterProvider: meterProvider,
		defaults: tunnelPublicLimiters{
			request:       ratelimit.New(store, tunnelPublicRequestLimiterName, cfg.RequestRate, ratelimit.WithMetrics(meterProvider)),
			requestKey:    "",
			initialize:    ratelimit.New(store, tunnelPublicInitializeLimiterName, cfg.InitializeRate, ratelimit.WithMetrics(meterProvider)),
			initializeKey: "",
		},
		limiters: sync.Map{},
		metrics:  metrics,
	}
}

// limiter returns the cached Limiter for name at rate, constructing it on
// first use.
func (rt *tunnelPublicRuntime) limiter(name string, rate ratelimit.Rate) *ratelimit.Limiter {
	key := limiterKey{name: name, rate: rate}
	if cached, ok := rt.limiters.Load(key); ok {
		if l, ok := cached.(*ratelimit.Limiter); ok {
			return l
		}
	}
	created := ratelimit.New(rt.store, name, rate, ratelimit.WithMetrics(rt.meterProvider))
	actual, _ := rt.limiters.LoadOrStore(key, created)
	if l, ok := actual.(*ratelimit.Limiter); ok {
		return l
	}
	return created
}

// limitersForServer resolves the admission limiters for one tunnel from its
// stored public limit: a NULL rate keeps the deployment-wide pair on the plain
// tunnel key; a stored rate gets its own limiters and rate-suffixed keys. The
// owner sets one number for every MCP interaction, so the same rate feeds the
// initialize bucket too — the built-in initialize guard exists to pace session
// reservations and must never undercut a limit the owner raised.
func (rt *tunnelPublicRuntime) limitersForServer(source *tunneledmcprepo.TunneledMcpServer) tunnelPublicLimiters {
	tunnelID := source.ID.String()
	limiters := rt.defaults
	limiters.requestKey = tunnelID
	limiters.initializeKey = tunnelID
	if rate := storedPublicRate(source.PublicRequestRatePerSecond, source.PublicRequestBurst); rate.Valid() {
		key := storedPublicRateKey(tunnelID, rate)
		limiters.request = rt.limiter(tunnelPublicRequestLimiterName, rate)
		limiters.requestKey = key
		limiters.initialize = rt.limiter(tunnelPublicInitializeLimiterName, rate)
		limiters.initializeKey = key
	}
	return limiters
}

// storedPublicRate turns a tunnel's stored per-second rate and optional burst
// into a Rate; an unset rate is the zero Rate (use the deployment default).
// The resolution is publiclimits.Effective, so the limit enforced here is the
// one the management API reports.
func storedPublicRate(perSecond, burst pgtype.Int4) ratelimit.Rate {
	if !publiclimits.Stored(perSecond) {
		return ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0}
	}
	rate, burstCap := publiclimits.Effective(perSecond, burst)
	return ratelimit.PerSecond(rate).WithBurst(burstCap)
}

func storedPublicRateKey(tunnelID string, rate ratelimit.Rate) string {
	return tunnelID + "@" + strconv.Itoa(rate.Tokens) + "/" + strconv.Itoa(rate.Burst)
}

// recordRejection counts a pre-proxy 429 on the anonymous path under the
// endpoint slug the request was served on — the same identifier the request
// logger carries — so the dimension stays bounded by the number of public
// tunnels rather than by client input.
func (rt *tunnelPublicRuntime) recordRejection(ctx context.Context, endpoint *mcpendpointsrepo.McpEndpoint, reason mcpmetrics.TunnelPublicRejectReason) {
	rt.metrics.RecordTunnelPublicRejection(ctx, endpoint.Slug, reason)
}

// isTunneledPublic reports whether the mcp_server is a tunneled backend with
// public visibility — the anonymous serving mode.
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
) (*tunneledmcprepo.TunneledMcpServer, error) {
	if s.tunnelPublic == nil {
		return nil, oops.E(oops.CodeNotFound, nil, "not found").LogWarn(ctx, logger.With(attr.SlogErrorMessage("public tunnel runtime unavailable")))
	}

	source, err := tunneledmcprepo.New(s.db).GetServerByID(ctx, tunneledmcprepo.GetServerByIDParams{
		ID:        mcpServer.TunneledMcpServerID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load tunneled mcp server").LogError(ctx, logger)
	}
	if !source.AllowPublic {
		return nil, oops.E(oops.CodeNotFound, nil, "not found").LogWarn(ctx, logger.With(attr.SlogErrorMessage("tunnel source does not allow public serving")))
	}
	return &source, nil
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

	source, err := s.requireTunneledPublicConsent(ctx, logger, endpoint, mcpServer)
	if err != nil {
		return err
	}
	rt := s.tunnelPublic
	limiters := rt.limitersForServer(source)

	ctx, cancel := context.WithTimeout(ctx, rt.cfg.MaxRequestLifetime)
	defer cancel()
	r = r.WithContext(ctx)

	tunnelID := mcpServer.TunneledMcpServerID.UUID.String()

	res, err := limiters.allowRequest(ctx)
	if err != nil {
		// Limiter store outage is an availability failure (503), not an
		// upstream/tunnel fault: fail closed — an anonymous surface without
		// its abuse controls must not serve.
		return oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
	}
	if !res.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())+1))
		rt.recordRejection(ctx, endpoint, mcpmetrics.TunnelPublicRejectRequestRate)
		return oops.E(oops.CodeRateLimitExceeded, nil, "too many requests to this MCP server").LogWarn(ctx, logger)
	}

	var organizationID string
	ctx, organizationID, err = s.prepareProxyBackendContext(ctx, w, r, logger, endpoint, mcpServer)
	if err != nil {
		return err
	}
	r = r.WithContext(ctx)

	sid := strings.TrimSpace(r.Header.Get(proxy.McpSessionIDHeader))
	if sid != "" {
		return s.serveTunneledPublicSession(w, r, logger, endpoint, mcpServer, organizationID, tunnelID, sid)
	}
	return s.serveTunneledPublicInit(w, r, logger, endpoint, mcpServer, organizationID, tunnelID, limiters)
}

// stripPublicResponseHeaders removes headers that must never reach an
// anonymous caller: the customer backend's own WWW-Authenticate challenge
// (this endpoint deliberately has no authorization server — relaying the
// challenge would misdirect clients at an unreachable one), and
// state-mutating headers (Set-Cookie, Clear-Site-Data) that would let a
// backend plant or wipe browser state on the Gram or custom-domain origin.
func stripPublicResponseHeaders(resp *http.Response) {
	resp.Header.Del("WWW-Authenticate")
	resp.Header.Del("Set-Cookie")
	resp.Header.Del("Clear-Site-Data")
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
	organizationID string,
	tunnelID string,
	limiters tunnelPublicLimiters,
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
		res, err := limiters.allowInitialize(ctx)
		if err != nil {
			return oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
		}
		if !res.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())+1))
			rt.recordRejection(ctx, endpoint, mcpmetrics.TunnelPublicRejectInitializeRate)
			return oops.E(oops.CodeRateLimitExceeded, nil, "too many initialize requests to this MCP server").LogWarn(ctx, logger)
		}

		sid, err = tunnelsessions.MintSessionID()
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "mint anonymous tunnel session id").LogError(ctx, logger)
		}

		if err := rt.sessions.Reserve(ctx, tunnelID, mcpServerID, sid); err != nil {
			if capErr, ok := errors.AsType[*tunnelsessions.CapacityError](err); ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(capErr.RetryAfter.Seconds())+1))
				rt.recordRejection(ctx, endpoint, mcpmetrics.TunnelPublicRejectSessionCapacity)
				return oops.E(oops.CodeRateLimitExceeded, nil, "this MCP server is at its anonymous session capacity").LogWarn(ctx, logger)
			}
			return oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
		}
		reserved = true
	}

	p, err := s.tunnelManager.buildProxy(ctx, tunnelrouting.ClientAffinityKeyFromRequest(r), logger, endpoint.ProjectID, organizationID, mcpServer, "", "", nil)
	if err != nil {
		if reserved {
			s.rollbackReservation(ctx, logger, tunnelID, mcpServerID, sid)
		}
		return err
	}

	committed := false
	p.UpstreamResponseInterceptor = func(ctx context.Context, resp *http.Response) error {
		if rejection := tunnelrouting.BusyResponseRejection(resp); rejection != nil {
			return rejection
		}
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
	if _, err := s.requireTunneledPublicConsent(ctx, logger, endpoint, mcpServer); err != nil {
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
	// Read one byte past the proxy's buffered-body cap: restoring exactly the
	// cap would make an oversized body indistinguishable from a cap-sized one
	// downstream, silently truncating it instead of rejecting it as too large.
	body, err := io.ReadAll(io.LimitReader(r.Body, proxy.DefaultMaxBufferedBodyBytes+1))
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
	organizationID string,
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
		// Configured headers win over copied request headers.
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
			MetaMCPServerID:     "",
		},
		session.GatewayAddr,
		headers,
		mcpServer.Visibility,
		organizationID,
		endpoint.ProjectID.String(),
		"",
		"",
		nil,
	)
	// Redirects won't work across a tunnel boundary; disable.
	p.DisableRedirects = true
	p.GuardianClientOptions = m.guardianClientOptions()

	isDelete := r.Method == http.MethodDelete
	p.UpstreamResponseInterceptor = func(ctx context.Context, resp *http.Response) error {
		if rejection := tunnelrouting.BusyResponseRejection(resp); rejection != nil {
			return rejection
		}
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
		if guardian.IsDeadPeerDialError(err) {
			if delErr := rt.sessions.Delete(ctx, tunnelID, mcpServerID, sid); delErr != nil {
				logger.ErrorContext(ctx, "drop anonymous tunnel session for dead gateway", attr.SlogError(delErr))
			}
			return oops.E(oops.CodeNotFound, tunnelsessions.ErrNotFound, "session not found").LogWarn(ctx, logger.With(attr.SlogErrorMessage("recorded tunnel gateway is unreachable")))
		}
		return fmt.Errorf("serve public tunneled session: %w", err)
	}
	return nil
}
