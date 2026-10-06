package protectedresource

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

func TestDataErrorClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "raw JSON Unicode", err: &pgconn.PgError{Code: pgerrcode.UntranslatableCharacter}, want: true},
		{name: "extracted text NUL", err: &pgconn.PgError{Code: pgerrcode.CharacterNotInRepertoire}, want: true},
		{name: "JSON syntax", err: &pgconn.PgError{Code: pgerrcode.InvalidTextRepresentation}, want: true},
		{name: "JSON number overflow", err: &pgconn.PgError{Code: pgerrcode.NumericValueOutOfRange}, want: true},
		{name: "wrapped representation error", err: fmt.Errorf("write: %w", &pgconn.PgError{Code: pgerrcode.UntranslatableCharacter}), want: true},
		{name: "connection failure", err: &pgconn.PgError{Code: pgerrcode.ConnectionException}, want: false},
		{name: "constraint failure", err: &pgconn.PgError{Code: pgerrcode.ForeignKeyViolation}, want: false},
		{name: "timeout", err: context.DeadlineExceeded, want: false},
		{name: "no error", err: nil, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, isDataError(tc.err))
		})
	}
}

func TestMismatchMessage(t *testing.T) {
	t.Parallel()

	doc := wellknown.OAuthProtectedResourceMetadata{Resource: "https://rs.example.test/other", MetadataURL: "https://rs.example.test/.well-known/oauth-protected-resource"}
	require.Equal(t, "The metadata document names the resource https://rs.example.test/other, not the requested one.", MismatchMessage("https://rs.example.test/mcp", doc))

	doc.Resource = "https://rs.example.test/mcp"
	require.Equal(t, "The metadata document was read from https://rs.example.test/.well-known/oauth-protected-resource, not the resource's well-known location.", MismatchMessage("https://rs.example.test/mcp", doc))

	doc.Resource = ""
	require.Equal(t, "The metadata document names the resource (empty), not the requested one.", MismatchMessage("https://rs.example.test/mcp", doc))

	doc.Resource = "https://user:secret@rs.example.test/other?token=abc#frag"
	require.Equal(t, "The metadata document names the resource https://rs.example.test/other, not the requested one.", MismatchMessage("https://rs.example.test/mcp", doc))

	doc.Resource = "urn:example:resource"
	require.Equal(t, "The metadata document names the resource <invalid URL>, not the requested one.", MismatchMessage("https://rs.example.test/mcp", doc))

	doc.Resource = "https://rs.example.test/" + strings.Repeat("é", 300)
	got := MismatchMessage("https://rs.example.test/mcp", doc)
	require.True(t, utf8.ValidString(got))
	require.Equal(t, 200, utf8.RuneCountInString(strings.TrimSuffix(strings.TrimPrefix(got, "The metadata document names the resource "), "…, not the requested one.")))
}

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

func TestDiagnosticStorage(t *testing.T) {
	t.Parallel()
	const resource = "https://example.test/mcp%2Froute?arbitrary=secret"
	const metadataURL = "https://example.test/.well-known/oauth-protected-resource/mcp%2Froute?arbitrary=secret"
	const safeMetadataURL = "https://example.test/.well-known/oauth-protected-resource/mcp%2Froute"
	raw := []byte(`{"resource":"` + resource + `"}`)
	doc := wellknown.OAuthProtectedResourceMetadata{Resource: resource, MetadataURL: metadataURL, Raw: raw}
	require.True(t, doc.ValidForResource(resource))
	db := &diagnosticWriteDB{}
	require.NoError(t, Record(t.Context(), db, uuid.New(), "test-org", resource, doc))
	require.Equal(t, resource, db.args[2])
	require.Equal(t, safeMetadataURL, db.args[3])
	require.Equal(t, string(raw), db.args[14])
	require.Equal(t, metadataURL, doc.MetadataURL)
	require.NoError(t, RecordError(t.Context(), db, uuid.New(), "test-org", resource, metadataURL, "safe failure"))
	require.Equal(t, resource, db.args[2])
	require.Equal(t, safeMetadataURL, db.args[3])
	require.Equal(t, "safe failure", db.args[4])
}
