package gram

import (
	"net"

	"github.com/urfave/cli/v2"

	"context"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/tunnel/route"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/clientcredentials"
)

type mcpRemoteSessionDependencies struct {
	Verifier   remotesessions.IDTokenVerifier
	Refresher  *remotesessions.IssuerMetadataRefresher
	Enricher   *remotesessions.SessionEnricher
	Challenges *remotesessions.ChallengeManager

	// OrgEncryption is the process's organization encryption. Every process
	// serving OAuth flows builds it, so a missing configuration stops that
	// process at startup.
	// TODO(AIM-1001): seal and open OAuth credentials with it.
	OrgEncryption orgEncryptionDeps
}

// Both listener processes must use identical identity verification and private
// authority checks when resuming the OAuth flows that cross their boundary.
// newTunnelHTTPClient builds the transport that carries back-channel OAuth
// calls (token exchange, refresh, revocation, dynamic client registration) for
// remote session issuers bound to a tunneled MCP server, instead of dialing
// them from cloud egress.
func newTunnelHTTPClient(c *cli.Context, guardianPolicy *guardian.Policy, stores *redisStores) (*tunnelrouting.HTTPClient, error) {
	// guardian.WithAllowedCIDRBlocks silently drops invalid CIDRs, so a typo
	// here would strand tunnels fail-closed with no signal. Reject
	// misconfiguration at startup instead.
	cidrs := c.StringSlice("tunnel-gateway-cidr-blocks")
	for _, cidr := range cidrs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, fmt.Errorf("invalid tunnel gateway CIDR block %q: %w", cidr, err)
		}
	}
	return tunnelrouting.NewHTTPClient(tunnelOAuthRoutes(c, stores), c.String("tunnel-forward-token"), guardianPolicy, cidrs), nil
}

// tunnelAuthRouteMirrorFlagName turns on reading tunneled OAuth routes from
// the auth role's mirror.
const tunnelAuthRouteMirrorFlagName = "tunnel-auth-route-mirror"

func tunnelAuthRouteMirrorFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    tunnelAuthRouteMirrorFlagName,
		Usage:   "Read routes for tunneled OAuth back-channel calls from the auth role's route mirror first, falling back to the ops role's routes. Turn on once the tunnel gateway publishes the mirror",
		EnvVars: []string{"GRAM_TUNNEL_AUTH_ROUTE_MIRROR"},
		Value:   false,
	}
}

// tunnelOAuthRoutes is the route store for OAuth back-channel calls over a
// tunnel. Unless the mirror flag is on it is the tunnel gateway's bulk routes
// on the ops role. With the flag on, a tunnel the gateway mirrors to the auth
// role is served from there, so tunneled OAuth keeps working while the ops
// instance is down, full or being moved.
func tunnelOAuthRoutes(c *cli.Context, stores *redisStores) route.Store {
	bulk := route.NewRedis(stores.Ops)
	if !c.Bool(tunnelAuthRouteMirrorFlagName) {
		return bulk
	}
	return route.NewMirrorReader(route.NewAuthMirror(stores.Auth), bulk)
}

func newMCPRemoteSessionDependencies(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, enc *encryption.Client, guardianPolicy *guardian.Policy, tunnels *tunnelrouting.HTTPClient, stores *redisStores, serverURL *url.URL, callbackOrigins remotesessions.CallbackOrigins, auditLogger *audit.Logger, assertionSigner remotesessions.TokenEndpointAssertionSigner, protectedResources *protectedresource.Prober, features feature.Provider, connections *mcpconnectionattempts.Recorder, orgEncryption orgEncryptionDeps) (*mcpRemoteSessionDependencies, error) {
	idTokenKeys, err := remotesessions.NewIDTokenKeyResolver(logger, guardianPolicy, meterProvider, ratelimit.NewRedisStore(stores.Auth))
	if err != nil {
		return nil, fmt.Errorf("initialize remote session id token key resolver: %w", err)
	}
	verifier := remotesessions.NewIDTokenVerifier(idTokenKeys)
	refresher := remotesessions.NewIssuerMetadataRefresher(logger, meterProvider, db, guardianPolicy, tunnels, auditLogger)
	enricher := remotesessions.NewSessionEnricher(logger, enc, guardianPolicy, idTokenKeys,
		ratelimit.New(ratelimit.NewRedisStore(stores.Auth), "remote_session_enrichment", remotesessions.EnrichmentRate, ratelimit.WithMetrics(meterProvider)), tunnels, refresher)
	challenges := remotesessions.NewChallengeManager(logger, tracerProvider, meterProvider, db, enc, guardianPolicy, tunnels, stores.AuthCache, serverURL,
		remotesessions.WithClientCredentialSource(func(m *remotesessions.ChallengeManager) remotesessions.ClientCredentialSource {
			return clientcredentials.New(logger, db, enc, m, stores.AuthCache)
		}),
		remotesessions.WithPrivateAuthorityValidator(func(ctx context.Context, state remotesessions.RemoteLoginState) error {
			return mcp.ValidateRemoteLoginPrivateAuthority(ctx, db, logger, state)
		}),
		remotesessions.WithIDTokenVerifier(verifier),
		remotesessions.WithIssuerMetadataRefresher(refresher),
		remotesessions.WithSessionEnricher(enricher),
		remotesessions.WithRegistrationAuditLogger(auditLogger),
		remotesessions.WithTokenEndpointAssertionSigner(assertionSigner),
		remotesessions.WithCallbackOrigins(callbackOrigins),
		remotesessions.WithConnectionRecorder(connections),
		remotesessions.WithProtectedResourceProber(protectedResources),
		remotesessions.WithFeatureFlags(features),
		remotesessions.WithClientSecrets(orgEncryption.ClientSecrets(logger, enc)),
		remotesessions.WithUpstreamTokens(orgEncryption.UpstreamTokens(logger, enc)))
	return &mcpRemoteSessionDependencies{Verifier: verifier, Refresher: refresher, Enricher: enricher, Challenges: challenges, OrgEncryption: orgEncryption}, nil
}
