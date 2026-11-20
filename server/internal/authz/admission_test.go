package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCredentialAdmissionHookFailsClosed(t *testing.T) {
	t.Parallel()
	admissionError := errors.New("admission failed")
	tests := map[string]PrincipalCredentialAdmitter{
		"unconfigured": nil,
		"missing owner": func(context.Context, *pgxpool.Pool) (PrincipalCredentialAdmission, error) {
			return PrincipalCredentialAdmission{}, nil
		},
		"failed admission": func(context.Context, *pgxpool.Pool) (PrincipalCredentialAdmission, error) {
			return PrincipalCredentialAdmission{}, admissionError
		},
	}
	for name, admit := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), EngineOpts{AdmitPrincipalCredential: admit})
			_, err := engine.AdmitPrincipalCredential(t.Context())
			if name == "failed admission" {
				require.ErrorIs(t, err, admissionError)
				return
			}
			var denied *oops.ShareableError
			require.ErrorAs(t, err, &denied)
			require.Equal(t, oops.CodeUnauthorized, denied.Code)
		})
	}
}

func TestCredentialAdmissionHookReloadsAndConjoinsPolicies(t *testing.T) {
	t.Parallel()
	calls := 0
	grants := []Grant{NewGrant(ScopeProjectRead, "project-one")}
	admit := func(context.Context, *pgxpool.Pool) (PrincipalCredentialAdmission, error) {
		calls++
		agent := grants
		if calls > 1 {
			agent = nil
		}
		return PrincipalCredentialAdmission{OwnerUserID: "current-owner", Credential: grants, Agent: agent, Owner: grants, Authorizer: RepresentedUserPolicy{UserID: "current-owner", Policy: grants}}, nil
	}
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), EngineOpts{AdmitPrincipalCredential: admit})
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-test"}, urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001"), contextvalues.PrincipalCredential{})
	prepared, err := engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.NoError(t, engine.Require(prepared, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	_, owner, ok := contextvalues.PrincipalCredentialProvenance(prepared)
	require.True(t, ok)
	require.Equal(t, "current-owner", owner)
	prepared, err = engine.PrepareContext(prepared)
	require.NoError(t, err)
	require.Equal(t, 2, calls, "even a prepared context must repeat admission")
	require.Error(t, engine.Require(prepared, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}), "empty live agent policy must not inherit credential or owner authority")
}

func TestCredentialDBTXAdmissionHookFailsClosed(t *testing.T) {
	t.Parallel()
	admissionError := errors.New("admission failed")
	tests := map[string]PrincipalCredentialDBTXAdmitter{
		"unconfigured": nil,
		"missing owner": func(context.Context, accessrepo.DBTX) (PrincipalCredentialAdmission, error) {
			return PrincipalCredentialAdmission{}, nil
		},
		"failed admission": func(context.Context, accessrepo.DBTX) (PrincipalCredentialAdmission, error) {
			return PrincipalCredentialAdmission{}, admissionError
		},
	}
	for name, admit := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), EngineOpts{AdmitPrincipalCredentialWithDBTX: admit})
			_, err := engine.AdmitPrincipalCredentialWithDBTX(t.Context(), nil)
			if name == "failed admission" {
				require.ErrorIs(t, err, admissionError)
				return
			}
			var denied *oops.ShareableError
			require.ErrorAs(t, err, &denied)
			require.Equal(t, oops.CodeUnauthorized, denied.Code)
		})
	}
}

