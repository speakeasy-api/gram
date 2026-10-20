package remotesessions_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestResolveAuthClientAccessTokens_ServesAConsentFromADeletedSignIn(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-deleted-sign-in")
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	workloadCtx, caller := workloadCaller(t, ctx, f.ti.conn, auth, "ac-deleted-sign-in")
	member := f.member(t, ctx)
	f.consent(t, member, f.signInA, f.clientID, "member-token")
	require.NoError(t, testrepo.New(f.ti.conn).ForceSoftDeleteUserSessionIssuer(ctx, testrepo.ForceSoftDeleteUserSessionIssuerParams{ID: f.signInA, ProjectID: f.projectID}))

	for name, c := range map[string]struct {
		withCaller func() (context.Context, urn.SessionSubject)
	}{
		"user":     {func() (context.Context, urn.SessionSubject) { return ctx, member }},
		"workload": {func() (context.Context, urn.SessionSubject) { return workloadCtx, caller }},
	} {
		callerCtx, subject := c.withCaller()
		tokens, err := f.mgr.ResolveAuthClientAccessTokens(callerCtx, f.request(subject, f.clientID))
		require.NoError(t, err, name)
		require.Equal(t, "member-token", tokens[f.issuerID].Token, "%s: the deleted sign-in still names the consent's organization", name)
		reconnect, err := f.mgr.AuthClientNeedsReconnect(callerCtx, f.request(subject, f.clientID))
		require.NoError(t, err, name)
		require.False(t, reconnect, name)
	}
}

// reconsentFixture is a global registration linked to a sign-in in each of
// two organizations the fixture's user belongs to, with an agent in the first
// organization attached to the user's grant.
type reconsentFixture struct {
	authClientTokenFixture
	clientID, issuerID        uuid.UUID
	otherOrg                  string
	otherProject, otherSignIn uuid.UUID
	member                    urn.SessionSubject
	agentID                   uuid.UUID
}

func newReconsentFixture(t *testing.T, slug string) (context.Context, reconsentFixture) {
	t.Helper()
	ctx, f := newAuthClientTokenFixture(t, slug)
	r := reconsentFixture{authClientTokenFixture: f, member: f.member(t, ctx)}
	r.otherOrg, r.otherProject, r.otherSignIn = f.otherOrganizationSignIn(t, ctx, slug+"-b")
	r.issuerID = seedGlobalRemoteIssuer(t, ctx, f.ti.conn, slug+"-rsi")
	r.clientID = seedRemoteClientAtTier(t, ctx, f.ti.conn, uuid.NullUUID{}, pgtype.Text{}, r.issuerID, slug+"-client", f.signInA, r.otherSignIn)
	agent, err := agentsrepo.New(f.ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: f.organizationID, OwnerUserID: r.member.ID, ProjectID: conv.ToNullUUID(f.projectID), Name: slug})
	require.NoError(t, err)
	r.agentID = agent.ID
	return ctx, r
}

func (r reconsentFixture) store(t *testing.T, ctx context.Context, organizationID string, signIn uuid.UUID, token string) repo.RemoteSession {
	t.Helper()
	accessEnc, err := r.enc.Encrypt([]byte(token))
	require.NoError(t, err)
	session, err := remotesessions.StoreRemoteSessionGrant(ctx, repo.New(r.ti.conn), organizationID, repo.UpsertRemoteSessionParams{SubjectUrn: r.member, UserSessionIssuerID: signIn, RemoteSessionClientID: r.clientID, AccessTokenEncrypted: accessEnc, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{}})
	require.NoError(t, err)
	return session
}

func (r reconsentFixture) attachAgent(t *testing.T, ctx context.Context, session repo.RemoteSession) {
	t.Helper()
	_, err := repo.New(r.ti.conn).AttachPrincipalRemoteSessionBinding(ctx, repo.AttachPrincipalRemoteSessionBindingParams{ProjectID: r.projectID, OrganizationID: r.organizationID, PrincipalID: r.agentID, UserSessionIssuerID: r.signInA, SubjectUrn: r.member.String(), RemoteSessionID: session.ID})
	require.NoError(t, err)
}

func (r reconsentFixture) agentGrant(ctx context.Context) (repo.RemoteSession, error) {
	session, err := repo.New(r.ti.conn).GetPrincipalRemoteSessionBinding(ctx, repo.GetPrincipalRemoteSessionBindingParams{ProjectID: r.projectID, OrganizationID: r.organizationID, PrincipalID: r.agentID, UserSessionIssuerID: r.signInA, RemoteSessionClientID: r.clientID})
	if err != nil {
		return session, fmt.Errorf("get agent grant: %w", err)
	}
	return session, nil
}

