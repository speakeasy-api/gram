package remotesessions_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

func grantProjectRead(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, userID string) {
	t.Helper()
	selector, err := authz.NewSelector(authz.ScopeProjectRead, projectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: organizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, userID), Scope: string(authz.ScopeProjectRead), Selectors: selector})
	require.NoError(t, err)
}

// workloadCaller authenticates a workload assigned to an agent auth.UserID
// owns, in auth's organization and project.
func workloadCaller(t *testing.T, ctx context.Context, conn *pgxpool.Pool, auth *contextvalues.AuthContext, slug string) (context.Context, urn.SessionSubject) {
	t.Helper()
	organizationID := auth.ActiveOrganizationID
	grantProjectRead(t, ctx, conn, organizationID, *auth.ProjectID, auth.UserID)
	agent, err := agentsrepo.New(conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: organizationID, OwnerUserID: auth.UserID, Name: slug})
	require.NoError(t, err)
	wq := workloadrepo.New(conn)
	issuer, err := wq.CreateWorkloadIssuer(ctx, workloadrepo.CreateWorkloadIssuerParams{OrganizationID: organizationID, Name: slug, Tags: []string{}, Issuer: "https://" + slug + ".example.com", JwksUri: "https://" + slug + ".example.com/jwks"})
	require.NoError(t, err)
	_, err = wq.UpsertWorkloadAgentAssignment(ctx, workloadrepo.UpsertWorkloadAgentAssignmentParams{OrganizationID: organizationID, WorkloadIssuerID: issuer.ID, Subject: "run", MatchKind: "exact", AgentID: agent.ID})
	require.NoError(t, err)
	policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, nil)
	require.NoError(t, err)
	encoded, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	profile := contextvalues.PrincipalCredential{DelegatedGrants: encoded, DelegatedGrantsVersion: int32(runtimepolicy.CurrentDelegatedPolicyVersion)}
	return contextvalues.WithPrincipalCredentialAuthorization(ctx, auth, urn.NewWorkloadPrincipal(issuer.ID, "run"), profile), urn.NewWorkloadSubject(issuer.ID, "run")
}

// principalCredentialCaller authenticates an agent principal credential that
// acts for auth.UserID.
func principalCredentialCaller(t *testing.T, ctx context.Context, conn *pgxpool.Pool, auth *contextvalues.AuthContext, slug string) (context.Context, urn.SessionSubject) {
	t.Helper()
	projectID, organizationID := *auth.ProjectID, auth.ActiveOrganizationID
	grantProjectRead(t, ctx, conn, organizationID, projectID, auth.UserID)
	agent, err := agentsrepo.New(conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: organizationID, OwnerUserID: auth.UserID, ProjectID: conv.ToNullUUID(projectID), Name: slug})
	require.NoError(t, err)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	signer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "https://platform.example.invalid", false)
	require.NoError(t, err)
	credentials := principalcredential.New(signer, conn)
	raw, _, err := credentials.Mint(principalcredential.Credential{OrganizationID: organizationID, ProjectID: projectID, Principal: urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), AuthorizerUserID: auth.UserID})
	require.NoError(t, err)
	authenticated, err := credentials.Authenticate(ctx, raw)
	require.NoError(t, err)
	return authenticated, urn.NewAgentSubject(agent.ID)
}

// orgRegistration seeds a registration shared by every project in the
// fixture's organization.
func (f authClientTokenFixture) orgRegistration(t *testing.T, ctx context.Context, slug string) (clientID, issuerID uuid.UUID) {
	t.Helper()
	issuerID = seedOrgLevelRemoteIssuer(t, ctx, f.ti.conn, f.organizationID, slug+"-rsi")
	return seedRemoteClientAtTier(t, ctx, f.ti.conn, uuid.NullUUID{}, conv.ToPGText(f.organizationID), issuerID, slug+"-client"), issuerID
}

func (f authClientTokenFixture) member(t *testing.T, ctx context.Context) urn.SessionSubject {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return urn.NewUserSubject(auth.UserID)
}

