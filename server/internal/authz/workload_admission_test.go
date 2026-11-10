package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
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
				Owner:          []Grant{NewGrant(ScopeProjectRead, "project-one")},
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

func TestWorkloadAdmissionRequiresEveryPolicy(t *testing.T) {
	t.Parallel()
	for _, empty := range []string{"ceiling", "agent", "owner"} {
		t.Run(empty, func(t *testing.T) {
			t.Parallel()
			check := Check{Scope: ScopeProjectRead, ResourceID: "project-one"}
			allow := []Grant{NewGrant(check.Scope, check.ResourceID)}
			admission := WorkloadSessionAdmission{
				AgentPrincipal: "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001", OwnerUserID: "user_123",
				Ceiling: allow, Agent: allow, Owner: allow,
			}
			switch empty {
			case "ceiling":
				admission.Ceiling = nil
			case "agent":
				admission.Agent = nil
			case "owner":
				admission.Owner = nil
			}
			engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{
				AdmitWorkloadSession: func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error) { return admission, nil },
			})
			ctx := principalPolicyTestContext(t, allow, allow, allow)
			admitted, err := engine.AdmitWorkloadSession(ctx)
			require.NoError(t, err)
			var denied *oops.ShareableError
			require.ErrorAs(t, engine.Require(admitted, check), &denied)
			require.Equal(t, oops.CodeForbidden, denied.Code)
		})
	}
}

func TestWorkloadAdmissionRetainsOnlyValidAgentPrincipal(t *testing.T) {
	t.Parallel()
	for _, principal := range []string{"agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001", "user:user_test", "agent:invalid", "agent:00000000-0000-0000-0000-000000000000", ""} {
		t.Run(principal, func(t *testing.T) {
			t.Parallel()
			engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{
				AdmitWorkloadSession: func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error) {
					return WorkloadSessionAdmission{AgentPrincipal: principal, OwnerUserID: "user_test"}, nil
				},
			})
			ctx, err := engine.AdmitWorkloadSession(t.Context())
			if principal == "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001" {
				require.NoError(t, err)
				id, ok := AdmittedWorkloadAgent(ctx)
				require.True(t, ok)
				require.Equal(t, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001", id.String())
			} else {
				require.Error(t, err)
				_, ok := AdmittedWorkloadAgent(ctx)
				require.False(t, ok)
			}
		})
	}
}

// Embedding the interface gives the test a distinct transaction without a pool.
type workloadAdmissionTestTx struct{ pgx.Tx }

func TestWorkloadAdmissionInTxRequiresConfiguredAdmitter(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), nil, EngineOpts{
		AdmitWorkloadSession: func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error) {
			t.Fatal("transaction admission must not acquire a second pool connection")
			return WorkloadSessionAdmission{}, nil
		},
	})
	ctx := t.Context()
	got, err := engine.AdmitWorkloadSessionInTx(ctx, &workloadAdmissionTestTx{})
	require.Error(t, err)
	require.Equal(t, ctx, got)
}

func TestWorkloadAdmissionInTxUsesConfiguredAdmitter(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("admission refused")
	for _, outcome := range []string{"success", "error", "invalid"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			tx := &workloadAdmissionTestTx{}
			ctx := principalPolicyTestContext(t, nil, nil, nil)
			check := Check{Scope: ScopeProjectRead, ResourceID: "project-one"}
			allow := []Grant{NewGrant(check.Scope, check.ResourceID)}
			calls := 0
			engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), nil, EngineOpts{
				AdmitWorkloadSession: func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error) {
					t.Fatal("transaction admission must not acquire a second pool connection")
					return WorkloadSessionAdmission{}, nil
				},
				AdmitWorkloadSessionInTx: func(received context.Context, receivedTx pgx.Tx) (WorkloadSessionAdmission, error) {
					calls++
					require.Equal(t, ctx, received)
					require.Same(t, tx, receivedTx)
					if outcome == "error" {
						return WorkloadSessionAdmission{}, sentinel
					}
					if outcome == "invalid" {
						return WorkloadSessionAdmission{}, nil
					}
					return WorkloadSessionAdmission{AgentPrincipal: "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001", OwnerUserID: "user_test", Ceiling: allow, Agent: allow, Owner: allow}, nil
				},
			})
			got, err := engine.AdmitWorkloadSessionInTx(ctx, tx)
			require.Equal(t, 1, calls)
			if outcome == "success" {
				require.NoError(t, err)
				require.NoError(t, engine.Require(got, check))
				_, ok := AdmittedWorkloadAgent(got)
				require.True(t, ok)
			} else {
				require.Error(t, err)
				require.Equal(t, ctx, got)
				if outcome == "error" {
					require.ErrorIs(t, err, sentinel)
				}
			}
		})
	}
}
