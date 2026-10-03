package gram

import (
	"context"
	"log/slog"
	"net"
	"net/url"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/tunnel/route"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

type mcpRemoteSessionDependencies struct {
	IDTokenKeys *jwks.KeyResolver
	Verifier    remotesessions.IDTokenVerifier
	Refresher   *remotesessions.IssuerMetadataRefresher
	Enricher    *remotesessions.SessionEnricher
	Challenges  *remotesessions.ChallengeManager
}

// Both listener processes must use identical identity verification and private
// authority checks when resuming the OAuth flows that cross their boundary.
// newTunnelHTTPClient builds the transport that carries back-channel OAuth
// calls (token exchange, refresh, revocation, dynamic client registration) for
// remote session issuers bound to a tunneled MCP server, instead of dialing
// them from cloud egress.
func newTunnelHTTPClient(c *cli.Context, guardianPolicy *guardian.Policy, redisClient *redis.Client) *tunnelrouting.HTTPClient {
	// guardian.WithAllowedCIDRBlocks silently drops invalid CIDRs, so a typo
	// here would strand tunnels fail-closed with no signal. Reject
	// misconfiguration at startup instead.
	cidrs := c.StringSlice("tunnel-gateway-cidr-blocks")
	for _, cidr := range cidrs {
		_, _, err := net.ParseCIDR(cidr)
		inv.Require("tunnel gateway CIDR blocks", cidr+" parses", err)
	}
	return tunnelrouting.NewHTTPClient(route.NewRedis(redisClient), c.String("tunnel-forward-token"), guardianPolicy, cidrs)
}

func newMCPRemoteSessionDependencies(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, enc *encryption.Client, guardianPolicy *guardian.Policy, tunnels *tunnelrouting.HTTPClient, redisClient *redis.Client, serverURL *url.URL, callbackOrigins remotesessions.CallbackOrigins, auditLogger *audit.Logger, assertionSigner remotesessions.TokenEndpointAssertionSigner) (*mcpRemoteSessionDependencies, error) {
	idTokenKeys := remotesessions.NewIDTokenKeyResolver(logger, guardianPolicy, meterProvider, ratelimit.NewRedisStore(redisClient))
	verifier := remotesessions.NewIDTokenVerifier(idTokenKeys)
	refresher := remotesessions.NewIssuerMetadataRefresher(logger, meterProvider, db, guardianPolicy, tunnels, auditLogger)
	enricher := remotesessions.NewSessionEnricher(logger, enc, guardianPolicy, idTokenKeys,
		ratelimit.New(ratelimit.NewRedisStore(redisClient), "remote_session_enrichment", remotesessions.EnrichmentRate, ratelimit.WithMetrics(meterProvider)), tunnels, refresher)
	challenges := remotesessions.NewChallengeManager(logger, tracerProvider, meterProvider, db, enc, guardianPolicy, tunnels, cache.NewRedisCacheAdapter(redisClient), serverURL,
		remotesessions.WithPrivateAuthorityValidator(func(ctx context.Context, state remotesessions.RemoteLoginState) error {
			return mcp.ValidateRemoteLoginPrivateAuthority(ctx, db, logger, state)
		}),
		remotesessions.WithIDTokenVerifier(verifier),
		remotesessions.WithIssuerMetadataRefresher(refresher),
		remotesessions.WithSessionEnricher(enricher),
		remotesessions.WithRegistrationAuditLogger(auditLogger),
		remotesessions.WithTokenEndpointAssertionSigner(assertionSigner),
		remotesessions.WithCallbackOrigins(callbackOrigins))
	return &mcpRemoteSessionDependencies{IDTokenKeys: idTokenKeys, Verifier: verifier, Refresher: refresher, Enricher: enricher, Challenges: challenges}, nil
}
