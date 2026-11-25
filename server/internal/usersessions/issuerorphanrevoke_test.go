// Issuer retirement uses exact grant provenance, not the registration's
// compatibility links, and calls the upstream only after commit.

package usersessions_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// orphanRevocationSpy records what the fake upstream's /revoke received; the
// handler runs on its own goroutine, hence the mutex. A non-zero status makes
// the upstream answer every POST with that code instead of 200.
type orphanRevocationSpy struct {
	mu     sync.Mutex
	calls  int
	tokens []string
	status int
}

func (s *orphanRevocationSpy) snapshot() (int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, append([]string(nil), s.tokens...)
}

func newOrphanRevocationUpstream(t *testing.T, spy *orphanRevocationSpy) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		spy.mu.Lock()
		spy.calls++
		spy.tokens = append(spy.tokens, form.Get("token"))
		status := spy.status
		spy.mu.Unlock()

		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server
}

// seedRevocableClient seeds a remote issuer advertising revocationEndpoint and
// a client bound to userSessionIssuerID; orgLevel stores the client at the
// organization level (project_id NULL) instead of on the auth context project.
func seedRevocableClient(t *testing.T, ctx context.Context, ti *testInstance, userSessionIssuerID uuid.UUID, slug string, revocationEndpoint string, orgLevel bool) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	q := remotesessionsrepo.New(ti.conn)

	issuer, err := q.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		Slug:                              slug,
		Issuer:                            "https://" + slug + ".example.com",
		Name:                              pgtype.Text{String: "", Valid: false},
		LogoAssetID:                       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientSetupDocumentationUrl:       pgtype.Text{String: "", Valid: false},
		AuthorizationEndpoint:             conv.ToPGText("https://" + slug + ".example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://" + slug + ".example.com/token"),
		RevocationEndpoint:                conv.ToPGTextEmpty(revocationEndpoint),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ServiceDocumentation:              pgtype.Text{String: "", Valid: false},
		OpPolicyUri:                       pgtype.Text{String: "", Valid: false},
		OpTosUri:                          pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{},
		ResponseTypesSupported:            []string{},
		TokenEndpointAuthMethodsSupported: []string{},
		ClientIDMetadataDocumentSupported: false,
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	clientProject := conv.ToNullUUID(*authCtx.ProjectID)
	if orgLevel {
		clientProject = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	client, err := q.CreateRemoteSessionClientFixture(ctx, remotesessionsrepo.CreateRemoteSessionClientFixtureParams{
		ProjectID:               clientProject,
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                slug + "-cid",
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		TokenEndpointAuthMethod: conv.ToPGTextEmpty("none"),
		Scope:                   nil,
		Audience:                pgtype.Text{String: "", Valid: false},
		LegacyCallbackUrl:       false,
	})
	require.NoError(t, err)

	return client.ID
}

// seedLegacyProjectRevocableClient mirrors a project client written before
// remote_session_clients.organization_id was populated. The project remains
// the authoritative source of its organization tenancy.
func seedLegacyProjectRevocableClient(t *testing.T, ctx context.Context, ti *testInstance, userSessionIssuerID uuid.UUID, slug string, revocationEndpoint string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	q := remotesessionsrepo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		Slug:                              slug,
		Issuer:                            "https://" + slug + ".example.com",
		Name:                              pgtype.Text{String: "", Valid: false},
		LogoAssetID:                       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientSetupDocumentationUrl:       pgtype.Text{String: "", Valid: false},
		AuthorizationEndpoint:             conv.ToPGText("https://" + slug + ".example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://" + slug + ".example.com/token"),
		RevocationEndpoint:                conv.ToPGTextEmpty(revocationEndpoint),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ServiceDocumentation:              pgtype.Text{String: "", Valid: false},
		OpPolicyUri:                       pgtype.Text{String: "", Valid: false},
		OpTosUri:                          pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{},
		ResponseTypesSupported:            []string{},
		TokenEndpointAuthMethodsSupported: []string{},
		ClientIDMetadataDocumentSupported: false,
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClientFixture(ctx, remotesessionsrepo.CreateRemoteSessionClientFixtureParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          pgtype.Text{String: "", Valid: false},
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                slug + "-cid",
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		TokenEndpointAuthMethod: conv.ToPGTextEmpty("none"),
		Scope:                   nil,
		Audience:                pgtype.Text{String: "", Valid: false},
		LegacyCallbackUrl:       false,
	})
	require.NoError(t, err)
	return client.ID
}

