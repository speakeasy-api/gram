package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestTypedConsentRegistrationsSkipsMembersUnlessEnforced(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_typed_consent_members")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	flags := new(feature.InMemory)
	f.service.features = flags
	f.service.authEnforcement = authenforcement.NewGate(pool)
	endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, MetaMcpServerID: validUUID(uuid.New())}
	for _, shadow := range []bool{false, true} {
		flags.SetFlag(feature.FlagAuthClientResolver, f.orgID, shadow)
		typed, err := f.service.typedConsentRegistrations(t.Context(), endpoint, func() ([]metaMember, error) {
			t.Fatal("disabled typed resolution must not load the member snapshot")
			return nil, nil
		})
		require.NoError(t, err)
		require.Empty(t, typed)
	}
	f.enforce()
	calls := 0
	typed, err := f.service.typedConsentRegistrations(t.Context(), endpoint, func() ([]metaMember, error) {
		calls++
		return nil, nil
	})
	require.NoError(t, err)
	require.Empty(t, typed)
	require.Equal(t, 1, calls)
}
