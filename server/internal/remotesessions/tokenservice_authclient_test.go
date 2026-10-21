package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// authClientTokenFixture is one project with two sign-ins, A and B, and a
// registration linked to A only.
type authClientTokenFixture struct {
	ti             *testInstance
	enc            *encryption.Client
	mgr            *remotesessions.ChallengeManager
	projectID      uuid.UUID
	organizationID string
	signInA        uuid.UUID
	signInB        uuid.UUID
	clientID       uuid.UUID
	issuerID       uuid.UUID
}

func newAuthClientTokenFixture(t *testing.T, slug string) (context.Context, authClientTokenFixture) {
	t.Helper()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	enc := testenv.NewEncryptionClient(t)
	signInA := createUserSessionIssuer(t, ctx, ti.conn, slug+"-a")
	signInB := createUserSessionIssuer(t, ctx, ti.conn, slug+"-b")
	clientID, issuerID := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, signInA, authCtx.ActiveOrganizationID, slug)
	return ctx, authClientTokenFixture{
		ti:             ti,
		enc:            enc,
		mgr:            newResolveManager(t, ti.conn, enc),
		projectID:      *authCtx.ProjectID,
		organizationID: authCtx.ActiveOrganizationID,
		signInA:        signInA,
		signInB:        signInB,
		clientID:       clientID,
		issuerID:       issuerID,
	}
}

// consent stores subject's token for clientID, consented under signIn.
func (f authClientTokenFixture) consent(t *testing.T, subject urn.SessionSubject, signIn, clientID uuid.UUID, token string) repo.RemoteSession {
	t.Helper()
	return upsertLiveSession(t, t.Context(), f.ti.conn, f.enc, subject, signIn, clientID, token)
}

func upsertLiveSession(t *testing.T, ctx context.Context, conn *pgxpool.Pool, enc *encryption.Client, subject urn.SessionSubject, signIn, clientID uuid.UUID, token string) repo.RemoteSession {
	t.Helper()
	accessEnc, err := enc.Encrypt([]byte(token))
	require.NoError(t, err)
	session, err := repo.New(conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   signIn,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		Scopes:                []string{},
	})
	require.NoError(t, err)
	return session
}

// request resolves registrationID for subject on a server whose sign-in is B.
func (f authClientTokenFixture) request(subject urn.SessionSubject, registrationID uuid.UUID) remotesessions.AuthClientTokenRequest {
	return remotesessions.AuthClientTokenRequest{
		ProjectID:           f.projectID,
		OrganizationID:      f.organizationID,
		UserSessionIssuerID: f.signInB,
		Subject:             subject,
		RegistrationID:      registrationID,
		SkipUnusable:        false,
	}
}

func TestResolveAuthClientAccessTokens_ServesAConsentFromAnotherSignIn(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-cross-sign-in")
	subject := urn.NewUserSubject("ac-cross-sign-in-user")
	session := f.consent(t, subject, f.signInA, f.clientID, "token-from-a")

	tokens, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(subject, f.clientID))
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]remotesessions.UpstreamToken{f.issuerID: {Token: "token-from-a", Resource: "", CredentialOwner: remotesessions.CredentialOwnerSubject, RemoteSessionClientID: f.clientID}}, tokenCredentials(tokens))
	require.Equal(t, session.ID, tokens[f.issuerID].RemoteSessionID, "the consented row itself serves, so refresh writes back to it")

	reconnect, err := f.mgr.AuthClientNeedsReconnect(ctx, f.request(subject, f.clientID))
	require.NoError(t, err)
	require.False(t, reconnect)
}

func TestResolveClientAccessTokens_StillRequiresTheSignInLink(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-legacy-link")
	subject := urn.NewUserSubject("ac-legacy-link-user")
	f.consent(t, subject, f.signInA, f.clientID, "token-from-a")

	_, err := f.mgr.ResolveClientAccessTokens(ctx, f.projectID, f.organizationID, f.signInB, subject, f.clientID, false)
	require.ErrorIs(t, err, remotesessions.ErrChosenClientUnbound)
}

