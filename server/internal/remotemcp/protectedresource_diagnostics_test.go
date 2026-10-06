package remotemcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/stretchr/testify/require"
)

// Capture write arguments without a database; these helpers do not use returned rows.
type diagnosticWriteDB struct {
	repo.DBTX
	args []any
}

func (d *diagnosticWriteDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	d.args = args
	return diagnosticWriteRow{}
}

type diagnosticWriteRow struct{}

func (diagnosticWriteRow) Scan(...any) error { return nil }

func TestProtectedResourceDiagnosticStorage(t *testing.T) {
	t.Parallel()
	const resource = "https://example.test/mcp%2Froute?arbitrary=secret"
	const metadataURL = "https://example.test/.well-known/oauth-protected-resource/mcp%2Froute?arbitrary=secret"
	const safeMetadataURL = "https://example.test/.well-known/oauth-protected-resource/mcp%2Froute"
	raw := []byte(`{"resource":"` + resource + `"}`)
	doc := wellknown.OAuthProtectedResourceMetadata{Resource: resource, MetadataURL: metadataURL, Raw: raw}
	require.True(t, doc.ValidForResource(resource))
	db := &diagnosticWriteDB{}
	require.NoError(t, recordProtectedResource(t.Context(), db, uuid.New(), "test-org", resource, doc))
	require.Equal(t, resource, db.args[2])
	require.Equal(t, safeMetadataURL, db.args[3])
	require.Equal(t, string(raw), db.args[14])
	require.Equal(t, metadataURL, doc.MetadataURL)
	require.NoError(t, recordProtectedResourceError(t.Context(), db, uuid.New(), "test-org", resource, metadataURL, "safe failure"))
	require.Equal(t, resource, db.args[2])
	require.Equal(t, safeMetadataURL, db.args[3])
	require.Equal(t, "safe failure", db.args[4])
}
