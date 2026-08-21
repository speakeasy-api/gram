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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// orphanRevocationSpy records what the fake upstream's /revoke received; the
// handler runs on its own goroutine, hence the mutex.
type orphanRevocationSpy struct {
	mu     sync.Mutex
	calls  int
	tokens []string
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
		spy.mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

// seedRevocableUpstream seeds a remote issuer advertising revocationEndpoint, a
// client bound to userSessionIssuerID, and a session with real ciphertext;
// returns the client id and the plaintext refresh token the revoker should send.
func seedRevocableUpstream(t *testing.T, ctx context.Context, ti *testInstance, userSessionIssuerID uuid.UUID, slug string, revocationEndpoint string) (clientID uuid.UUID, refreshToken string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
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

	client, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
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

	accessEnc, err := enc.Encrypt([]byte(slug + "-access"))
	require.NoError(t, err)
	refreshToken = slug + "-refresh"
	refreshEnc, err := enc.Encrypt([]byte(refreshToken))
	require.NoError(t, err)

	_, err = q.UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
		SubjectUrn:             urn.NewUserSubject("orphan-subject-" + slug),
		UserSessionIssuerID:    userSessionIssuerID,
		RemoteSessionClientID:  client.ID,
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

	return client.ID, refreshToken
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

// Deleting an issuer that holds a client's only binding tombstones the client's
// sessions and pushes an RFC 7009 revocation for each.
func TestDeleteUserSessionIssuer_RevokesOrphanedClientSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	spy := &orphanRevocationSpy{}
	upstream := newOrphanRevocationUpstream(t, spy)

	issuerID := createIssuerForOrphanTest(t, ctx, ti, "orphan-sole-issuer")
	clientID, refreshToken := seedRevocableUpstream(t, ctx, ti, issuerID, "orphan-sole", upstream.URL+"/revoke")

	err := ti.service.DeleteUserSessionIssuer(ctx, &issuersgen.DeleteUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               issuerID.String(),
	})
	require.NoError(t, err)

	active, err := remotesessionsrepo.New(ti.conn).CountActiveRemoteSessionsByClientID(ctx, clientID)
	require.NoError(t, err)
	require.Zero(t, active, "the orphaned client's sessions are tombstoned with the issuer")

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

	err := ti.service.DeleteUserSessionIssuer(ctx, &issuersgen.DeleteUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               issuerID.String(),
	})
	require.NoError(t, err)

	active, err := remotesessionsrepo.New(ti.conn).CountActiveRemoteSessionsByClientID(ctx, clientID)
	require.NoError(t, err)
	require.EqualValues(t, 1, active, "sessions on a client with a live sibling binding stay live")

	calls, _ := spy.snapshot()
	require.Zero(t, calls, "no upstream revocation for a still-reachable grant")

	// Deleting the sibling removes the client's last live binding, so the
	// cascade must catch the grant it spared above.
	err = ti.service.DeleteUserSessionIssuer(ctx, &issuersgen.DeleteUserSessionIssuerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               siblingID.String(),
	})
	require.NoError(t, err)

	active, err = remotesessionsrepo.New(ti.conn).CountActiveRemoteSessionsByClientID(ctx, clientID)
	require.NoError(t, err)
	require.Zero(t, active, "the last binding's deletion tombstones the spared sessions")

	_, tokens := spy.snapshot()
	require.Equal(t, []string{refreshToken}, tokens, "the spared grant is revoked when its last binding goes")
}