func TestResolveAuthClientAccessTokens_EachRegistrationServesItsOwnToken(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-two-registrations")
	otherClientID, otherIssuerID := seedActiveClient(t, ctx, f.ti.conn, f.projectID, f.signInA, f.organizationID, "ac-two-registrations-2")
	subject := urn.NewUserSubject("ac-two-registrations-user")
	f.consent(t, subject, f.signInA, f.clientID, "first-token")
	f.consent(t, subject, f.signInA, otherClientID, "second-token")

	first, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(subject, f.clientID))
	require.NoError(t, err)
	require.Equal(t, "first-token", first[f.issuerID].Token)
	require.Len(t, first, 1)

	second, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(subject, otherClientID))
	require.NoError(t, err)
	require.Equal(t, "second-token", second[otherIssuerID].Token)
	require.Len(t, second, 1)
}

func TestResolveAuthClientAccessTokens_NeverServesAnotherSubject(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-other-subject")
	f.consent(t, urn.NewUserSubject("ac-owner"), f.signInA, f.clientID, "owner-token")
	intruder := urn.NewUserSubject("ac-intruder")

	_, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(intruder, f.clientID))
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)

	req := f.request(intruder, f.clientID)
	req.SkipUnusable = true
	tokens, err := f.mgr.ResolveAuthClientAccessTokens(ctx, req)
	require.NoError(t, err)
	require.Empty(t, tokens)

	reconnect, err := f.mgr.AuthClientNeedsReconnect(ctx, f.request(intruder, f.clientID))
	require.NoError(t, err)
	require.True(t, reconnect)
}

func TestResolveAuthClientAccessTokens_RefusesAnotherProjectsRegistration(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-other-project")
	otherProject := createProject(t, ctx, f.ti.conn, "ac-other-project-p2")
	otherIssuer := createRemoteIssuerInProject(t, ctx, f.ti.conn, otherProject, "ac-other-project-rsi")
	otherClient := seedRemoteClientAtTier(t, ctx, f.ti.conn, conv.ToNullUUID(otherProject), conv.ToPGText(f.organizationID), otherIssuer, "ac-other-project-client")
	subject := urn.NewUserSubject("ac-other-project-user")
	f.consent(t, subject, f.signInA, otherClient, "other-project-token")

	_, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(subject, otherClient))
	require.ErrorIs(t, err, remotesessions.ErrChosenClientUnbound)

	reconnect, err := f.mgr.AuthClientNeedsReconnect(ctx, f.request(subject, otherClient))
	require.NoError(t, err)
	require.True(t, reconnect)
}

func TestResolveAuthClientAccessTokens_RefusesAnotherOrganizationsRegistration(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-other-org")
	otherOrg := createOrganization(t, ctx, f.ti.conn, "ac-other-org-tenant")
	otherIssuer := seedOrgLevelRemoteIssuer(t, ctx, f.ti.conn, otherOrg, "ac-other-org-rsi")
	otherClient := seedRemoteClientAtTier(t, ctx, f.ti.conn, uuid.NullUUID{}, conv.ToPGText(otherOrg), otherIssuer, "ac-other-org-client")
	subject := urn.NewUserSubject("ac-other-org-user")
	f.consent(t, subject, f.signInA, otherClient, "other-org-token")

	_, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(subject, otherClient))
	require.ErrorIs(t, err, remotesessions.ErrChosenClientUnbound)
}

func TestResolveAuthClientAccessTokens_RefusesADeletedRegistrationOrIssuer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		delete func(ctx context.Context, f authClientTokenFixture) (int64, error)
	}{
		{"registration", func(ctx context.Context, f authClientTokenFixture) (int64, error) {
			return repo.New(f.ti.conn).SoftDeleteRemoteSessionClientFixture(ctx, f.clientID)
		}},
		{"issuer", func(ctx context.Context, f authClientTokenFixture) (int64, error) {
			return repo.New(f.ti.conn).SoftDeleteRemoteSessionIssuerFixture(ctx, f.issuerID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newAuthClientTokenFixture(t, "ac-deleted-"+tc.name)
			subject := urn.NewUserSubject("ac-deleted-user")
			f.consent(t, subject, f.signInA, f.clientID, "deleted-token")
			n, err := tc.delete(ctx, f)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)

			_, err = f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(subject, f.clientID))
			require.ErrorIs(t, err, remotesessions.ErrChosenClientUnbound)

			reconnect, err := f.mgr.AuthClientNeedsReconnect(ctx, f.request(subject, f.clientID))
			require.NoError(t, err)
			require.True(t, reconnect)
		})
	}
}

