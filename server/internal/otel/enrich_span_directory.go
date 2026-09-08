package otel

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/singleflight"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

type enrichDirectory struct {
	logger    *slog.Logger
	replicaDB database.DBTX
	cache     cache.TypedCacheObject[cachedUserEnrichment]
	loads     singleflight.Group
}

func NewEnrichDirectory(logger *slog.Logger, replicaDB database.DBTX, cacheImpl cache.Cache) *enrichDirectory {
	logger = logger.With(attr.SlogComponent("enrich-directory"))
	return &enrichDirectory{
		logger:    logger,
		replicaDB: replicaDB,
		cache: cache.NewTypedObjectCache[cachedUserEnrichment](
			logger.With(attr.SlogCacheNamespace("otel_user_enrichment")),
			cacheImpl,
			cache.SuffixNone,
		),
		loads: singleflight.Group{},
	}
}

func (e *enrichDirectory) Name() string {
	return "enrich-directory"
}

func (e *enrichDirectory) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	organizationID := span.GetProvenance().GetOrganizationId()
	_, email, err := dialect.ForSpan(span).ExternalUserEmail(span)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to read user email for directory span enrichment", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil, nil
	}
	resolved, err := fetchUserEnrichment(ctx, e.replicaDB, &e.cache, &e.loads, organizationID, email)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to resolve user enrichment", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	}
	return resolved.attributes(), nil
}
