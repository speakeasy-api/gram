package authz

import (
	"context"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMandatoryPublicServerAdmission(t *testing.T) {
	t.Parallel()
	anonymous := MandatoryAuthorization{caller: MandatoryCallerAnonymous, publicAnonymous: true}.decide(MandatoryPolicySourceNone, MandatoryDenyReasonUnauthenticated)
	connect := MCPCheck(ScopeMCPConnect, "server_1", "project_1")
	for _, visibility := range []MandatoryServerVisibility{MandatoryServerPublic, MandatoryServerPrivate, "unknown", ""} {
		decision, err := anonymous.DecideServerAdmission("org_1", visibility, connect)
		require.NoError(t, err)
		require.Equal(t, visibility == MandatoryServerPublic, decision.Allowed)
	}
	// Raw authorization stays strict; only the explicit server boundary admits public access.
	decision, err := anonymous.Decide("org_1", connect)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	_, err = anonymous.DecideServerAdmission("org_1", MandatoryServerPublic, MCPCheck(ScopeMCPAuth, "client_1", "project_1"))
	require.Error(t, err)
	_, err = anonymous.DecideServerAdmission("org_1", MandatoryServerPublic, MCPCheck(ScopeMCPConnect, "", "project_1"))
	require.Error(t, err)
}

func TestMandatoryPublicAdmissionDoesNotBypassCredentialPolicy(t *testing.T) {
	t.Parallel()
	connect := MCPCheck(ScopeMCPConnect, "server_1", "project_1")
	for _, reason := range []MandatoryDenyReason{MandatoryDenyReasonNoPolicy, MandatoryDenyReasonInactiveCredential} {
		caller := MandatoryAuthorization{caller: MandatoryCallerUser, organizationID: "org_1"}.decide(MandatoryPolicySourceRequest, reason)
		decision, err := caller.DecideServerAdmission("org_1", MandatoryServerPublic, connect)
		require.NoError(t, err)
		require.False(t, decision.Allowed)
		require.Equal(t, reason, decision.Reason)
		decision, err = caller.DecideServerAdmission("org_other", MandatoryServerPublic, connect)
		require.NoError(t, err)
		require.False(t, decision.Allowed)
		require.Equal(t, MandatoryDenyReasonOrganizationMismatch, decision.Reason)
	}
}

func TestMandatoryPublicAdmissionRequiresAbsentIdentity(t *testing.T) {
	t.Parallel()
	for _, ctx := range []context.Context{
		contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{}),
		contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{APIKeyID: "key_1"}),
		contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{}),
	} {
		authorization, err := (&Engine{}).PrepareMandatory(ctx)
		require.NoError(t, err)
		decision, err := authorization.DecideServerAdmission("org_1", MandatoryServerPublic, MCPCheck(ScopeMCPConnect, "server_1", "project_1"))
		require.NoError(t, err)
		require.False(t, decision.Allowed)
	}
	authorization, err := (&Engine{}).PrepareMandatory(t.Context())
	require.NoError(t, err)
	decision, err := authorization.DecideServerAdmission("org_1", MandatoryServerPublic, MCPCheck(ScopeMCPConnect, "server_1", "project_1"))
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Equal(t, MandatoryPolicySourceVisibility, decision.Source)
}
