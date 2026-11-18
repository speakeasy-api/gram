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

func TestMandatoryPublicAuthClientAdmission(t *testing.T) {
	t.Parallel()
	anonymous := MandatoryAuthorization{caller: MandatoryCallerAnonymous, publicAnonymous: true}.decide(MandatoryPolicySourceNone, MandatoryDenyReasonUnauthenticated)
	// An anonymous caller owns no user identity, so its personal check names no owner.
	personal := MCPAuthClientCheck("client_1", MCPAuthClientDimensions{ClientType: AuthClientTypeRemoteSession, ProjectID: "project_1", ConnectionOwner: "", UpstreamClient: ""})
	shared := MCPAuthClientCheck("client_1", MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: "project_1", ConnectionOwner: "", UpstreamClient: ""})
	for _, use := range []Check{personal, shared} {
		for _, visibility := range []MandatoryServerVisibility{MandatoryServerPublic, MandatoryServerPrivate, "unknown", ""} {
			decision, err := anonymous.DecideAuthClientAdmission("org_1", visibility, use)
			if visibility == MandatoryServerPublic {
				require.NoError(t, err)
				require.True(t, decision.Allowed, "a public server serves its anonymous caller the client it resolved")
				require.Equal(t, MandatoryPolicySourceVisibility, decision.Source)
				continue
			}
			if use.Dimensions[SelectorKeyClientType] == AuthClientTypeRemoteSession {
				require.Error(t, err, "off a public server an ownerless personal check is still invalid")
				continue
			}
			require.NoError(t, err)
			require.False(t, decision.Allowed)
			require.Equal(t, MandatoryDenyReasonUnauthenticated, decision.Reason)
		}
	}
	_, err := anonymous.DecideAuthClientAdmission("org_1", MandatoryServerPublic, MCPCheck(ScopeMCPConnect, "server_1", "project_1"))
	require.Error(t, err)
}

func TestMandatoryPublicAuthClientAdmissionDoesNotBypassCredentialPolicy(t *testing.T) {
	t.Parallel()
	use := MCPAuthClientCheck("client_1", MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: "project_1", ConnectionOwner: "", UpstreamClient: ""})
	caller := MandatoryAuthorization{caller: MandatoryCallerUser, organizationID: "org_1"}.decide(MandatoryPolicySourceRequest, MandatoryDenyReasonNoPolicy)
	decision, err := caller.DecideAuthClientAdmission("org_1", MandatoryServerPublic, use)
	require.NoError(t, err)
	require.False(t, decision.Allowed, "a caller that presents a credential is decided by its policy on a public server too")
	require.Equal(t, MandatoryDenyReasonNoPolicy, decision.Reason)

	for _, ctx := range []context.Context{
		contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{}),
		contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{APIKeyID: "key_1"}),
	} {
		authorization, err := (&Engine{}).PrepareMandatory(ctx)
		require.NoError(t, err)
		decision, err := authorization.DecideAuthClientAdmission("org_1", MandatoryServerPublic, use)
		require.NoError(t, err)
		require.False(t, decision.Allowed, "a malformed credential context is not an absent identity")
	}
}