// seedRemoteSession stores a session with real ciphertext for the subject on
// the client; returns the plaintext refresh token the revoker should send.
func seedRemoteSession(t *testing.T, ctx context.Context, ti *testInstance, sessionIssuerID uuid.UUID, clientID uuid.UUID, subject string) string {
	t.Helper()

	enc := testenv.NewEncryptionClient(t)
	accessEnc, err := enc.Encrypt([]byte(subject + "-access"))
	require.NoError(t, err)
	refreshToken := subject + "-refresh"
	refreshEnc, err := enc.Encrypt([]byte(refreshToken))
	require.NoError(t, err)

	_, err = remotesessionsrepo.New(ti.conn).UpsertRemoteSessionFixture(ctx, remotesessionsrepo.UpsertRemoteSessionFixtureParams{
		SubjectUrn:             urn.NewUserSubject(subject),
		UserSessionIssuerID:    sessionIssuerID,
		RemoteSessionClientID:  clientID,
		AccessTokenEncrypted:   accessEnc,
		AccessExpiresAt:        conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted:  conv.ToPGText(refreshEnc),
		AuthorizationExpiresAt: pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		RefreshExpiresAt:       pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		Scopes:                 []string{},
		Resource:               pgtype.Text{String: "", Valid: false},
		AutoRefresh:            false,
	})
	require.NoError(t, err)

	return refreshToken
}

// seedRevocableUpstream seeds a remote issuer advertising revocationEndpoint, a
// client bound to userSessionIssuerID, and a session with real ciphertext;
// returns the client id and the plaintext refresh token the revoker should send.
func seedRevocableUpstream(t *testing.T, ctx context.Context, ti *testInstance, userSessionIssuerID uuid.UUID, slug string, revocationEndpoint string) (clientID uuid.UUID, refreshToken string) {
	t.Helper()

	clientID = seedRevocableClient(t, ctx, ti, userSessionIssuerID, slug, revocationEndpoint, false)
	refreshToken = seedRemoteSession(t, ctx, ti, userSessionIssuerID, clientID, "orphan-subject-"+slug)
	return clientID, refreshToken
}

func createIssuerForOrphanTest(t *testing.T, ctx context.Context, ti *testInstance, slug string) uuid.UUID {
	t.Helper()

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 slug,
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	return uuid.MustParse(issuer.ID)
}

func deleteIssuerForOrphanTest(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) {
	t.Helper()

	require.NoError(t, ti.service.DeleteUserSessionIssuer(ctx, &issuersgen.DeleteUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               issuerID.String(),
	}))
}

func countActiveClientSessions(t *testing.T, ctx context.Context, ti *testInstance, clientID uuid.UUID) int {
	t.Helper()

	active, err := remotesessionsrepo.New(ti.conn).CountActiveRemoteSessionsByClientID(ctx, clientID)
	require.NoError(t, err)
	return int(active)
}

// Deleting an issuer that holds a client's only binding tombstones the client's
// sessions and pushes an RFC 7009 revocation for each.
func TestDeleteUserSessionIssuer_RevokesOrphanedClientSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-sole-issuer")
	clientID, refreshToken := seedRevocableUpstream(t, ctx, ti, issuerID, "orphan-sole", upstream.URL+"/revoke")

	deleteIssuerForOrphanTest(t, ctx, ti, issuerID)

	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientID), "the orphaned client's sessions are tombstoned with the issuer")

	calls, tokens := spy.snapshot()
	require.Equal(t, 1, calls, "one upstream revocation per orphaned session")
	require.Equal(t, []string{refreshToken}, tokens, "the stored refresh token is what gets revoked")
}

