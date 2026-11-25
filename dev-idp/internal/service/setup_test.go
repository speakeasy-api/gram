package service

import (
	"database/sql"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/plog"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func testEmaAppsService(t *testing.T, db *sql.DB) *EmaAppsService {
	t.Helper()
	return NewEmaAppsService(plog.NewLogger(io.Discard), tracenoop.NewTracerProvider(), db)
}

func testEmaTrustRulesService(t *testing.T, db *sql.DB) *EmaTrustRulesService {
	t.Helper()
	return NewEmaTrustRulesService(plog.NewLogger(io.Discard), tracenoop.NewTracerProvider(), db)
}