// otherOrganizationSignIn seeds a project with a sign-in in a new
// organization the fixture's user also belongs to.
func (f authClientTokenFixture) otherOrganizationSignIn(t *testing.T, ctx context.Context, slug string) (organizationID string, projectID, signIn uuid.UUID) {
	t.Helper()
	organizationID = createOrganization(t, ctx, f.ti.conn, slug+"-org")
	project, err := projectsrepo.New(f.ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: organizationID})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(f.ti.conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: organizationID, UserID: conv.ToPGText(f.member(t, ctx).ID)}))
	return organizationID, project.ID, createUserSessionIssuerInProject(t, ctx, f.ti.conn, project.ID, slug+"-usi")
}

func TestResolveAuthClientAccessTokens_SharesAnOrganizationConsentAcrossProjects(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-org-share")
	clientID, issuerID := f.orgRegistration(t, ctx, "ac-org-share-org")
	signInP2 := createUserSessionIssuerInProject(t, ctx, f.ti.conn, createProject(t, ctx, f.ti.conn, "ac-org-share-p2"), "ac-org-share-p2-usi")

	fromP2 := urn.NewUserSubject("ac-org-share-p2-user")
	session := f.consent(t, fromP2, signInP2, clientID, "p2-token")
	tokens, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(fromP2, clientID))
	require.NoError(t, err)
	require.Equal(t, "p2-token", tokens[issuerID].Token, "an organization registration's consent serves every project of the organization")
	require.Equal(t, session.ID, tokens[issuerID].RemoteSessionID)
	reconnect, err := f.mgr.AuthClientNeedsReconnect(ctx, f.request(fromP2, clientID))
	require.NoError(t, err)
	require.False(t, reconnect)

	// A re-consent keeps the row's original sign-in and still serves.
	f.consent(t, fromP2, f.signInA, clientID, "p1-token")
	tokens, err = f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(fromP2, clientID))
	require.NoError(t, err)
	require.Equal(t, "p1-token", tokens[issuerID].Token)

	fromOrg := urn.NewUserSubject("ac-org-share-org-user")
	f.consent(t, fromOrg, seedOrganizationTierUserSessionIssuer(t, ctx, f.ti.conn, "ac-org-share-org-usi"), clientID, "org-token")
	tokens, err = f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(fromOrg, clientID))
	require.NoError(t, err)
	require.Equal(t, "org-token", tokens[issuerID].Token, "an organization sign-in serves too")

	otherOrg, otherProject, otherSignIn := f.otherOrganizationSignIn(t, ctx, "ac-org-share-b")
	fromOtherOrg := urn.NewUserSubject("ac-org-share-b-user")
	f.consent(t, fromOtherOrg, otherSignIn, clientID, "foreign-token")
	_, err = f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(fromOtherOrg, clientID))
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "a consent from another organization never serves")
	reconnect, err = f.mgr.AuthClientNeedsReconnect(ctx, f.request(fromOtherOrg, clientID))
	require.NoError(t, err)
	require.True(t, reconnect)

	_, err = f.mgr.ResolveAuthClientAccessTokens(ctx, remotesessions.AuthClientTokenRequest{ProjectID: otherProject, OrganizationID: otherOrg, UserSessionIssuerID: otherSignIn, Subject: fromP2, RegistrationID: clientID, SkipUnusable: false})
	require.ErrorIs(t, err, remotesessions.ErrChosenClientUnbound, "another organization cannot reach the registration")
}

func TestResolveAuthClientAccessTokens_WorkloadServesItsOwnersConsent(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-workload")
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	workloadCtx, caller := workloadCaller(t, ctx, f.ti.conn, auth, "ac-workload")
	owner := f.member(t, ctx)

	session := f.consent(t, owner, f.signInA, f.clientID, "p1-token")
	tokens, err := f.mgr.ResolveAuthClientAccessTokens(workloadCtx, f.request(caller, f.clientID))
	require.NoError(t, err)
	require.Equal(t, "p1-token", tokens[f.issuerID].Token, "another sign-in of the same project serves")
	require.Equal(t, session.ID, tokens[f.issuerID].RemoteSessionID)
	reconnect, err := f.mgr.AuthClientNeedsReconnect(workloadCtx, f.request(caller, f.clientID))
	require.NoError(t, err)
	require.False(t, reconnect)

	clientID, issuerID := f.orgRegistration(t, ctx, "ac-workload-org")
	f.consent(t, owner, createUserSessionIssuerInProject(t, ctx, f.ti.conn, createProject(t, ctx, f.ti.conn, "ac-workload-p2"), "ac-workload-p2-usi"), clientID, "p2-token")
	tokens, err = f.mgr.ResolveAuthClientAccessTokens(workloadCtx, f.request(caller, clientID))
	require.NoError(t, err)
	require.Equal(t, "p2-token", tokens[issuerID].Token, "so does an organization registration's consent from another project")

	_, err = f.mgr.ResolveClientAccessTokens(workloadCtx, f.projectID, f.organizationID, f.signInB, caller, f.clientID, false)
	require.ErrorIs(t, err, remotesessions.ErrChosenClientUnbound, "the legacy path still requires the sign-in link")
}