// A sibling binding whose issuer is already tombstoned does not spare the
// client: a dead binding counts as no binding.
func TestDeleteUserSessionIssuer_DeadSiblingBindingDoesNotSpare(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-deadsib-issuer")
	siblingID := createIssuerForOrphanTest(t, ctx, ti, "orphan-deadsib-sibling")
	clientID, refreshToken := seedRevocableUpstream(t, ctx, ti, issuerID, "orphan-deadsib", upstream.URL+"/revoke")

	// Tombstone the sibling issuer directly so its binding row survives dead.
	_, err := usersessionsrepo.New(ti.conn).DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{
		ID:        siblingID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	deleteIssuerForOrphanTest(t, ctx, ti, issuerID)

	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientID), "a dead sibling binding does not keep the sessions alive")

	_, tokens := spy.snapshot()
	require.Equal(t, []string{refreshToken}, tokens, "the grant is revoked despite the dead sibling binding")
}

// One issuer delete orphaning several clients revokes every client's sessions.
func TestDeleteUserSessionIssuer_RevokesAllOrphanedClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-multi-issuer")
	clientA, refreshA := seedRevocableUpstream(t, ctx, ti, issuerID, "orphan-multi-a", upstream.URL+"/revoke")
	clientB, refreshB := seedRevocableUpstream(t, ctx, ti, issuerID, "orphan-multi-b", upstream.URL+"/revoke")

	deleteIssuerForOrphanTest(t, ctx, ti, issuerID)

	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientA), "the first orphaned client's sessions are tombstoned")
	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientB), "the second orphaned client's sessions are tombstoned")

	calls, tokens := spy.snapshot()
	require.Equal(t, 2, calls, "one upstream revocation per orphaned credential")
	require.ElementsMatch(t, []string{refreshA, refreshB}, tokens, "each orphaned client's refresh token is revoked")
}

// An orphaned client holding no sessions is a clean no-op: the delete succeeds
// and nothing is POSTed upstream.
func TestDeleteUserSessionIssuer_OrphanedClientWithoutSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-empty-issuer")
	clientID := seedRevocableClient(t, ctx, ti, issuerID, "orphan-empty", upstream.URL+"/revoke", false)

	deleteIssuerForOrphanTest(t, ctx, ti, issuerID)

	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientID))

	calls, _ := spy.snapshot()
	require.Zero(t, calls, "a sessionless orphan produces no upstream traffic")
}

// Every subject's grant on an orphaned client is tombstoned and revoked, one
// POST per credential.
func TestDeleteUserSessionIssuer_RevokesEverySubjectOnOrphanedClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-subjects-issuer")
	clientID := seedRevocableClient(t, ctx, ti, issuerID, "orphan-subjects", upstream.URL+"/revoke", false)
	refreshOne := seedRemoteSession(t, ctx, ti, issuerID, clientID, "orphan-subjects-one")
	refreshTwo := seedRemoteSession(t, ctx, ti, issuerID, clientID, "orphan-subjects-two")

	deleteIssuerForOrphanTest(t, ctx, ti, issuerID)

	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientID), "every subject's session is tombstoned")

	calls, tokens := spy.snapshot()
	require.Equal(t, 2, calls, "one upstream revocation per subject's credential")
	require.ElementsMatch(t, []string{refreshOne, refreshTwo}, tokens)
}

// An organization-level client (project_id NULL) is still swept when orphaned,
// including sessions whose provenance issuer lives in another project.
func TestDeleteUserSessionIssuer_SparesOtherProjectProvenanceOnSharedRegistration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	// Provenance issuer in a second project of the same org.
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "orphan-xproj",
		Slug:           "orphan-xproj",
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	otherIssuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          otherProject.ID,
		Slug:               "orphan-xproj-issuer",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-orglevel-issuer")
	clientID := seedRevocableClient(t, ctx, ti, issuerID, "orphan-orglevel", upstream.URL+"/revoke", true)
	seedRemoteSession(t, ctx, ti, otherIssuer.ID, clientID, "orphan-orglevel-subject")

	deleteIssuerForOrphanTest(t, ctx, ti, issuerID)

	require.Equal(t, 1, countActiveClientSessions(t, ctx, ti, clientID), "issuer retirement does not widen to another project sharing the registration")

	_, tokens := spy.snapshot()
	require.Empty(t, tokens)
}

