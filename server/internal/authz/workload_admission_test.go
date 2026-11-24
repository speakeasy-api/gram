package authz

import (
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMandatoryWorkloadAuthClientUsesAdmittedOwner(t *testing.T) {
	t.Parallel()
	const org = "org_workload"
	const owner = "user_owner"
	allow := []Grant{NewGrantWithSelector(ScopeMCPAuth, PersonalAuthClientSelector())}
	admission := WorkloadSessionAdmission{
		AgentPrincipal: "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001",
		OwnerUserID:    owner, Ceiling: allow, Agent: allow, Owner: allow, LegacyAuthCeiling: false,
	}
	actor := urn.NewWorkloadPrincipal(uuid.New(), "workload")
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: org}, actor, contextvalues.PrincipalCredential{})
	ctx, err := withWorkloadSessionAdmission(ctx, admission)
	require.NoError(t, err)
	authorization, err := (&Engine{}).PrepareMandatory(ctx)
	require.NoError(t, err)
	require.Equal(t, MandatoryCallerWorkload, authorization.Caller())
	require.Empty(t, authorization.RepresentedUserID())
	for _, tc := range []struct {
		org, owner string
		allowed    bool
	}{
		{org: org, owner: owner, allowed: true},
		{org: org, owner: "different_user", allowed: false},
		{org: "different_org", owner: owner, allowed: false},
	} {
		decision, err := authorization.DecideAuthClientAdmission(tc.org, MandatoryServerPrivate, personalClientCheck(tc.owner))
		require.NoError(t, err)
		require.Equal(t, tc.allowed, decision.Allowed)
	}
	admission.Agent = nil
	ctx, err = withWorkloadSessionAdmission(ctx, admission)
	require.NoError(t, err)
	authorization, err = (&Engine{}).PrepareMandatory(ctx)
	require.NoError(t, err)
	decision, err := authorization.DecideAuthClientAdmission(org, MandatoryServerPrivate, personalClientCheck(owner))
	require.NoError(t, err)
	require.False(t, decision.Allowed, "the workload owner alone cannot grant use")
}