func TestResolveAuthClientAccessTokens_PrincipalCredentialServesTheAuthorizersConsent(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-principal")
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	credentialCtx, caller := principalCredentialCaller(t, ctx, f.ti.conn, auth, "ac-principal")
	authorizer := f.member(t, ctx)

	session := f.consent(t, authorizer, f.signInA, f.clientID, "authorizer-token")
	tokens, err := f.mgr.ResolveAuthClientAccessTokens(credentialCtx, f.request(caller, f.clientID))
	require.NoError(t, err)
	require.Equal(t, "authorizer-token", tokens[f.issuerID].Token, "a consent through another sign-in of the project serves")
	require.Equal(t, session.ID, tokens[f.issuerID].RemoteSessionID)
	reconnect, err := f.mgr.AuthClientNeedsReconnect(credentialCtx, f.request(caller, f.clientID))
	require.NoError(t, err)
	require.False(t, reconnect)

	_, _, otherSignIn := f.otherOrganizationSignIn(t, ctx, "ac-principal-b")
	clientID, _ := f.orgRegistration(t, ctx, "ac-principal-org")
	f.consent(t, authorizer, otherSignIn, clientID, "foreign-token")
	_, err = f.mgr.ResolveAuthClientAccessTokens(credentialCtx, f.request(caller, clientID))
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "a consent from another organization never serves")
	reconnect, err = f.mgr.AuthClientNeedsReconnect(credentialCtx, f.request(caller, clientID))
	require.NoError(t, err)
	require.True(t, reconnect)
}

func TestResolveAuthClientAccessTokens_GlobalRegistrationStaysInItsOrganization(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-global")
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedGlobalRemoteIssuer(t, ctx, f.ti.conn, "ac-global-rsi")
	clientID := seedRemoteClientAtTier(t, ctx, f.ti.conn, uuid.NullUUID{}, pgtype.Text{}, issuerID, "ac-global-client")
	member := f.member(t, ctx)

	otherOrg := createOrganization(t, ctx, f.ti.conn, "ac-global-other-org")
	otherProject, err := projectsrepo.New(f.ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "ac-global-b", Slug: "ac-global-b", OrganizationID: otherOrg})
	require.NoError(t, err)
	otherSignIn := createUserSessionIssuerInProject(t, ctx, f.ti.conn, otherProject.ID, "ac-global-b-usi")
	require.NoError(t, testrepo.New(f.ti.conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: otherOrg, UserID: conv.ToPGText(auth.UserID)}))
	inOtherOrg := func(subject urn.SessionSubject) remotesessions.AuthClientTokenRequest {
		return remotesessions.AuthClientTokenRequest{ProjectID: otherProject.ID, OrganizationID: otherOrg, UserSessionIssuerID: otherSignIn, Subject: subject, RegistrationID: clientID, SkipUnusable: false}
	}
	otherClientID := seedRemoteClientAtTier(t, ctx, f.ti.conn, uuid.NullUUID{}, pgtype.Text{}, issuerID, "ac-global-client-b")
	otherAuth := *auth
	otherAuth.ActiveOrganizationID = otherOrg
	otherAuth.ProjectID = &otherProject.ID
	workloadCtx, caller := workloadCaller(t, ctx, f.ti.conn, &otherAuth, "ac-global-workload")

	f.consent(t, member, f.signInA, clientID, "org-a-token")
	tokens, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(member, clientID))
	require.NoError(t, err)
	require.Equal(t, "org-a-token", tokens[issuerID].Token)

	_, err = f.mgr.ResolveAuthClientAccessTokens(ctx, inOtherOrg(member))
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "the user path never crosses organizations")
	_, err = f.mgr.ResolveAuthClientAccessTokens(workloadCtx, inOtherOrg(caller))
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "nor does the workload path")
	reconnect, err := f.mgr.AuthClientNeedsReconnect(workloadCtx, inOtherOrg(caller))
	require.NoError(t, err)
	require.True(t, reconnect)

	f.consent(t, member, otherSignIn, otherClientID, "org-b-token")
	req := inOtherOrg(caller)
	req.RegistrationID = otherClientID
	tokens, err = f.mgr.ResolveAuthClientAccessTokens(workloadCtx, req)
	require.NoError(t, err)
	require.Equal(t, "org-b-token", tokens[issuerID].Token, "a consent made in the organization serves its workload")
}