func TestResolveAuthClientAccessTokens_AgentAttachmentsKeepTheSignInLink(t *testing.T) {
	t.Parallel()
	_, f := newAuthClientTokenFixture(t, "ac-agent")
	agent := urn.NewAgentSubject(uuid.New())

	// No authenticated actor: the agent subject alone marks an attachment.
	ctx := t.Context()
	_, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(agent, f.clientID))
	require.ErrorIs(t, err, remotesessions.ErrChosenClientUnbound)

	reconnect, err := f.mgr.AuthClientNeedsReconnect(ctx, f.request(agent, f.clientID))
	require.NoError(t, err)
	require.True(t, reconnect)
}

func TestResolveAuthClientAccessTokens_SharedClientsOnOneIssuerEachResolve(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-shared-issuer")
	sibling := seedRemoteClientAtTier(t, ctx, f.ti.conn, conv.ToNullUUID(f.projectID), conv.ToPGText(f.organizationID), f.issuerID, "ac-shared-issuer-sibling", f.signInA)
	subject := urn.NewUserSubject("ac-shared-issuer-user")
	f.consent(t, subject, f.signInA, f.clientID, "first-shared")
	f.consent(t, subject, f.signInA, sibling, "second-shared")

	// Both clients on one upstream issuer under one sign-in trip the legacy
	// resolver's uniqueness invariant.
	_, err := f.mgr.ResolveAccessTokens(ctx, f.projectID, f.organizationID, f.signInA, subject)
	require.Error(t, err)

	first, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(subject, f.clientID))
	require.NoError(t, err)
	require.Equal(t, "first-shared", first[f.issuerID].Token)
	second, err := f.mgr.ResolveAuthClientAccessTokens(ctx, f.request(subject, sibling))
	require.NoError(t, err)
	require.Equal(t, "second-shared", second[f.issuerID].Token)
}

func TestResolveAuthClientAccessTokens_RejectsAnIncompleteRequest(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-incomplete")
	req := f.request(urn.NewUserSubject("ac-incomplete-user"), uuid.Nil)
	_, err := f.mgr.ResolveAuthClientAccessTokens(ctx, req)
	require.ErrorIs(t, err, remotesessions.ErrInvalidAuthorizationRequest)
	_, err = f.mgr.AuthClientNeedsReconnect(ctx, req)
	require.ErrorIs(t, err, remotesessions.ErrInvalidAuthorizationRequest)
}

// The tenant-scoped lookup behind principal credentials and workloads keeps
// its membership and reach checks without the sign-in link.
func TestGetTenantUserRemoteSessionForClient(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-tenant")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	member := urn.NewUserSubject(authCtx.UserID)
	session := f.consent(t, member, f.signInA, f.clientID, "member-token")
	q := repo.New(f.ti.conn)
	params := func(subject urn.SessionSubject, clientID, projectID uuid.UUID, organizationID string) repo.GetTenantUserRemoteSessionForClientParams {
		return repo.GetTenantUserRemoteSessionForClientParams{ProjectID: projectID, OrganizationID: organizationID, SubjectUrn: subject, RemoteSessionClientID: clientID}
	}

	got, err := q.GetTenantUserRemoteSessionForClient(ctx, params(member, f.clientID, f.projectID, f.organizationID))
	require.NoError(t, err)
	require.Equal(t, session.ID, got.ID, "a member's session serves whichever sign-in it came through")

	_, err = q.GetTenantUserRemoteSession(ctx, repo.GetTenantUserRemoteSessionParams{SubjectUrn: member, RemoteSessionClientID: f.clientID, ProjectID: f.projectID, OrganizationID: f.organizationID, UserSessionIssuerID: f.signInB})
	require.ErrorIs(t, err, pgx.ErrNoRows, "the legacy lookup still requires the sign-in")

	nonMember := "ac-tenant-non-member"
	seedUser(t, ctx, f.ti.conn, nonMember, nonMember+"@example.com", "Non Member")
	f.consent(t, urn.NewUserSubject(nonMember), f.signInA, f.clientID, "non-member-token")
	_, err = q.GetTenantUserRemoteSessionForClient(ctx, params(urn.NewUserSubject(nonMember), f.clientID, f.projectID, f.organizationID))
	require.ErrorIs(t, err, pgx.ErrNoRows, "a user outside the organization is refused")

	otherProject := createProject(t, ctx, f.ti.conn, "ac-tenant-p2")
	_, err = q.GetTenantUserRemoteSessionForClient(ctx, params(member, f.clientID, otherProject, f.organizationID))
	require.ErrorIs(t, err, pgx.ErrNoRows, "another project cannot reach a project's registration")

	otherOrg := createOrganization(t, ctx, f.ti.conn, "ac-tenant-other-org")
	_, err = q.GetTenantUserRemoteSessionForClient(ctx, params(member, f.clientID, f.projectID, otherOrg))
	require.ErrorIs(t, err, pgx.ErrNoRows, "the project must belong to the organization")

	_, err = q.SoftDeleteRemoteSessionClientFixture(ctx, f.clientID)
	require.NoError(t, err)
	_, err = q.GetTenantUserRemoteSessionForClient(ctx, params(member, f.clientID, f.projectID, f.organizationID))
	require.ErrorIs(t, err, pgx.ErrNoRows, "a deleted registration serves nothing")
}

