package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func putExecutionTestGrant(t *testing.T, db *pgxpool.Pool, principal urn.Principal, g authz.Grant) {
	t.Helper()
	raw, err := json.Marshal(g.Selector)
	require.NoError(t, err)
	_, err = accessrepo.New(db).InsertPrincipalGrantIfAbsent(t.Context(), accessrepo.InsertPrincipalGrantIfAbsentParams{OrganizationID: "org-test", PrincipalUrn: principal, Scope: string(g.Scope), Selectors: raw})
	require.NoError(t, err)
}

func deleteExecutionTestScope(t *testing.T, db *pgxpool.Pool, principal urn.Principal, scope authz.Scope) {
	t.Helper()
	rows, err := accessrepo.New(db).ListPrincipalGrantsByOrg(t.Context(), accessrepo.ListPrincipalGrantsByOrgParams{OrganizationID: "org-test", PrincipalUrn: principal.String()})
	require.NoError(t, err)
	for _, row := range rows {
		if row.Scope == string(scope) {
			_, err = accessrepo.New(db).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{OrganizationID: "org-test", ID: row.ID})
			require.NoError(t, err)
		}
	}
}

// Revocation/restoration intentionally exercises sequential requests sharing one invocation policy.
//
//nolint:paralleltest,tparallel // Subtests mutate and restore the same agent grants.
func TestExecutionAuthorizationLivePolicyAndPlatformIsolation(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_authorization")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "execution-admission")
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Execution", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	thread := seedThreadWithEvent(t, db, assistant.ID, "execution-admission", "execution-admission", eventStatusPending)
	identity, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, assistant.ID, root)
	require.NoError(t, err)
	principal := urn.NewPrincipal(urn.PrincipalTypeAgent, identity.Identity.AgentID.String())
	q := identityrepo.New(db)
	remote, server := uuid.New(), uuid.New()
	require.NoError(t, q.FixtureCreateRemote(t.Context(), identityrepo.FixtureCreateRemoteParams{ID: remote, ProjectID: project}))
	require.NoError(t, q.FixtureCreateMCPServer(t.Context(), identityrepo.FixtureCreateMCPServerParams{ID: server, ProjectID: project, RemoteID: uuid.NullUUID{UUID: remote, Valid: true}}))
	require.NoError(t, q.FixtureAttachMCPServer(t.Context(), identityrepo.FixtureAttachMCPServerParams{AssistantID: assistant.ID, ServerID: server, ProjectID: project}))
	businessGrant := authz.NewGrant(authz.ScopeMCPConnect, server.String())
	putExecutionTestGrant(t, db, principal, businessGrant)
	putExecutionTestGrant(t, db, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"), businessGrant)
	engine := authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{AdmitWorkloadSession: runtimepolicy.AdmitWorkloadSession})
	manager := assistanttokens.New("test-secret", db, engine)
	signer := executionTestIssuer(t)
	manager.ConfigureExecutionIdentity(signer, testIdentityService)
	core.assistantTokens = manager
	putExecutionTestGrant(t, db, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), businessGrant)
	exerciseInvocationCredentials(t, db, core, manager, assistant, root, thread, server)
	var dispatched atomic.Pointer[string]
	core.runtime = testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnToken: &dispatched}
	for _, mode := range []string{sourceKindCron, sourceKindDashboard} {
		t.Run(mode, func(t *testing.T) {
			payload := []byte(`{}`)
			if mode == sourceKindDashboard {
				payload = []byte(`{"user_id":"user-2"}`)
			}
			raw, err := core.captureExecution(t.Context(), assistant, mode, thread, uuid.NullUUID{UUID: root, Valid: true}, "event-"+mode, payload)
			require.NoError(t, err)
			execution, err := decodeExecution(raw)
			require.NoError(t, err)
			_, err = core.processEventTurn(t.Context(), assistantThreadRecord{ID: thread, ProjectID: project, AssistantID: assistant.ID, SourceKind: mode}, assistant, assistantRuntimeRecord{}, assistantThreadEventRecord{ID: uuid.New(), EventID: "event-" + mode, NormalizedPayloadJSON: raw})
			require.NoError(t, err)
			require.NotNil(t, dispatched.Load())
			token := *dispatched.Load()
			require.True(t, assistanttokens.IsExecutionToken(token), "bound production turn must never mint an owner token")
			// Rollback stops bound issuance/use without deleting identities, inventing
			// an execute grant, or substituting the legacy user token path.
			stopped, gateErr := assistantidentity.New(testIdentityService.Issuer(), false, assistantidentity.Rollout{DisableExecution: true})
			require.NoError(t, gateErr)
			manager.ConfigureExecutionIdentity(signer, stopped)
			_, gateErr = manager.GenerateExecution(t.Context(), *execution)
			require.ErrorIs(t, gateErr, assistantidentity.ErrRolloutDisabled)
			_, _, gateErr = manager.AuthorizeRuntime(t.Context(), token)
			require.Error(t, gateErr)
			dispatched.Store(nil)
			_, gateErr = core.processEventTurn(t.Context(), assistantThreadRecord{ID: thread, ProjectID: project, AssistantID: assistant.ID, SourceKind: mode}, assistant, assistantRuntimeRecord{}, assistantThreadEventRecord{ID: uuid.New(), EventID: "event-" + mode, NormalizedPayloadJSON: raw})
			require.ErrorIs(t, gateErr, assistantidentity.ErrRolloutDisabled)
			require.Nil(t, dispatched.Load(), "disabled bound execution must not invoke the runtime with owner credentials")
			manager.ConfigureExecutionIdentity(signer, testIdentityService)

			ctx, claims, err := manager.AuthorizeRuntime(t.Context(), "Bearer "+token)
			require.NoError(t, err)
			require.Empty(t, claims.UserID)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.Empty(t, ac.UserID)
			actor, ok := contextvalues.AuthenticatedActor(ctx)
			require.True(t, ok)
			require.Equal(t, urn.PrincipalTypeWorkload, actor.Type)
			_, err = manager.Validate(token)
			require.Error(t, err, "execution audience is not legacy assistant audience")
			bctx, err := manager.AuthorizeBusiness(t.Context(), token, server, nil)
			require.NoError(t, err)
			require.NoError(t, assistanttokens.RevalidateBusinessExecution(bctx))
			_, err = manager.AuthorizeBusiness(bctx, token, server, nil)
			require.NoError(t, err, "nested MCP routing preserves independent business restrictions")
			require.NoError(t, engine.Require(bctx, authz.MCPToolCallCheck(server.String(), authz.MCPToolCallDimensions{Tool: "read", ProjectID: project.String()})))
			_, err = manager.AuthorizeBusiness(t.Context(), token, uuid.New(), nil)
			require.Error(t, err)
			_, err = manager.AuthorizeBusiness(t.Context(), token, uuid.Nil, nil)
			require.Error(t, err)
			denied := errors.New("human policy denied")
			_, err = manager.AuthorizeBusiness(t.Context(), token, server, func(context.Context, assistantidentity.Execution) ([]authz.Grant, error) { return nil, denied })
			require.ErrorIs(t, err, denied)
			_, err = manager.AuthorizeBusiness(t.Context(), token, server, func(context.Context, assistantidentity.Execution) ([]authz.Grant, error) { return nil, nil })
			require.Error(t, err, "empty restriction denies, not unrestricted")
			// Business denial does not mutate invocation identity or assistant-owned context.
			pctx, pclaims, err := manager.AuthorizePlatform(t.Context(), token)
			require.NoError(t, err)
			require.Empty(t, pclaims.UserID, "platform never installs creator or invoker authority")
			actor, ok = contextvalues.AuthenticatedActor(pctx)
			require.True(t, ok)
			require.Equal(t, urn.PrincipalTypeWorkload, actor.Type)
			ap, ok := contextvalues.GetAssistantPrincipal(pctx)
			require.True(t, ok)
			require.Equal(t, assistant.ID, ap.AssistantID)
			require.Equal(t, thread, ap.ThreadID)
			deleteExecutionTestScope(t, db, principal, authz.ScopeMCPConnect)
			_, err = manager.AuthorizeBusiness(t.Context(), token, server, nil)
			require.Error(t, err)
			_, _, err = manager.AuthorizePlatform(t.Context(), token)
			require.NoError(t, err)
			putExecutionTestGrant(t, db, principal, businessGrant)
			_, _, err = manager.AuthorizeRuntime(t.Context(), token)
			require.NoError(t, err, "running requires no additional capability")
			stale := *execution
			stale.Identity.AssistantGeneration++
			_, err = manager.GenerateExecution(t.Context(), stale)
			require.Error(t, err)
			expandedResource := uuid.New()
			putExecutionTestGrant(t, db, principal, authz.NewGrant(authz.ScopeMCPConnect, expandedResource.String()))
			_, err = manager.AuthorizeBusiness(t.Context(), token, expandedResource, nil)
			require.Error(t, err, "live policy expansion cannot widen captured business ceiling")
			if mode == sourceKindDashboard {
				deleteExecutionTestScope(t, db, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"), authz.ScopeMCPConnect)
				_, err = manager.AuthorizeBusiness(t.Context(), token, server, nil)
				require.Error(t, err, "live human revocation denies despite intact agent grants")
				require.Error(t, assistanttokens.RevalidateBusinessExecution(bctx), "refresh revalidation uses live human policy")
				putExecutionTestGrant(t, db, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"), businessGrant)
				deleteExecutionTestScope(t, db, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"), authz.ScopeProjectWrite)
				_, _, err = manager.AuthorizeRuntime(t.Context(), token)
				require.Error(t, err)
				_, _, err = manager.AuthorizePlatform(t.Context(), token)
				require.NoError(t, err, "invoker revocation cannot remove assistant-owned reply authority")
			}
			wrong := *execution
			wrong.Identity.OrganizationID = "org-other"
			_, err = manager.GenerateExecution(t.Context(), wrong)
			require.Error(t, err)
		})
	}
}

func TestExecutionExistingBindingRunsWithoutGrantOrUpgrade(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_upgrade")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "execution-upgrade")
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Upgrade", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	identity, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, assistant.ID, root)
	require.NoError(t, err)
	thread := seedThreadWithEvent(t, db, assistant.ID, "execution-upgrade", "execution-upgrade", eventStatusPending)
	raw, err := core.captureExecution(t.Context(), assistant, sourceKindCron, thread, uuid.NullUUID{UUID: root, Valid: true}, "old-event", []byte(`{}`))
	require.NoError(t, err)
	old, err := decodeExecution(raw)
	require.NoError(t, err)
	require.NoError(t, testIdentityService.AdmitModel(t.Context(), db, *old), "existing bindings need no new grant or upgrade")
	raw, err = core.captureExecution(t.Context(), assistant, sourceKindCron, thread, uuid.NullUUID{UUID: root, Valid: true}, "new-event", []byte(`{}`))
	require.NoError(t, err)
	fresh, err := decodeExecution(raw)
	require.NoError(t, err)
	require.NoError(t, testIdentityService.AdmitModel(t.Context(), db, *fresh))
	require.NoError(t, identityrepo.New(db).FixtureSuspendAgent(t.Context(), identityrepo.FixtureSuspendAgentParams{OrganizationID: "org-test", AgentID: identity.Identity.AgentID}))
	require.ErrorIs(t, testIdentityService.AdmitModel(t.Context(), db, *fresh), assistantidentity.ErrInvalidIdentity)
	// Suspension is captured as the same workload, not legacy identity. Token
	// minting rejects it without changing the assistant entity's active state.
	raw, err = core.captureExecution(t.Context(), assistant, sourceKindCron, thread, uuid.NullUUID{UUID: root, Valid: true}, "suspended-event", []byte(`{}`))
	require.NoError(t, err)
	suspended, err := decodeExecution(raw)
	require.NoError(t, err)
	require.NotNil(t, suspended)
	manager := assistanttokens.New("legacy-test-secret", db, nil)
	manager.ConfigureExecutionIdentity(executionTestIssuer(t), testIdentityService)
	token, err := manager.GenerateExecution(t.Context(), *suspended)
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
	require.Empty(t, token)
	unchanged, err := core.getAssistantForDispatch(t.Context(), assistant.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, unchanged.Status)

	_, err = agentrepo.New(db).ResumeAgent(t.Context(), agentrepo.ResumeAgentParams{OrganizationID: "org-test", ID: identity.Identity.AgentID})
	require.NoError(t, err)
	require.NoError(t, testIdentityService.AdmitModel(t.Context(), db, *fresh), "temporary suspension is not permanent invalidation")
	token, err = manager.GenerateExecution(t.Context(), *suspended)
	require.NoError(t, err)
	require.True(t, assistanttokens.IsExecutionToken(token))

	paused := StatusPaused
	_, err = core.UpdateAssistant(t.Context(), project, assistant.ID, nil, nil, nil, nil, nil, nil, nil, &paused)
	require.NoError(t, err)
	require.ErrorIs(t, testIdentityService.AdmitModel(t.Context(), db, *fresh), assistantidentity.ErrInvalidIdentity, "paused assistant cannot dispatch with stale active metadata")
}

func TestUnboundAssistantDispatchRetainsOriginalToken(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_legacy_token")
	require.NoError(t, err)
	project, id, _, _ := insertAssistantFixture(t, db)
	thread := seedThreadWithEvent(t, db, id, "legacy-token", "legacy-token", eventStatusPending)
	core := newProvisioningCore(t, db)
	assistant, err := core.getAssistantForDispatch(t.Context(), id)
	require.NoError(t, err)
	manager := assistanttokens.New("legacy-test-secret", db, nil)
	core.assistantTokens = manager // No workload signer needed for an unbound assistant.
	var dispatched atomic.Pointer[string]
	core.runtime = testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnToken: &dispatched}
	_, err = core.processEventTurn(t.Context(), assistantThreadRecord{ID: thread, ProjectID: project, AssistantID: id, SourceKind: sourceKindCron}, assistant, assistantRuntimeRecord{}, assistantThreadEventRecord{ID: uuid.New(), EventID: "legacy-token", NormalizedPayloadJSON: []byte(`{}`)})
	require.NoError(t, err)
	require.NotNil(t, dispatched.Load())
	require.False(t, assistanttokens.IsExecutionToken(*dispatched.Load()))
	claims, err := manager.Validate(*dispatched.Load())
	require.NoError(t, err)
	require.Equal(t, assistant.CreatedByUserID, claims.UserID)
	require.Equal(t, assistantRuntimeTokenTTL, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
}
