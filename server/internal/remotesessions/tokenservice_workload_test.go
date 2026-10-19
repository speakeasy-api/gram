package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
	"github.com/stretchr/testify/require"
)

func TestResolveAuthorization_WorkloadOwner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, organizationID := *auth.ProjectID, auth.ActiveOrganizationID
	selector, err := authz.NewSelector(authz.ScopeProjectRead, projectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: organizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID), Scope: string(authz.ScopeProjectRead), Selectors: selector})
	require.NoError(t, err)
	enc := testenv.NewEncryptionClient(t)
	manager := newResolveManager(t, ti.conn, enc)
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "workload-owner")
	clientID, remoteIssuer := seedActiveClient(t, ctx, ti.conn, projectID, userIssuer, organizationID, "workload-owner-upstream")
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: organizationID, OwnerUserID: auth.UserID, Name: "workload-owner"})
	require.NoError(t, err)
	wq := workloadrepo.New(ti.conn)
	issuer, err := wq.CreateWorkloadIssuer(ctx, workloadrepo.CreateWorkloadIssuerParams{OrganizationID: organizationID, Name: "workload-owner", Tags: []string{}, Issuer: "https://workload.example.com", JwksUri: "https://workload.example.com/jwks"})
	require.NoError(t, err)
	actor := urn.NewWorkloadPrincipal(issuer.ID, "scheduled-run")
	caller := urn.NewWorkloadSubject(issuer.ID, "scheduled-run")
	policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, nil)
	require.NoError(t, err)
	encoded, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	profile := contextvalues.PrincipalCredential{DelegatedGrants: encoded, DelegatedGrantsVersion: int32(runtimepolicy.CurrentDelegatedPolicyVersion)}
	workloadCtx := contextvalues.WithPrincipalCredentialAuthorization(ctx, auth, actor, profile)
	resolve := func(c context.Context, subject urn.SessionSubject) (remotesessions.ResolvedAuthorization, error) {
		return manager.ResolveAuthorization(c, projectID, organizationID, userIssuer, remoteIssuer, subject, "")
	}
	// No assignment must never fall back to a workload's own or an arbitrary user's session.
	_, err = resolve(workloadCtx, caller)
	require.Error(t, err)
	_, err = wq.UpsertWorkloadAgentAssignment(ctx, workloadrepo.UpsertWorkloadAgentAssignmentParams{OrganizationID: organizationID, WorkloadIssuerID: issuer.ID, Subject: "scheduled-run", MatchKind: "exact", AgentID: agent.ID})
	require.NoError(t, err)
	_, err = resolve(workloadCtx, caller)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "owner has no session")
	cipher, err := enc.Encrypt([]byte("owner-access-token"))
	require.NoError(t, err)
	source, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: urn.NewUserSubject(auth.UserID), UserSessionIssuerID: userIssuer, RemoteSessionClientID: clientID, AccessTokenEncrypted: cipher, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{}})
	require.NoError(t, err)
	resolved, err := resolve(workloadCtx, caller)
	require.NoError(t, err)
	require.Equal(t, source.ID, resolved.RemoteSessionID)
	reconnect, err := manager.RemoteSessionsNeedReconnect(workloadCtx, projectID, organizationID, userIssuer, caller)
	require.NoError(t, err)
	require.False(t, reconnect)
	unscoped, err := manager.ResolveAccessToken(workloadCtx, clientID, urn.NewUserSubject(auth.UserID), "")
	require.NoError(t, err)
	require.Empty(t, unscoped, "workloads cannot impersonate an owner through unscoped resolution")
	currentActor, ok := contextvalues.AuthenticatedActor(workloadCtx)
	require.True(t, ok)
	require.Equal(t, actor, currentActor)
	credential, ok := contextvalues.PrincipalCredentialAuthorization(workloadCtx)
	require.True(t, ok)
	require.Empty(t, credential.AuthorizerUserID)
	for _, subject := range []urn.SessionSubject{urn.NewWorkloadSubject(issuer.ID, "other-run"), urn.NewUserSubject(auth.UserID), urn.NewAgentSubject(agent.ID)} {
		_, err := resolve(workloadCtx, subject)
		require.ErrorIs(t, err, remotesessions.ErrInvalidAuthorizationRequest)
	}
	_, err = resolve(ctx, caller)
	require.ErrorIs(t, err, remotesessions.ErrInvalidAuthorizationRequest, "a subject is not workload authentication")
	for _, mutate := range []func(*contextvalues.AuthContext){
		func(a *contextvalues.AuthContext) { a.ActiveOrganizationID = "another-organization" },
		func(a *contextvalues.AuthContext) { a.ProjectID = new(uuid.New()) },
	} {
		mismatched := *auth
		mutate(&mismatched)
		mismatchCtx := contextvalues.WithPrincipalCredentialAuthorization(ctx, &mismatched, actor, profile)
		_, err = resolve(mismatchCtx, caller)
		require.ErrorIs(t, err, remotesessions.ErrInvalidAuthorizationRequest)
	}
	_, err = wq.SoftDeleteWorkloadAgentAssignmentForSubject(ctx, workloadrepo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{OrganizationID: organizationID, WorkloadIssuerID: issuer.ID, Subject: "scheduled-run", MatchKind: "exact"})
	require.NoError(t, err)
	_, err = resolve(workloadCtx, caller)
	require.Error(t, err, "live assignment revocation invalidates an existing context")
	_, err = wq.UpsertWorkloadAgentAssignment(ctx, workloadrepo.UpsertWorkloadAgentAssignmentParams{OrganizationID: organizationID, WorkloadIssuerID: issuer.ID, Subject: "scheduled-run", MatchKind: "exact", AgentID: agent.ID})
	require.NoError(t, err)
	_, err = agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: organizationID, ID: agent.ID})
	require.NoError(t, err)
	_, err = resolve(workloadCtx, caller)
	require.Error(t, err, "a suspended assigned agent must not release its owner's token")
}
