package risk_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// TestAuthorizationModesAgree runs organization administration and risk
// policy applicability through the engine, a shadow comparison and the
// registry evaluator: every mode must allow, deny and select the same.
func TestAuthorizationModesAgree(t *testing.T) {
	t.Parallel()

	for _, mode := range []authz.MigrationMode{authz.MigrationModeOld, authz.MigrationModeShadow, authz.MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestRiskService(t, func(ti *testInstance) { ti.authzMode = &mode })
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			organizationID := authCtx.ActiveOrganizationID
			projectID := authCtx.ProjectID.String()
			orgAdmin := authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, organizationID)}

			listPolicies := func(t *testing.T, grants ...authz.Grant) error {
				t.Helper()
				if _, err := ti.service.ListRiskPolicies(withExactAccessGrants(t, ctx, ti.conn, grants...), &gen.ListRiskPoliciesPayload{}); err != nil {
					return fmt.Errorf("list risk policies: %w", err)
				}
				return nil
			}
			require.NoError(t, listPolicies(t, orgAdmin))
			requireOopsCode(t, listPolicies(t), oops.CodeForbidden)
			requireOopsCode(t, listPolicies(t,
				authz.Grant{Scope: authz.ScopeProjectWrite, Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID)},
			), oops.CodeForbidden)

			adminCtx := withExactAccessGrants(t, ctx, ti.conn, orgAdmin)
			allServers := &types.RiskMCPScope{AllServers: true, Servers: []*types.RiskMCPServerScope{}}
			for _, policy := range []*gen.CreateRiskPolicyPayload{
				{Name: new("Everyone"), Sources: []string{"gitleaks"}, Action: "block", AudienceType: "everyone", McpScope: allServers},
				{Name: new("Targeted"), Sources: []string{"gitleaks"}, Action: "block", AudienceType: "targeted", AudiencePrincipalUrns: []string{"user:" + authCtx.UserID}, McpScope: allServers},
			} {
				_, err := ti.service.CreateRiskPolicy(adminCtx, policy)
				require.NoError(t, err)
			}

			engine := authz.NewEngine(testenv.NewLogger(t), ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
			core := policycore.New(ti.conn).WithAuthorization(authztest.NewClientInMode(t, engine, mode))
			list := func(principal policycore.MCPPrincipal) []string {
				t.Helper()
				policies, err := core.ListEnabledForMCP(ctx, organizationID, *authCtx.ProjectID, policycore.MCPTarget{
					ServerID:        uuid.New(),
					ToolName:        "send_message",
					ToolAnnotations: nil,
					PlatformToolset: true,
					Principal:       &principal,
				})
				require.NoError(t, err)
				names := make([]string, 0, len(policies))
				for _, policy := range policies {
					names = append(names, policy.Name)
				}
				return names
			}
			require.ElementsMatch(t, []string{"Everyone", "Targeted"}, list(policycore.MCPPrincipal{UserID: authCtx.UserID, AgentID: ""}))
			require.ElementsMatch(t, []string{"Everyone"}, list(policycore.MCPPrincipal{UserID: "", AgentID: ""}))
		})
	}
}

// TestPolicyCoreWithoutAuthorizationFailsClosed keeps a core that was never
// given a client from guessing which policies apply.
func TestPolicyCoreWithoutAuthorizationFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	adminCtx := withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	_, err := ti.service.CreateRiskPolicy(adminCtx, &gen.CreateRiskPolicyPayload{
		Name: new("Everyone"), Sources: []string{"gitleaks"}, Action: "block", AudienceType: "everyone",
		McpScope: &types.RiskMCPScope{AllServers: true, Servers: []*types.RiskMCPServerScope{}},
	})
	require.NoError(t, err)
	_, err = policycore.New(ti.conn).ListEnabledForMCP(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, policycore.MCPTarget{
		ServerID:        uuid.New(),
		ToolName:        "send_message",
		ToolAnnotations: nil,
		PlatformToolset: true,
		Principal:       &policycore.MCPPrincipal{UserID: authCtx.UserID, AgentID: ""},
	})
	require.Error(t, err)
}
