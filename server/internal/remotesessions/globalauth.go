package remotesessions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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

// NewGlobalService deliberately omits tenant authentication and mounts no routes.
func NewGlobalService(logger *slog.Logger, tp trace.TracerProvider, mp metric.MeterProvider, db *pgxpool.Pool, enc *encryption.Client, policy *guardian.Policy) *Service {
	logger = logger.With(attr.SlogComponent("remotesessions"))
	return &Service{
		auth:            nil,
		sessions:        nil,
		authz:           nil,
		environments:    nil,
		auditLogger:     nil,
		serverURL:       nil,
		refresher:       nil,
		productFeatures: nil,
		logger:          logger,
		tracer:          tp.Tracer("github.com/speakeasy-api/gram/server/internal/remotesessions"),
		db:              db,
		enc:             enc,
		policy:          policy,
		revoker:         NewUpstreamRevoker(logger, tp, mp, db, enc, policy),
	}
}
