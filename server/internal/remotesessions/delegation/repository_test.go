package delegation

import (
	"testing"
	"time"

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

func TestDelegationTimeNullable(t *testing.T) {
	t.Parallel()
	require.False(t, delegationTime(time.Time{}).Valid)
	now := time.Now()
	got := delegationTime(now)
	require.True(t, got.Valid)
	require.Equal(t, now, got.Time)
	require.Equal(t, pgtype.Finite, got.InfinityModifier)
}
