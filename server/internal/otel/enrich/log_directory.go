package enrich

import (
	"context"
	"log/slog"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/singleflight"
)

type logDirectory struct {
	logger    *slog.Logger
	replicaDB database.DBTX
	cache     cache.TypedCacheObject[cachedUserEnrichment]
	loads     singleflight.Group
}

func NewLogDirectory(logger *slog.Logger, replicaDB database.DBTX, cacheImpl cache.Cache) *logDirectory {
	logger = logger.With(attr.SlogComponent("enrich-log-directory"))
	return &logDirectory{
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

func (*logDirectory) Name() string {
	return "enrich-directory"
}

func (e *logDirectory) Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	organizationID := record.GetProvenance().GetOrganizationId()
	_, email, err := dialect.ForLog(record).ExternalUserEmail(record)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to read user email for directory log enrichment", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil, nil
	}
	resolved, err := fetchUserEnrichment(ctx, e.replicaDB, &e.cache, &e.loads, organizationID, email)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to resolve user enrichment", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	}
	return resolved.attributes(), nil
}