func TestStoreRemoteSessionGrant_ReconsentInAnotherOrganizationStartsANewGrant(t *testing.T) {
	t.Parallel()
	ctx, r := newReconsentFixture(t, "ac-reconsent-org")
	inOrgA := r.request(r.member, r.clientID)
	inOrgB := remotesessions.AuthClientTokenRequest{ProjectID: r.otherProject, OrganizationID: r.otherOrg, UserSessionIssuerID: r.otherSignIn, Subject: r.member, RegistrationID: r.clientID, SkipUnusable: false}

	first := r.store(t, ctx, r.organizationID, r.signInA, "org-a-token")
	r.attachAgent(t, ctx, first)
	attached, err := r.agentGrant(ctx)
	require.NoError(t, err)
	require.Equal(t, first.ID, attached.ID)

	second := r.store(t, ctx, r.otherOrg, r.otherSignIn, "org-b-token")
	require.NotEqual(t, first.ID, second.ID, "a consent in another organization is a new grant")
	require.Equal(t, r.otherSignIn, second.UserSessionIssuerID)

	tokens, err := r.mgr.ResolveAuthClientAccessTokens(ctx, inOrgB)
	require.NoError(t, err)
	require.Equal(t, "org-b-token", tokens[r.issuerID].Token)
	require.Equal(t, second.ID, tokens[r.issuerID].RemoteSessionID)
	_, err = r.mgr.ResolveAuthClientAccessTokens(ctx, inOrgA)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "the grant no longer serves the first organization")
	reconnect, err := r.mgr.AuthClientNeedsReconnect(ctx, inOrgA)
	require.NoError(t, err)
	require.True(t, reconnect)

	// With the flags off the one live grant serves both sign-ins, as an
	// in-place update did.
	for _, legacy := range []struct {
		projectID      uuid.UUID
		organizationID string
		signIn         uuid.UUID
	}{{r.otherProject, r.otherOrg, r.otherSignIn}, {r.projectID, r.organizationID, r.signInA}} {
		tokens, err := r.mgr.ResolveClientAccessTokens(ctx, legacy.projectID, legacy.organizationID, legacy.signIn, r.member, r.clientID, false)
		require.NoError(t, err)
		require.Equal(t, "org-b-token", tokens[r.issuerID].Token)
	}

	_, err = r.agentGrant(ctx)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the agent's attachment stays on the tombstoned grant and resolves nothing")
}

func TestStoreRemoteSessionGrant_ReconsentInTheSameOrganizationUpdatesInPlace(t *testing.T) {
	t.Parallel()
	ctx, r := newReconsentFixture(t, "ac-reconsent-same")

	first := r.store(t, ctx, r.organizationID, r.signInA, "first-token")
	r.attachAgent(t, ctx, first)
	second := r.store(t, ctx, r.organizationID, r.signInB, "second-token")
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, r.signInA, second.UserSessionIssuerID, "provenance is unchanged")
	require.Equal(t, first.GrantGeneration+1, second.GrantGeneration)

	tokens, err := r.mgr.ResolveAuthClientAccessTokens(ctx, r.request(r.member, r.clientID))
	require.NoError(t, err)
	require.Equal(t, "second-token", tokens[r.issuerID].Token)

	_, err = r.agentGrant(ctx)
	require.ErrorIs(t, err, pgx.ErrNoRows, "an attachment pins the previous grant generation, as before")
}

func TestStoreRemoteSessionGrant_WithoutOrganizationProvenanceIsRefused(t *testing.T) {
	t.Parallel()
	ctx, r := newReconsentFixture(t, "ac-reconsent-no-org")

	first := r.store(t, ctx, r.organizationID, r.signInA, "first-token")
	accessEnc, err := r.enc.Encrypt([]byte("second-token"))
	require.NoError(t, err)
	_, err = remotesessions.StoreRemoteSessionGrant(ctx, repo.New(r.ti.conn), "", repo.UpsertRemoteSessionParams{SubjectUrn: r.member, UserSessionIssuerID: r.otherSignIn, RemoteSessionClientID: r.clientID, AccessTokenEncrypted: accessEnc, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{}})
	require.Error(t, err, "a state without an organization cannot write a grant")

	kept, err := repo.New(r.ti.conn).GetRemoteSessionByID(ctx, repo.GetRemoteSessionByIDParams{ID: first.ID, ProjectID: r.projectID, OrganizationID: r.organizationID})
	require.NoError(t, err)
	require.Equal(t, first.GrantGeneration, kept.GrantGeneration, "the existing grant is untouched")
}

func TestGetActiveRemoteSessionInTenant_RefusesADeletedIssuer(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-active-deleted-issuer")
	subject := urn.NewUserSubject("ac-active-deleted-issuer-user")
	f.consent(t, subject, f.signInA, f.clientID, "token")
	params := repo.GetActiveRemoteSessionInTenantParams{ProjectID: f.projectID, OrganizationID: f.organizationID, SubjectUrn: subject, RemoteSessionClientID: f.clientID}
	q := repo.New(f.ti.conn)
	_, err := q.GetActiveRemoteSessionInTenant(ctx, params)
	require.NoError(t, err)

	n, err := q.SoftDeleteRemoteSessionIssuerFixture(ctx, f.issuerID)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	_, err = q.GetActiveRemoteSessionInTenant(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