// The tenant lookup behind workloads and principal credentials requires a
// live organization, user and membership.
func TestGetTenantUserRemoteSessionForClient_RequiresALiveMember(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		revoke func(ctx context.Context, q *testrepo.Queries, organizationID, userID string) error
	}{
		{"disabled organization", func(ctx context.Context, q *testrepo.Queries, organizationID, _ string) error {
			return q.DisableDelegationOrganizationFixture(ctx, organizationID)
		}},
		{"deleted user", func(ctx context.Context, q *testrepo.Queries, _, userID string) error {
			return q.SetUserLifecycleFixture(ctx, testrepo.SetUserLifecycleFixtureParams{DeletedAt: conv.ToPGTimestamptz(time.Now()), WorkosDeletedAt: pgtype.Timestamptz{}, LastLogin: pgtype.Timestamptz{}, ID: userID})
		}},
		{"user deleted in WorkOS", func(ctx context.Context, q *testrepo.Queries, _, userID string) error {
			return q.SetUserLifecycleFixture(ctx, testrepo.SetUserLifecycleFixtureParams{DeletedAt: pgtype.Timestamptz{}, WorkosDeletedAt: conv.ToPGTimestamptz(time.Now()), LastLogin: pgtype.Timestamptz{}, ID: userID})
		}},
		{"removed membership", func(ctx context.Context, q *testrepo.Queries, organizationID, _ string) error {
			return q.ForceSoftDeleteOrganizationUserRelationshipsFixture(ctx, organizationID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newAuthClientTokenFixture(t, "ac-live")
			member := f.member(t, ctx)
			f.consent(t, member, f.signInA, f.clientID, "member-token")
			params := repo.GetTenantUserRemoteSessionForClientParams{ProjectID: f.projectID, OrganizationID: f.organizationID, SubjectUrn: member, RemoteSessionClientID: f.clientID}
			q := repo.New(f.ti.conn)
			_, err := q.GetTenantUserRemoteSessionForClient(ctx, params)
			require.NoError(t, err)

			require.NoError(t, tc.revoke(ctx, testrepo.New(f.ti.conn), f.organizationID, member.ID))
			_, err = q.GetTenantUserRemoteSessionForClient(ctx, params)
			require.ErrorIs(t, err, pgx.ErrNoRows)
		})
	}
}

