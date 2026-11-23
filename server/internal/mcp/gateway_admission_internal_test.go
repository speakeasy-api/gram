package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestGatewayCallerFromContextPerson(t *testing.T) {
	t.Parallel()
	for _, subject := range []urn.SessionSubject{
		urn.NewUserSubject("user_test"),
		urn.NewAPIKeySubject(uuid.New()),
	} {
		principal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_test")
		ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{}, principal)
		caller, err := gatewayCallerFromContext(ctx, subject)

		require.NoError(t, err)
		require.Equal(t, gatewayCaller{
			kind:         gatewayCallerPerson,
			principal:    principal,
			owner:        "",
			ceiling:      nil,
			projectScope: nil,
		}, caller)
	}
}

func TestGatewayCallerFromContextAgentUsesAuthorizerAndCredentialCeiling(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	agentID := uuid.New()
	authorizer := "user_authorizer"
	currentOwner := "user_current_owner"
	ceiling := []authz.Grant{mcpProjectGrant(projectID, "credential")}
	agentPolicy := []authz.Grant{mcpProjectGrant(projectID, "agent")}
	ownerPolicy := []authz.Grant{mcpProjectGrant(projectID, "owner")}
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String())
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_test",
		ProjectID:            &projectID,
	}, actor, contextvalues.PrincipalCredential{
		AuthorizerUserID:         authorizer,
		DelegatedGrants:          nil,
		DelegatedGrantsVersion:   0,
		DerivesPolicyAtAdmission: true,
	})
	engine := authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), authz.EngineOpts{
		AdmitPrincipalCredential: func(context.Context, *pgxpool.Pool) (authz.PrincipalCredentialAdmission, error) {
			return authz.PrincipalCredentialAdmission{
				OwnerUserID:       currentOwner,
				Credential:        ceiling,
				Agent:             agentPolicy,
				Owner:             ownerPolicy,
				Authorizer:        authz.RepresentedUserPolicy{UserID: authorizer, Policy: ownerPolicy},
				LegacyAuthCeiling: false,
			}, nil
		},
	})
	ctx, err := engine.AdmitPrincipalCredential(ctx)
	require.NoError(t, err)

	caller, err := gatewayCallerFromContext(ctx, urn.NewAgentSubject(agentID))

	require.NoError(t, err)
	require.Equal(t, gatewayCallerAgent, caller.kind)
	require.Equal(t, authorizer, caller.owner)
	require.Equal(t, ceiling, caller.ceiling)
	require.Equal(t, projectID, *caller.projectScope)
	require.NotEqual(t, currentOwner, caller.owner)
	require.NotEqual(t, agentPolicy, caller.ceiling)
	require.NotEqual(t, ownerPolicy, caller.ceiling)
}

func TestGatewayCallerFromContextWorkloadUsesLiveOwnerAndCredentialCeiling(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	agentID := uuid.New()
	owner := "user_current_owner"
	ceiling := []authz.Grant{mcpProjectGrant(projectID, "credential")}
	workloadIssuerID := uuid.New()
	subject := urn.NewWorkloadSubject(workloadIssuerID, "repo:example/project")
	engine := authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), authz.EngineOpts{
		AdmitWorkloadSession: func(context.Context, *pgxpool.Pool) (authz.WorkloadSessionAdmission, error) {
			return authz.WorkloadSessionAdmission{
				AgentPrincipal:    urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()).String(),
				OwnerUserID:       owner,
				Ceiling:           ceiling,
				Agent:             []authz.Grant{mcpProjectGrant(projectID, "agent")},
				Owner:             []authz.Grant{mcpProjectGrant(projectID, "owner")},
				LegacyAuthCeiling: false,
			}, nil
		},
	})
	workloadPrincipal := urn.NewWorkloadPrincipal(workloadIssuerID, "repo:example/project")
	base := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{}, workloadPrincipal)
	ctx, err := engine.AdmitWorkloadSession(base)
	require.NoError(t, err)

	caller, err := gatewayCallerFromContext(ctx, subject)

	require.NoError(t, err)
	require.Equal(t, gatewayCallerWorkload, caller.kind)
	require.Equal(t, owner, caller.owner)
	require.Equal(t, ceiling, caller.ceiling)
	require.Equal(t, projectID, *caller.projectScope)
}

func TestGatewayCallerProjectScope(t *testing.T) {
	t.Parallel()
	projectOne, projectTwo := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name    string
		ceiling []authz.Grant
		want    *uuid.UUID
	}{
		{name: "empty", ceiling: nil, want: nil},
		{name: "one project", ceiling: []authz.Grant{mcpProjectGrant(projectOne, "one")}, want: &projectOne},
		{name: "same project", ceiling: []authz.Grant{mcpProjectGrant(projectOne, "one"), mcpProjectGrant(projectOne, "two")}, want: &projectOne},
		{name: "several projects", ceiling: []authz.Grant{mcpProjectGrant(projectOne, "one"), mcpProjectGrant(projectTwo, "two")}, want: nil},
		{name: "unscoped", ceiling: []authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP}}}, want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := gatewayCallerProjectScope(tc.ceiling)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func mcpProjectGrant(projectID uuid.UUID, resourceID string) authz.Grant {
	return authz.Grant{
		PrincipalUrn: "",
		Scope:        authz.ScopeMCPConnect,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   resourceID,
			authz.SelectorKeyProjectID:    projectID.String(),
		},
		Inferred: false,
	}
}
