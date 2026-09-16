package gram

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

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
)

type mcpRemoteSessionDependencies struct {
	Verifier   remotesessions.IDTokenVerifier
	Refresher  *remotesessions.IssuerMetadataRefresher
	Enricher   *remotesessions.SessionEnricher
	Challenges *remotesessions.ChallengeManager
}

// Both listener processes must use identical identity verification and private
// authority checks when resuming the OAuth flows that cross their boundary.
func newMCPRemoteSessionDependencies(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, enc *encryption.Client, guardianPolicy *guardian.Policy, redisClient *redis.Client, serverURL *url.URL, auditLogger *audit.Logger, assertionSigner remotesessions.TokenEndpointAssertionSigner) (*mcpRemoteSessionDependencies, error) {
	idTokenKeys, err := remotesessions.NewIDTokenKeyResolver(logger, guardianPolicy, meterProvider, ratelimit.NewRedisStore(redisClient))
	if err != nil {
		return nil, fmt.Errorf("initialize remote session id token key resolver: %w", err)
	}
	verifier := remotesessions.NewIDTokenVerifier(idTokenKeys)
	refresher := remotesessions.NewIssuerMetadataRefresher(logger, meterProvider, db, guardianPolicy, auditLogger)
	enricher := remotesessions.NewSessionEnricher(logger, enc, guardianPolicy, idTokenKeys,
		ratelimit.New(ratelimit.NewRedisStore(redisClient), "remote_session_enrichment", remotesessions.EnrichmentRate, ratelimit.WithMetrics(meterProvider)), refresher)
	challenges := remotesessions.NewChallengeManager(logger, tracerProvider, meterProvider, db, enc, guardianPolicy, cache.NewRedisCacheAdapter(redisClient), serverURL,
		remotesessions.WithPrivateAuthorityValidator(func(ctx context.Context, state remotesessions.RemoteLoginState) error {
			return mcp.ValidateRemoteLoginPrivateAuthority(ctx, db, logger, state)
		}),
		remotesessions.WithIDTokenVerifier(verifier),
		remotesessions.WithIssuerMetadataRefresher(refresher),
		remotesessions.WithSessionEnricher(enricher),
		remotesessions.WithRegistrationAuditLogger(auditLogger),
		remotesessions.WithTokenEndpointAssertionSigner(assertionSigner))
	return &mcpRemoteSessionDependencies{Verifier: verifier, Refresher: refresher, Enricher: enricher, Challenges: challenges}, nil
}