func TestDeleteOrganizationUserSessionIssuer_RevokesLegacyProjectClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)
	issuer, err := ti.service.CreateIssuer(ctx, &orgissuersgen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "orphan-org-legacy-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerID := uuid.MustParse(issuer.ID)
	clientID := seedLegacyProjectRevocableClient(t, ctx, ti, issuerID, "orphan-org-legacy", upstream.URL+"/revoke")
	refreshToken := seedRemoteSession(t, ctx, ti, issuerID, clientID, "orphan-org-legacy-subject")

	require.NoError(t, ti.service.DeleteIssuer(ctx, &orgissuersgen.DeleteIssuerPayload{ID: issuer.ID, SessionToken: nil}))
	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientID))
	_, tokens := spy.snapshot()
	require.Equal(t, []string{refreshToken}, tokens)
}

// A failing upstream revocation endpoint does not undo the local tombstone:
// fail-secure means the grant is dead in Speakeasy even when the POST is rejected.
func TestDeleteUserSessionIssuer_UpstreamFailureKeepsLocalTombstone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	spy := &orphanRevocationSpy{}
	spy.status = http.StatusInternalServerError
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-fail-issuer")
	clientID, refreshToken := seedRevocableUpstream(t, ctx, ti, issuerID, "orphan-fail", upstream.URL+"/revoke")

	deleteIssuerForOrphanTest(t, ctx, ti, issuerID)

	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientID), "the tombstone persists when the upstream rejects the revocation")

	calls, tokens := spy.snapshot()
	require.Equal(t, 1, calls, "the revocation was attempted despite the 500")
	require.Equal(t, []string{refreshToken}, tokens)
}

// Re-binding the client after the orphan revoke mints a fresh session row; the
// tombstoned grant is never resurrected.
func TestDeleteUserSessionIssuer_RebindMintsFreshSessionWithoutResurrection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-rebind-issuer")
	clientID, _ := seedRevocableUpstream(t, ctx, ti, issuerID, "orphan-rebind", upstream.URL+"/revoke")
	subject := urn.NewUserSubject("orphan-subject-orphan-rebind")

	q := remotesessionsrepo.New(ti.conn)
	old, err := q.GetActiveRemoteSession(ctx, remotesessionsrepo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	require.NoError(t, err)

	deleteIssuerForOrphanTest(t, ctx, ti, issuerID)

	_, err = q.GetActiveRemoteSession(ctx, remotesessionsrepo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "the orphaned grant is tombstoned")

	// Fresh connect: bind the client to a new issuer and re-auth the subject.
	rebindIssuer := createIssuerForOrphanTest(t, ctx, ti, "orphan-rebind-fresh")
	seedRemoteSession(t, ctx, ti, rebindIssuer, clientID, "orphan-subject-orphan-rebind")

	fresh, err := q.GetActiveRemoteSession(ctx, remotesessionsrepo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	require.NoError(t, err)
	require.NotEqual(t, old.ID, fresh.ID, "the fresh connect mints a new row instead of reviving the tombstone")
	require.Equal(t, rebindIssuer, fresh.UserSessionIssuerID)
	require.Equal(t, 1, countActiveClientSessions(t, ctx, ti, clientID), "exactly one live grant after the re-bind")
}

// Issuer retirement serializes with an in-flight registration write. Once
// that writer releases the registration, retirement rechecks the grant.
func TestDeleteUserSessionIssuer_WaitsForRegistrationWriterBeforeRetiringGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-race-issuer")
	clientID, refreshToken := seedRevocableUpstream(t, ctx, ti, issuerID, "orphan-race", upstream.URL+"/revoke")

	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := remotesessionsrepo.New(tx).LockRemoteSessionClientForSessionWrite(ctx, clientID)
	require.NoError(t, err)

	deleteErr := make(chan error, 1)
	go func() {
		deleteErr <- ti.service.DeleteUserSessionIssuer(ctx, &issuersgen.DeleteUserSessionIssuerPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			ID:               issuerID.String(),
		})
	}()

	testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, testenv.BackendPID(tx), 1)

	require.NoError(t, tx.Commit(ctx))

	require.NoError(t, <-deleteErr)

	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientID), "issuer retirement finds its exact grant after acquiring the registration lock")

	calls, tokens := spy.snapshot()
	require.Equal(t, 1, calls, "exactly one post-commit revocation")
	require.Equal(t, []string{refreshToken}, tokens)
}
