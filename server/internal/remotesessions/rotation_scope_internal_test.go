package remotesessions

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestRotationRegistrationScope(t *testing.T) {
	t.Parallel()

	row := func(clientScope []string) repo.GetRemoteSessionClientForRotationRow {
		return repo.GetRemoteSessionClientForRotationRow{
			RemoteSessionClient:     repo.RemoteSessionClient{Scope: clientScope},
			IssuerScopesSupported:   []string{"read", "write"},
			IssuerScopeOverride:     nil,
			IssuerOmitScopeFallback: pgtype.Bool{Bool: false, Valid: false},
		}
	}

	require.Equal(t, []string{"read"}, rotationRegistrationScope(row([]string{"read"}), []string{"files:read"}), "a stored scope wins")
	require.Equal(t, []string{"read", "write", "files:read"}, rotationRegistrationScope(row(nil), []string{"write", "files:read"}), "the login's request widens the issuer scope")
	require.Equal(t, []string{"read", "write"}, rotationRegistrationScope(row(nil), nil), "without a login, the issuer catalogue")

	override := row(nil)
	override.IssuerScopeOverride = []string{"read"}
	require.Equal(t, []string{"read"}, rotationRegistrationScope(override, nil), "the issuer override applies")

	omit := row(nil)
	omit.IssuerScopesSupported = nil
	omit.IssuerOmitScopeFallback = pgtype.Bool{Bool: true, Valid: true}
	require.Empty(t, rotationRegistrationScope(omit, nil), "an issuer set to send no scope registers none")
}
