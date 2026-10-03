package authz

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// Workload admission must replace whatever policy an earlier admission left on
// the context, or a workload whose agent holds nothing would act under the
// earlier principal's grants.
func TestWorkloadAdmissionReplacesEarlierAdmittedPolicies(t *testing.T) {
	t.Parallel()
	check := Check{Scope: ScopeProjectRead, ResourceID: "project-one"}
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{
		DevMode: false,
		AdmitWorkloadSession: func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error) {
			return WorkloadSessionAdmission{
				AuthorizerUserID: "",
				Authorizer:       nil,
				AgentPrincipal:   "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001",
				OwnerUserID:      "user_123",
				Ceiling:          []Grant{NewGrant(ScopeProjectRead, "project-one")},
				Agent:            nil,
			}, nil
		},
		AdmitPrincipalCredential:         nil,
		AdmitPrincipalCredentialWithDBTX: nil,
	})

	allowAll := []Grant{NewGrant(ScopeProjectRead, WildcardResource)}
	earlier := principalPolicyTestContext(t, allowAll, allowAll, allowAll)
	require.NoError(t, engine.Require(earlier, check), "the earlier policy must allow the check, or the denial below proves nothing")

	admitted, err := engine.AdmitWorkloadSession(earlier)
	require.NoError(t, err)

	err = engine.Require(admitted, check)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestWorkloadDelegationIntersectsRatherThanReplacesPolicies(t *testing.T) {
	t.Parallel()
	allow := []Grant{NewGrant(ScopeProjectRead, "project-one")}
	for _, tc := range []struct {
		name                  string
		authorizer            string
		ceiling, agent, human []Grant
		allowed               bool
	}{
		{"autonomous", "", allow, allow, nil, true},
		{"delegated", "human", allow, allow, allow, true},
		{"human denies", "human", allow, allow, nil, false},
		{"agent denies", "human", allow, nil, allow, false},
		{"ceiling denies", "human", nil, allow, allow, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{
				DevMode: false, AdmitPrincipalCredential: nil, AdmitPrincipalCredentialWithDBTX: nil,
				AdmitWorkloadSession: func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error) {
					return WorkloadSessionAdmission{AgentPrincipal: "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001", OwnerUserID: "owner", Ceiling: tc.ceiling, Agent: tc.agent, AuthorizerUserID: tc.authorizer, Authorizer: tc.human}, nil
				},
			})
			ctx, err := engine.AdmitWorkloadSession(principalPolicyTestContext(t, allow, allow, allow))
			require.NoError(t, err)
			err = engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project-one", ResourceKind: "", Dimensions: nil})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
