package remotesessions

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
)

// globalActor is audit metadata, never a dashboard credential.
type globalActor struct{ email string }

// authorizeGlobalOperation accepts trusted transport context only for global operations.
// Tenant handlers continue to require their existing AuthContext and RBAC grants.
func authorizeGlobalOperation(ctx context.Context, logger *slog.Logger) (globalActor, *slog.Logger, error) {
	if a, ok := contextvalues.GetAdminAuthContext(ctx); ok {
		if a == nil || a.SessionID == "" || a.OIDCSubject == "" {
			return globalActor{}, logger, oops.C(oops.CodeUnauthorized)
		}
		return globalActor{email: a.Email}, logger.With(attr.SlogAdminOIDCSubject(a.OIDCSubject), attr.SlogAuthSource("gram_admin")), nil
	}
	a, logger, err := auth.RequirePlatformAdmin(ctx, logger)
	if err != nil {
		return globalActor{}, logger, err
	}
	return globalActor{email: conv.PtrValOrEmpty(a.Email, "")}, logger, nil
}

// NewGlobalService deliberately omits tenant authentication and mounts no routes.
func NewGlobalService(logger *slog.Logger, tp trace.TracerProvider, mp metric.MeterProvider, db *pgxpool.Pool, enc *encryption.Client, policy *guardian.Policy) *Service {
	return &Service{auth: nil, sessions: nil, authz: nil, environments: nil, auditLogger: nil, serverURL: nil, refresher: nil, productFeatures: nil, logger: logger, tracer: tp.Tracer("github.com/speakeasy-api/gram/server/internal/remotesessions"), db: db, enc: enc, policy: policy, revoker: NewUpstreamRevoker(logger, tp, mp, db, enc, policy)}
}