func TestCredentialDBTXAdmissionHookReloadsAndConjoinsPolicies(t *testing.T) {
	t.Parallel()
	calls := 0
	grants := []Grant{NewGrant(ScopeProjectRead, "project-one")}
	admit := func(context.Context, accessrepo.DBTX) (PrincipalCredentialAdmission, error) {
		calls++
		agent := grants
		if calls > 1 {
			agent = nil
		}
		return PrincipalCredentialAdmission{OwnerUserID: "current-owner", Credential: grants, Agent: agent, Owner: grants, Authorizer: RepresentedUserPolicy{UserID: "current-owner", Policy: grants}}, nil
	}
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), EngineOpts{AdmitPrincipalCredentialWithDBTX: admit})
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-test"}, urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001"), contextvalues.PrincipalCredential{})
	prepared, err := engine.AdmitPrincipalCredentialWithDBTX(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, engine.Require(prepared, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	_, owner, ok := contextvalues.PrincipalCredentialProvenance(prepared)
	require.True(t, ok)
	require.Equal(t, "current-owner", owner)
	prepared, err = engine.AdmitPrincipalCredentialWithDBTX(prepared, nil)
	require.NoError(t, err)
	require.Equal(t, 2, calls, "even a prepared context must repeat admission")
	require.Error(t, engine.Require(prepared, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}), "empty live agent policy must not inherit credential or owner authority")
}

// Workload admission must replace whatever policy an earlier admission left on
// the context, or a workload whose agent holds nothing would act under the
// earlier principal's grants.
func TestWorkloadAdmissionReplacesEarlierAdmittedPolicies(t *testing.T) {
	t.Parallel()
	check := Check{Scope: ScopeProjectRead, ResourceID: "project-one"}
	ceiling := []Grant{NewGrant(ScopeProjectRead, "project-one")}
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{
		DevMode: false,
		AdmitWorkloadSession: func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error) {
			return WorkloadSessionAdmission{
				AgentPrincipal: "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001",
				OwnerUserID:    "user_123",
				Ceiling:        ceiling,
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
	gotCeiling, ok := CredentialCeilingFromContext(admitted)
	require.True(t, ok)
	require.Equal(t, ceiling, gotCeiling)

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
				owner, ok := AdmittedWorkloadOwner(ctx)
				require.True(t, ok)
				require.Equal(t, "user_test", owner)
			} else {
				require.Error(t, err)
				_, ok := AdmittedWorkloadAgent(ctx)
				require.False(t, ok)
				_, ok = AdmittedWorkloadOwner(ctx)
				require.False(t, ok)
			}
		})
	}
}

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

func TestMandatoryPublicServerAdmission(t *testing.T) {
	t.Parallel()
	anonymous := MandatoryAuthorization{caller: MandatoryCallerAnonymous, publicAnonymous: true}.decide(MandatoryPolicySourceNone, MandatoryDenyReasonUnauthenticated)
	connect := MCPServer(MCPServerIDFrom("server_1"), ProjectIDFrom("project_1")).Connect()
	for _, visibility := range []MandatoryServerVisibility{MandatoryServerPublic, MandatoryServerPrivate, "unknown", ""} {
		decision, err := anonymous.DecideServerAdmission("org_1", visibility, connect)
		require.NoError(t, err)
		require.Equal(t, visibility == MandatoryServerPublic, decision.Allowed)
	}
	// Raw authorization stays strict; only the explicit server boundary admits public access.
	decision, err := anonymous.Decide("org_1", connect)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	_, err = anonymous.DecideServerAdmission("org_1", MandatoryServerPublic, mcpCheck(ScopeMCPAuth, "client_1", "project_1"))
	require.Error(t, err)
	_, err = anonymous.DecideServerAdmission("org_1", MandatoryServerPublic, mcpCheck(ScopeMCPConnect, "", "project_1"))
	require.Error(t, err)
}

func TestMandatoryPublicAdmissionDoesNotBypassCredentialPolicy(t *testing.T) {
	t.Parallel()
	connect := MCPServer(MCPServerIDFrom("server_1"), ProjectIDFrom("project_1")).Connect()
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
		decision, err := authorization.DecideServerAdmission("org_1", MandatoryServerPublic, MCPServer(MCPServerIDFrom("server_1"), ProjectIDFrom("project_1")).Connect())
		require.NoError(t, err)
		require.False(t, decision.Allowed)
	}
	authorization, err := (&Engine{}).PrepareMandatory(t.Context())
	require.NoError(t, err)
	decision, err := authorization.DecideServerAdmission("org_1", MandatoryServerPublic, MCPServer(MCPServerIDFrom("server_1"), ProjectIDFrom("project_1")).Connect())
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
	_, err := anonymous.DecideAuthClientAdmission("org_1", MandatoryServerPublic, MCPServer(MCPServerIDFrom("server_1"), ProjectIDFrom("project_1")).Connect())
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

