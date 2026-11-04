package gram

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// newGrantsAuthzClient builds an authorization client for processes that only
// evaluate grants they load themselves, such as stream consumers deciding
// risk policy bypasses. No request is authorized there, so the engine has no
// membership source and never logs challenges. Use newAuthzClient wherever a
// request's own access is checked.
func newGrantsAuthzClient(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, flags feature.Provider) (*authz.Client, error) {
	noChallengeLogging := func(context.Context, string) (bool, error) { return false, nil }
	engine := authz.NewEngine(logger, db, noChallengeLogging, nil)
	return newAuthzClient(logger, meterProvider, engine, flags)
}
