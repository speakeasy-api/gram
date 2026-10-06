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
				AgentPrincipal: "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001",
				OwnerUserID:    "user_123",
				Ceiling:        []Grant{NewGrant(ScopeProjectRead, "project-one")},
				Agent:          nil,
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
