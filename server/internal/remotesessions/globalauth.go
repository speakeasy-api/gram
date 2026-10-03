package remotesessions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"go.opentelemetry.io/otel/metric"
)

// globalActor is audit metadata, never a dashboard credential.
type globalActor struct{ email string }

// authorizeGlobalOperation accepts trusted transport context only for global operations.
// Tenant handlers continue to require their existing AuthContext and RBAC grants.
func authorizeGlobalOperation(ctx context.Context, logger *slog.Logger) (globalActor, *slog.Logger, error) {
	email, logger, err := auth.RequireGlobalAdmin(ctx, logger)
	if err != nil {
		return globalActor{email: email}, logger, fmt.Errorf("authorize global operation: %w", err)
	}
	return globalActor{email: email}, logger, nil
}

// GlobalIssuers curates the global identity providers shared by every
// organization. It carries no tenant authentication and mounts no routes; the
// admin service and the remote sessions service both serve its methods.
type GlobalIssuers struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	policy       *guardian.Policy
	jwksResolver *jwks.Resolver
}

func NewGlobalIssuers(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, policy *guardian.Policy) *GlobalIssuers {
	logger = logger.With(attr.SlogComponent("remotesessions"))
	return newGlobalIssuers(logger, db, policy, jwks.NewResolver(policy, meterProvider, logger))
}

func newGlobalIssuers(logger *slog.Logger, db *pgxpool.Pool, policy *guardian.Policy, jwksResolver *jwks.Resolver) *GlobalIssuers {
	return &GlobalIssuers{logger: logger, db: db, policy: policy, jwksResolver: jwksResolver}
}
