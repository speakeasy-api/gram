package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestNewOrganizationsGetThePersonalAuthClientDefault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_personal_default"
	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, organizationID))
	none, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{AllUsersPrincipal()})
	require.NoError(t, err)
	require.Empty(t, none, "reconciling roles never adds it; only organization creation does")
	require.NoError(t, SeedPersonalAuthClientDefaultTx(ctx, conn, organizationID))

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{AllUsersPrincipal()})
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, ScopeMCPAuth, grants[0].Scope)
	require.Equal(t, PersonalAuthClientSelector(), grants[0].Selector)

	user := GrantsToContext(enterpriseSessionCtxWithOrg(t, organizationID), grants)
	own := MCPAuthClientCheck("client_1", MCPAuthClientDimensions{ClientType: AuthClientTypeRemoteSession, ProjectID: "p", ConnectionOwner: sessionUserID})
	foreign := MCPAuthClientCheck("client_1", MCPAuthClientDimensions{ClientType: AuthClientTypeRemoteSession, ProjectID: "p", ConnectionOwner: "someone_else"})
	shared := MCPAuthClientCheck("client_1", MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: "p", ConnectionOwner: ""})
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	requireMandatory(t, engine, user, MandatoryDecision{Allowed: true, Source: MandatoryPolicySourceRequest, Reason: MandatoryDenyReasonNone}, own)
	requireMandatory(t, engine, user, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceRequest, Reason: MandatoryDenyReasonNotGranted}, foreign)
	requireMandatory(t, engine, user, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceRequest, Reason: MandatoryDenyReasonNotGranted}, shared)

	require.NoError(t, SeedPersonalAuthClientDefaultTx(ctx, conn, organizationID))
	grants, err = LoadGrants(ctx, conn, organizationID, []urn.Principal{AllUsersPrincipal()})
	require.NoError(t, err)
	require.Len(t, grants, 1, "seeding twice keeps one grant")
}
