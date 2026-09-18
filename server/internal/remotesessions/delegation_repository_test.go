package remotesessions

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestCredentialFromRowNullableGeneration(t *testing.T) {
	t.Parallel()
	require.Equal(t, int64(1), credentialFromRow(repo.TrustedIssuerSession{}).generation)
	require.Equal(t, int64(7), credentialFromRow(repo.TrustedIssuerSession{
		CredentialGeneration: pgtype.Int8{Int64: 7, Valid: true},
	}).generation)
}