func TestResolveAuthClientAccessTokens_RefreshesAConsentFromAnotherSignIn(t *testing.T) {
	t.Parallel()
	var refreshes atomic.Int32
	ctx, env := newExpiredRefreshEnv(t, "ac-refresh", func(w http.ResponseWriter, _ *http.Request) {
		refreshes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed-access","refresh_token":"next-refresh","expires_in":3600}`))
	})
	otherSignIn := createUserSessionIssuer(t, ctx, env.db, "ac-refresh-other")
	request := func(subject urn.SessionSubject) remotesessions.AuthClientTokenRequest {
		return remotesessions.AuthClientTokenRequest{ProjectID: env.projectID, OrganizationID: env.organizationID, UserSessionIssuerID: otherSignIn, Subject: subject, RegistrationID: env.clientID, SkipUnusable: false}
	}

	reconnect, err := env.mgr.AuthClientNeedsReconnect(ctx, request(env.subject))
	require.NoError(t, err)
	require.False(t, reconnect, "an expired token with a refresh grant refreshes")

	tokens, err := env.mgr.ResolveAuthClientAccessTokens(ctx, request(env.subject))
	require.NoError(t, err)
	require.Equal(t, "refreshed-access", tokens[env.issuerID].Token)
	require.Equal(t, env.session.ID, tokens[env.issuerID].RemoteSessionID, "the refresh writes back to the consented row")
	require.EqualValues(t, 1, refreshes.Load())

	// A workload rechecks its owner's session after the refresh, through the
	// same sign-in-free lookup.
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	workloadCtx, caller := workloadCaller(t, ctx, env.db, auth, "ac-refresh-workload")
	accessEnc, err := env.enc.Encrypt([]byte("owner-expired"))
	require.NoError(t, err)
	refreshEnc, err := env.enc.Encrypt([]byte("owner-refresh"))
	require.NoError(t, err)
	expired, err := repo.New(env.db).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: urn.NewUserSubject(auth.UserID), UserSessionIssuerID: env.session.UserSessionIssuerID, RemoteSessionClientID: env.clientID, AccessTokenEncrypted: accessEnc, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)), RefreshTokenEncrypted: conv.ToPGText(refreshEnc), Scopes: []string{}})
	require.NoError(t, err)

	tokens, err = env.mgr.ResolveAuthClientAccessTokens(workloadCtx, request(caller))
	require.NoError(t, err)
	require.Equal(t, "refreshed-access", tokens[env.issuerID].Token)
	require.Equal(t, expired.ID, tokens[env.issuerID].RemoteSessionID)
	require.EqualValues(t, 2, refreshes.Load())
}

func TestAuthClientNeedsReconnect_ExpiredTokenWithoutARefreshGrant(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-expired")
	subject := urn.NewUserSubject("ac-expired-user")
	accessEnc, err := f.enc.Encrypt([]byte("expired-token"))
	require.NoError(t, err)
	refreshEnc, err := f.enc.Encrypt([]byte("refresh-token"))
	require.NoError(t, err)
	upsert := func(refresh pgtype.Text) {
		_, err := repo.New(f.ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: subject, UserSessionIssuerID: f.signInA, RemoteSessionClientID: f.clientID, AccessTokenEncrypted: accessEnc, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)), RefreshTokenEncrypted: refresh, Scopes: []string{}})
		require.NoError(t, err)
	}

	upsert(pgtype.Text{})
	reconnect, err := f.mgr.AuthClientNeedsReconnect(ctx, f.request(subject, f.clientID))
	require.NoError(t, err)
	require.True(t, reconnect)

	upsert(conv.ToPGText(refreshEnc))
	reconnect, err = f.mgr.AuthClientNeedsReconnect(ctx, f.request(subject, f.clientID))
	require.NoError(t, err)
	require.False(t, reconnect)
}

func TestResolveAuthClientAccessTokens_ServesAServiceAccountWithoutTheSignInLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	source := &fakeClientCredentials{answers: []clientCredentialAnswer{{token: "self-token"}}}
	mgr := newClientCredentialManager(t, ti.conn, testenv.NewEncryptionClient(t), source)
	fx := seedSelfClient(t, ctx, ti.conn, "ac-self")
	otherSignIn := createUserSessionIssuer(t, ctx, ti.conn, "ac-self-other")
	subject := urn.NewUserSubject("ac-self-user")

	tokens, err := mgr.ResolveAuthClientAccessTokens(ctx, remotesessions.AuthClientTokenRequest{ProjectID: fx.projectID, OrganizationID: fx.organizationID, UserSessionIssuerID: otherSignIn, Subject: subject, RegistrationID: fx.clientID, SkipUnusable: false})
	require.NoError(t, err)
	require.Equal(t, "self-token", tokens[fx.remoteIssuerID].Token)
	require.Equal(t, remotesessions.CredentialOwnerSelf, tokens[fx.remoteIssuerID].CredentialOwner)

	_, err = mgr.ResolveClientAccessTokens(ctx, fx.projectID, fx.organizationID, otherSignIn, subject, fx.clientID, false)
	require.ErrorIs(t, err, remotesessions.ErrChosenClientUnbound)
}
