package otel

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

type enrichLogDirectory struct {
	logger *slog.Logger
	db     database.DBTX
	cache  cache.TypedCacheObject[cachedUserEnrichment]
	loads  singleflight.Group
}

func newEnrichLogDirectory(logger *slog.Logger, db database.DBTX, cacheImpl cache.Cache) *enrichLogDirectory {
	logger = logger.With(attr.SlogComponent("enrich-log-directory"))
	return &enrichLogDirectory{
		logger: logger,
		db:     db,
		cache: cache.NewTypedObjectCache[cachedUserEnrichment](
			logger.With(attr.SlogCacheNamespace("otel_user_enrichment")),
			cacheImpl,
			cache.SuffixNone,
		),
		loads: singleflight.Group{},
	}
}

func (*enrichLogDirectory) Name() string {
	return "enrich-directory"
}

func (e *enrichLogDirectory) Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	organizationID := record.GetProvenance().GetOrganizationId()
	_, email, err := dialect.ForLog(record).ExternalUserEmail(record)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to read user email for directory log enrichment", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil, nil
	}
	resolved, err := fetchUserEnrichment(ctx, e.db, &e.cache, &e.loads, organizationID, email)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to resolve user enrichment", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	}
	return resolved.attributes(), nil
}
