package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The tests below skip Preflight, which refuses the same plans with plain
// reads, so they cover the refusals Bind repeats under its locks.

func TestIdentityCommitBindRefusesDetachingOrgClientOnOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "identity-commit-detach-issuer")
	currentProviderID := createServerIdentityProvider(t, ctx, ti, "identity-commit-detach-current", "", false, []string{"none"})
	orgClientID := bindServerIdentityClient(t, ctx, ti, "identity-commit-detach-org-client", currentProviderID, userIssuerID, true)
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "identity-commit-detach-next", "", false, []string{"none"})
	nextClientID := createServerIdentityClient(t, ctx, ti, "identity-commit-detach-next-client", nextProviderID, false)

	commit, tx, reg := lockIdentityCommit(t, ctx, ti, linkPlan(t, ctx, userIssuerID, nextProviderID, nextClientID))
	require.ErrorIs(t, commit.Bind(ctx, tx, reg), remotesessions.ErrIdentityOrgWideBinding)
	require.NoError(t, tx.Rollback(ctx))
	requireClientBound(t, ctx, ti, orgClientID, userIssuerID, true)
	requireClientBound(t, ctx, ti, nextClientID, userIssuerID, false)
}

func TestIdentityCommitBindRefusesAttachingOrgClientToOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "identity-commit-attach-issuer")
	providerID := createServerIdentityProvider(t, ctx, ti, "identity-commit-attach-provider", "", false, []string{"none"})
	orgClientID := createServerIdentityClient(t, ctx, ti, "identity-commit-attach-client", providerID, true)

	commit, tx, reg := lockIdentityCommit(t, ctx, ti, linkPlan(t, ctx, userIssuerID, providerID, orgClientID))
	require.ErrorIs(t, commit.Bind(ctx, tx, reg), remotesessions.ErrIdentityOrgWideBinding)
}

func TestIdentityCommitBindRefusesDetachingClientWithLiveAgentBinding(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "identity-commit-agent-issuer")
	currentProviderID := createServerIdentityProvider(t, ctx, ti, "identity-commit-agent-current", "", false, []string{"none"})
	clientID := bindServerIdentityClient(t, ctx, ti, "identity-commit-agent-client", currentProviderID, userIssuerID, false)
	seedLiveAgentBinding(t, ctx, ti, clientID, userIssuerID)
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "identity-commit-agent-next", "", false, []string{"none"})
	nextClientID := createServerIdentityClient(t, ctx, ti, "identity-commit-agent-next-client", nextProviderID, false)

	commit, tx, reg := lockIdentityCommit(t, ctx, ti, linkPlan(t, ctx, userIssuerID, nextProviderID, nextClientID))
	require.ErrorIs(t, commit.Bind(ctx, tx, reg), remotesessions.ErrIdentityConflict)
	require.NoError(t, tx.Rollback(ctx))
	requireClientBound(t, ctx, ti, clientID, userIssuerID, true)
	requireLiveAgentBindings(t, ctx, ti, clientID, userIssuerID)
}

func TestIdentityCommitBindRequiresLock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "identity-commit-unlocked-issuer")
	providerID := createServerIdentityProvider(t, ctx, ti, "identity-commit-unlocked-provider", "", false, []string{"none"})
	clientID := createServerIdentityClient(t, ctx, ti, "identity-commit-unlocked-client", providerID, false)

	commit := identityCommitter(t, ti).Prepare(linkPlan(t, ctx, userIssuerID, providerID, clientID))
	reg, err := commit.Register(ctx)
	require.NoError(t, err)
	tx, err := commit.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { o11y.NoLogDefer(func() error { return tx.Rollback(context.Background()) }) })
	require.ErrorIs(t, commit.Bind(ctx, tx, reg), remotesessions.ErrIdentityStepOrder)
	requireClientBound(t, ctx, ti, clientID, userIssuerID, false)
}

