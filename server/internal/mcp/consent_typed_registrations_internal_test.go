package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
		subject := urn.NewUserSubject("user_1")
		picks, err := f.service.consentDirectAuthClientPick(t.Context(), &ResolvedMcpEndpoint{OrganizationID: f.orgID, McpServerID: validUUID(f.serverID)}, AuthnChallengeState{Subject: &subject}, "", nil, nil)
		require.NoError(t, err)
		require.Nil(t, picks, "non-enforced approval must not need card-building dependencies")
	}
	f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
	f.enforce()
	calls := 0
	typed, err := f.service.typedConsentRegistrations(t.Context(), endpoint, func() ([]metaMember, error) {
		calls++
		return []metaMember{{serverID: f.serverID, projectID: f.projectID, backend: metaMemberBackendProxied}}, nil
	})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]uuid.UUID{f.registration: f.projectID}, typed)
	require.Equal(t, 1, calls)
}