func TestRefreshContextObservesRevocationWithoutChangingOriginalRequest(t *testing.T) {
	t.Parallel()
	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedOrganization(t, ctx, conn, auth.ActiveOrganizationID)
	seedConnectedUser(t, ctx, conn, auth.ActiveOrganizationID, auth.UserID, "fixture@example.com", "Fixture User", "fixture-workos-user", "fixture-membership")
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID)
	seedGrant(t, ctx, conn, auth.ActiveOrganizationID, principal, ScopeProjectRead, WildcardResource)
	admitted, err := engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.NoError(t, engine.Require(admitted, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	_, err = accessrepo.New(conn).DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{OrganizationID: auth.ActiveOrganizationID, PrincipalUrn: principal})
	require.NoError(t, err)
	refreshed, err := engine.RefreshContext(admitted)
	require.NoError(t, err)
	requireForbidden(t, engine.Require(refreshed, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	require.NoError(t, engine.Require(admitted, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
}

func TestAdmissionBoundarySurvivesGrantReplacement(t *testing.T) {
	t.Parallel()
	ctx := enterpriseTestCtx(t.Context())
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	initial := GrantsToContext(ctx, []Grant{NewGrant(ScopeProjectRead, "initial")})
	current := GrantsToContext(ctx, []Grant{NewGrant(ScopeProjectRead, WildcardResource)})
	boundary, err := engine.CaptureAdmissionBoundary(initial)
	require.NoError(t, err)
	bounded := boundary.Apply(current)
	require.NoError(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "initial"}))
	requireForbidden(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "new"}))
	// Principal re-admission replaces live policy sets; the job boundary persists.
	bounded = admittedPoliciesToContext(bounded, RepresentedUserPolicy{UserID: "", Policy: nil}, false, []Grant{NewGrant(ScopeProjectRead, WildcardResource)})
	requireForbidden(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "new"}))
	bounded = admittedPoliciesToContext(bounded, RepresentedUserPolicy{UserID: "", Policy: nil}, false, []Grant{})
	requireForbidden(t, engine.Require(bounded, Check{Scope: ScopeProjectRead, ResourceID: "initial"}))
}

func TestAdmissionBoundaryRetainsRepresentedUserPolicy(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	markerGrant := NewGrantWithSelector(ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyConnectionOwner: SelectorValueRepresentedUser}))
	agentPolicy := []Grant{markerGrant}
	admitted := admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{}}, false, agentPolicy)
	boundary, err := engine.CaptureAdmissionBoundary(admitted)
	require.NoError(t, err)
	requireForbidden(t, engine.Require(admitted, personalClientCheck("user_u")))
	// The represented user is granted mcp:auth after admission; the running
	// job must not gain it.
	refreshed := admittedPoliciesToContext(boundary.Apply(admitted), RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{markerGrant}}, false, agentPolicy)
	requireForbidden(t, engine.Require(refreshed, personalClientCheck("user_u")))
	require.NoError(t, engine.Require(admittedPoliciesToContext(admitted, RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{markerGrant}}, false, agentPolicy), personalClientCheck("user_u")))
}

func TestAdmissionBoundaryRequiresPreparedAuthorization(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	_, err := engine.CaptureAdmissionBoundary(enterpriseTestCtx(t.Context()))
	require.ErrorContains(t, err, "prepared authorization")
}

// Embedding the interface gives the test a distinct transaction without a pool.
type workloadAdmissionTestTx struct{ pgx.Tx }