func TestAuthClientRemoteSessionStatuses_ReportWhatTheResolverServes(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "ac-status")
	otherOrg := createOrganization(t, ctx, f.ti.conn, "ac-status-tenant")
	otherIssuer := seedOrgLevelRemoteIssuer(t, ctx, f.ti.conn, otherOrg, "ac-status-rsi")
	otherClient := seedRemoteClientAtTier(t, ctx, f.ti.conn, uuid.NullUUID{}, conv.ToPGText(otherOrg), otherIssuer, "ac-status-client")
	subject := urn.NewUserSubject("ac-status-user")
	session := f.consent(t, subject, f.signInA, f.clientID, "token-from-a")

	clients, err := f.mgr.ListAuthClientRegistrations(ctx, f.projectID, f.organizationID, []uuid.UUID{f.clientID, otherClient})
	require.NoError(t, err)
	require.Len(t, clients, 1, "another organization's registration is out of reach")
	require.Equal(t, f.clientID, clients[0].ID)

	statuses, err := f.mgr.AuthClientRemoteSessionStatuses(ctx, subject, f.projectID, f.organizationID, []uuid.UUID{f.clientID})
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[f.clientID].Status, "a consent through another sign-in reports as the resolver serves it")
	require.Equal(t, session.ID, statuses[f.clientID].ID)

	linked, err := f.mgr.RemoteSessionStatuses(ctx, subject, f.projectID, f.organizationID, f.signInB)
	require.NoError(t, err)
	require.Empty(t, linked, "without the resolver, sign-in B has no link to the registration")

	other, err := f.mgr.AuthClientRemoteSessionStatuses(ctx, urn.NewUserSubject("ac-status-other-user"), f.projectID, f.organizationID, []uuid.UUID{f.clientID})
	require.NoError(t, err)
	require.Empty(t, other, "another user's grant never reports")
}

func TestAuthClientRemoteSessionStatuses_SkipAGrantConsentedInAnotherOrganization(t *testing.T) {
	t.Parallel()
	ctx, r := newReconsentFixture(t, "ac-status-org")
	r.store(t, ctx, r.otherOrg, r.otherSignIn, "org-b-token")

	statuses, err := r.mgr.AuthClientRemoteSessionStatuses(ctx, r.member, r.projectID, r.organizationID, []uuid.UUID{r.clientID})
	require.NoError(t, err)
	require.Empty(t, statuses, "the resolver does not serve the first organization a grant consented in another")
	_, err = r.mgr.ResolveAuthClientAccessTokens(ctx, r.request(r.member, r.clientID))
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)

	inOrgB, err := r.mgr.AuthClientRemoteSessionStatuses(ctx, r.member, r.otherProject, r.otherOrg, []uuid.UUID{r.clientID})
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, inOrgB[r.clientID].Status)
}