// A bound client ReuseBound keeps means registration would only leave a new
// client behind upstream, so Register skips it.
func TestIdentityCommitReuseBoundSkipsRegistrationForBoundClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(registrationServer.Close)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "identity-commit-reuse-issuer")
	providerID := createServerIdentityProvider(t, ctx, ti, "identity-commit-reuse-provider", registrationServer.URL, false, []string{"client_secret_basic"})
	clientID := bindServerIdentityClient(t, ctx, ti, "identity-commit-reuse-client", providerID, userIssuerID, false)

	plan := linkPlan(t, ctx, userIssuerID, providerID, uuid.Nil)
	plan.Client = remotesessions.RegisterClient(remotesessions.RegistrationPolicy{Scope: nil, Audience: nil, TokenEndpointAuthMethod: nil, RequireClientSecret: true, AllowCIMD: false})
	plan.Bound = remotesessions.ReuseBound
	commit := identityCommitter(t, ti).Prepare(plan)
	require.NoError(t, commit.Preflight(ctx))
	reg, err := commit.Register(ctx)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RegistrationReused, reg.Method)
	require.Zero(t, requests.Load())

	tx, err := commit.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { o11y.NoLogDefer(func() error { return tx.Rollback(context.Background()) }) })
	require.NoError(t, commit.Lock(ctx, tx))
	require.NoError(t, commit.Bind(ctx, tx, reg))
	res, err := commit.Commit(ctx, tx)
	require.NoError(t, err)
	require.True(t, res.Reused)
	require.Equal(t, clientID, res.Client.ID)
}

// seedLiveAgentBinding attaches a new agent to the caller's remote session
// through clientID's binding to userIssuerID.
func seedLiveAgentBinding(t *testing.T, ctx context.Context, ti *testInstance, clientID, userIssuerID uuid.UUID) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agent, err := agentrepo.New(ti.conn).CreateAgent(ctx, agentrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		Name:           "Identity binding agent",
	})
	require.NoError(t, err)
	subject := urn.NewUserSubject(authCtx.UserID)
	session := insertRemoteSession(t, ctx, ti.conn, subject, userIssuerID.String(), clientID.String())
	_, err = testrepo.New(ti.conn).InsertPrincipalRemoteSessionBindingFixture(ctx, testrepo.InsertPrincipalRemoteSessionBindingFixtureParams{
		ProjectID:             *authCtx.ProjectID,
		OrganizationID:        authCtx.ActiveOrganizationID,
		PrincipalID:           agent.ID,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientID,
		RemoteSessionID:       session.ID,
		GrantGeneration:       session.GrantGeneration,
		AttachedBySubjectID:   subject.String(),
	})
	require.NoError(t, err)
}

func requireLiveAgentBindings(t *testing.T, ctx context.Context, ti *testInstance, clientID, userIssuerID uuid.UUID) {
	t.Helper()
	live, err := repo.New(ti.conn).HasLivePrincipalRemoteSessionBindingsForClientBinding(ctx, repo.HasLivePrincipalRemoteSessionBindingsForClientBindingParams{
		ProjectID:             projectIDFromContext(t, ctx),
		OrganizationID:        activeOrganizationID(t, ctx),
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userIssuerID,
	})
	require.NoError(t, err)
	require.True(t, live)
}

// identityCommitter builds a committer for plans that link stored clients,
// which need no encryption, registration or tunnels.
func identityCommitter(t *testing.T, ti *testInstance) *remotesessions.IdentityCommitter {
	t.Helper()
	return remotesessions.NewIdentityCommitter(testenv.NewLogger(t), ti.conn, nil, audit.NewLogger(), nil, nil, nil, nil)
}

// lockIdentityCommit prepares plan and runs it through Lock, skipping
// Preflight. The transaction is rolled back at cleanup.
func lockIdentityCommit(t *testing.T, ctx context.Context, ti *testInstance, plan remotesessions.IdentityPlan) (*remotesessions.IdentityCommit, *remotesessions.IdentityTx, remotesessions.Registration) {
	t.Helper()
	commit := identityCommitter(t, ti).Prepare(plan)
	reg, err := commit.Register(ctx)
	require.NoError(t, err)
	tx, err := commit.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { o11y.NoLogDefer(func() error { return tx.Rollback(context.Background()) }) })
	require.NoError(t, commit.Lock(ctx, tx))
	return commit, tx, reg
}

// linkPlan binds the stored client clientID of providerID, replacing the
// clients bound for the user session issuer's current provider.
func linkPlan(t *testing.T, ctx context.Context, userIssuerID, providerID, clientID uuid.UUID) remotesessions.IdentityPlan {
	t.Helper()
	return remotesessions.IdentityPlan{
		Scope: remotesessions.IdentityScope{
			OrganizationID:   activeOrganizationID(t, ctx),
			ProjectID:        projectIDFromContext(t, ctx),
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "identity-commit-test"),
			ActorDisplayName: nil,
		},
		UserSessionIssuerID: userIssuerID,
		Provider:            remotesessions.UseProvider(providerID),
		Client:              remotesessions.LinkClient(clientID),
		Bound:               remotesessions.ReplaceBound,
		ResourceDisplay:     nil,
	}
}
