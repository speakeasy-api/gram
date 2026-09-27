package mockworkos_test

import (
	"database/sql"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/modes/mockworkos"
	"github.com/speakeasy-api/gram/plog"
)

func openOrganizationEmulator(t *testing.T, path string) (*sql.DB, http.Handler) {
	t.Helper()
	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeFile, Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	handler := mockworkos.NewHandler(plog.NewLogger(io.Discard), tracenoop.NewTracerProvider(), db)
	return db, handler.Handler()
}
