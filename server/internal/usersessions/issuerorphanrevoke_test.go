// Pins the issuer-delete cascade for orphaned upstream grants: sessions on a
// client whose only live binding was the deleted issuer are tombstoned and
// revoked upstream; a client with a live sibling binding is left alone.

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
	client, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
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

	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userSessionIssuerID,
	}))

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

	_, err = remotesessionsrepo.New(ti.conn).UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
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

// A client also bound to a live sibling issuer keeps its sessions: they remain
// reachable and revocable through the sibling's bindings.
func TestDeleteUserSessionIssuer_SparesClientsBoundToSiblingIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-shared-issuer")
	siblingID := createIssuerForOrphanTest(t, ctx, ti, "orphan-shared-sibling")
	clientID, refreshToken := seedRevocableUpstream(t, ctx, ti, issuerID, "orphan-shared", upstream.URL+"/revoke")

	require.NoError(t, remotesessionsrepo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   siblingID,
	}))

	deleteIssuerForOrphanTest(t, ctx, ti, issuerID)

	require.Equal(t, 1, countActiveClientSessions(t, ctx, ti, clientID), "sessions on a client with a live sibling binding stay live")

	calls, _ := spy.snapshot()
	require.Zero(t, calls, "no upstream revocation for a still-reachable grant")

	// Deleting the sibling removes the client's last live binding, so the
	// cascade must catch the grant it spared above.
	deleteIssuerForOrphanTest(t, ctx, ti, siblingID)

	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientID), "the last binding's deletion tombstones the spared sessions")

	_, tokens := spy.snapshot()
	require.Equal(t, []string{refreshToken}, tokens, "the spared grant is revoked when its last binding goes")
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

	require.NoError(t, remotesessionsrepo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   siblingID,
	}))

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
func TestDeleteUserSessionIssuer_RevokesOrgLevelClientAcrossProjects(t *testing.T) {
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
	refreshToken := seedRemoteSession(t, ctx, ti, otherIssuer.ID, clientID, "orphan-orglevel-subject")

	deleteIssuerForOrphanTest(t, ctx, ti, issuerID)

	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientID), "the org-level client's sessions are tombstoned regardless of provenance project")

	_, tokens := spy.snapshot()
	require.Equal(t, []string{refreshToken}, tokens, "the cross-project grant is revoked with the client")
}

// A failing upstream revocation endpoint does not undo the local tombstone:
// fail-secure means the grant is dead in Gram even when the POST is rejected.
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
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   rebindIssuer,
	}))
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

// Write-skew guard: a sibling delete that commits between this delete's issuer
// tombstone and its orphan scan must still be observed. The fixture
// transaction holds the client-row lock the cascade takes, so the service
// delete deterministically blocks until the sibling is already dead — the
// exact schedule the FOR UPDATE lock exists to serialize.
func TestDeleteUserSessionIssuer_ConcurrentSiblingDeleteStillRevokes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-race-issuer")
	siblingID := createIssuerForOrphanTest(t, ctx, ti, "orphan-race-sibling")
	clientID, refreshToken := seedRevocableUpstream(t, ctx, ti, issuerID, "orphan-race", upstream.URL+"/revoke")

	require.NoError(t, remotesessionsrepo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   siblingID,
	}))

	tx, err := ti.conn.Begin(ctx) //nolint:glint // the raw-SQL rule catches tx.Exec with a query string; this transaction only ever runs SQLc-generated methods, and it exists to hold the client-row lock the cascade must wait on
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	// Hold the client-row lock before the service delete starts, so it cannot
	// scan for orphans until the sibling's delete below has committed.
	_, err = remotesessionsrepo.New(tx).LockRemoteSessionClientsBoundToUserSessionIssuer(ctx, siblingID)
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

	// The sibling's delete, replayed inside the lock-holding transaction:
	// tombstone the issuer and drop its bindings, then commit.
	txIssuers := usersessionsrepo.New(tx)
	_, err = txIssuers.DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{
		ID:        siblingID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.NoError(t, txIssuers.DeleteRemoteSessionClientAttachmentsForUserSessionIssuer(ctx, usersessionsrepo.DeleteRemoteSessionClientAttachmentsForUserSessionIssuerParams{
		UserSessionIssuerID: siblingID,
		ProjectID:           *authCtx.ProjectID,
	}))
	require.NoError(t, tx.Commit(ctx))

	require.NoError(t, <-deleteErr)

	require.Zero(t, countActiveClientSessions(t, ctx, ti, clientID), "the surviving delete observes the committed sibling delete and revokes")

	calls, tokens := spy.snapshot()
	require.Equal(t, 1, calls, "exactly one revocation despite the interleaved deletes")
	require.Equal(t, []string{refreshToken}, tokens)
}
