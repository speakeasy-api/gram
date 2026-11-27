package authz

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func requireOrganizationDisabled(t *testing.T, err error) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
	require.ErrorContains(t, err, OrganizationDisabledMessage)
}

func TestPrepareContext_refusesDisabledOrganization(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	conn := newTestDB(t)
	seedOrganization(t, t.Context(), conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(false), workos.NewStubClient())

	_, err := engine.PrepareContext(enterpriseSessionCtxWithOrg(t, orgID))
	require.NoError(t, err, "an enabled organization prepares as before")

	require.NoError(t, testrepo.New(conn).DisableDelegationOrganizationFixture(t.Context(), orgID))

	_, err = engine.PrepareContext(enterpriseSessionCtxWithOrg(t, orgID))
	requireOrganizationDisabled(t, err)
}

func TestPrepareContext_refusesLegacyAPIKeyInDisabledOrganization(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	conn := newTestDB(t)
	seedOrganization(t, t.Context(), conn, orgID)
	require.NoError(t, testrepo.New(conn).DisableDelegationOrganizationFixture(t.Context(), orgID))
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(false), workos.NewStubClient())

	// Legacy API keys skip grant loading, so the organization check must run
	// before that early return.
	ctx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		APIKeyID:             uuid.NewString(),
		AccountType:          "enterprise",
	})

	_, err := engine.PrepareContext(ctx)
	requireOrganizationDisabled(t, err)
}

func TestPrepareContext_refusesSupportSessionInDisabledOrganization(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	conn := newTestDB(t)
	seedOrganization(t, t.Context(), conn, orgID)
	require.NoError(t, testrepo.New(conn).DisableDelegationOrganizationFixture(t.Context(), orgID))
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(false), workos.NewStubClient())

	sessionID := "session_admin"
	ctx := contextvalues.WithValidatedSupportSession(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  orgID,
		UserID:                "user_admin",
		SessionID:             &sessionID,
		AccountType:           "enterprise",
		IsAdmin:               true,
		SupportOrganizationID: orgID,
	})

	_, err := engine.PrepareContext(ctx)
	requireOrganizationDisabled(t, err)
}

func TestPrepareContext_organizationWithoutRowIsNotRefused(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(false), workos.NewStubClient())

	_, err := engine.PrepareContext(enterpriseSessionCtxWithOrg(t, "org_"+uuid.NewString()))
	require.NoError(t, err)
}
