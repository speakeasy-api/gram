package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// seedRefreshableOrgSession seeds an org-scoped issuer (token endpoint pointed
// at handler), a client, and a session that holds an upstream refresh token but
// a still-valid access token — so a refresh exercises the "regardless of current
// expiry" path. enc must share the fixed test key with the service so the seeded
// refresh token decrypts.
func seedRefreshableOrgSession(t *testing.T, ctx context.Context, ti *testInstance, enc *encryption.Client, slug string, handler http.HandlerFunc) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	tokenServer := httptest.NewServer(handler)
	t.Cleanup(tokenServer.Close)

	q := repo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              slug,
		Issuer:                            tokenServer.URL,
		AuthorizationEndpoint:             conv.ToPGText(tokenServer.URL + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(tokenServer.URL + "/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
	})
	require.NoError(t, err)

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-"+slug)
	clientID := createRemoteClient(t, ctx, ti, issuer.ID.String(), userIssuerID.String(), "cid-"+slug)
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)

	accessEnc, err := enc.Encrypt([]byte("stale-access"))
	require.NoError(t, err)
	refreshEnc, err := enc.Encrypt([]byte("upstream-refresh"))
	require.NoError(t, err)
	refreshEncStr := refreshEnc

	row, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            urn.NewUserSubject("subject-" + slug),
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: conv.PtrToPGText(&refreshEncStr),
		RefreshExpiresAt:      pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		Scopes:                []string{},
	})
	require.NoError(t, err)
	return row.ID
}

// TestRefreshSession forces an upstream refresh on a session whose access token
// is still valid, persists the rotated tokens, returns the updated view, and
// records a refresh audit event attributed to the client's project.
func TestRefreshSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enc := testenv.NewEncryptionClient(t)

	// expires_in is 2h, well beyond the seeded session's 1h access expiry, so a
	// later expiry proves the refresh actually ran and persisted.
	sessionID := seedRefreshableOrgSession(t, ctx, ti, enc, "admin-refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"refreshed-access","token_type":"Bearer","expires_in":7200,"refresh_token":"refreshed-refresh"}`))
	})

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionRefresh)
	require.NoError(t, err)

	result, err := ti.service.RefreshSession(ctx, &orggen.RefreshSessionPayload{
		ID:           sessionID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.True(t, result.HasRefreshToken)

	gotExpiry, err := time.Parse(time.RFC3339, result.AccessExpiresAt)
	require.NoError(t, err)
	require.True(t, gotExpiry.After(time.Now().Add(90*time.Minute)), "access expiry should reflect the upstream's 2h expires_in, not the stale 1h")

	// The rotated access token was persisted (encrypted) for the binding.
	stored, err := repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            urn.NewUserSubject("subject-admin-refresh"),
		RemoteSessionClientID: uuid.MustParse(result.RemoteSessionClientID),
	})
	require.NoError(t, err)
	plain, err := enc.Decrypt(stored.AccessTokenEncrypted)
	require.NoError(t, err)
	require.Equal(t, "refreshed-access", plain)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionRefresh)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionRefresh)
	require.NoError(t, err)
	require.True(t, record.ProjectID.Valid)
	require.Equal(t, *authCtx.ProjectID, record.ProjectID.UUID)
}

// TestRefreshSession_NoRefreshToken refuses to refresh a session that holds no
// refresh token (defense in depth behind the UI gate) and records nothing.
func TestRefreshSession_NoRefreshToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-refresh-noref-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-refresh-noref-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-refresh-noref-client")
	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-refresh-noref-subject"), userIssuerID.String(), clientID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionRefresh)
	require.NoError(t, err)

	_, err = ti.service.RefreshSession(ctx, &orggen.RefreshSessionPayload{
		ID:           session.ID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionRefresh)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// TestRefreshSession_UnreadableRefreshToken surfaces an actionable, public-safe
// reason (not a generic error) when the stored refresh token cannot be
// decrypted, e.g. because it is corrupt.
func TestRefreshSession_UnreadableRefreshToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-refresh-corrupt-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-refresh-corrupt-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-refresh-corrupt-client")
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)

	// A non-empty value that is not valid ciphertext: passes the has-refresh-token
	// gate but fails to decrypt.
	bogus := "not-valid-ciphertext"
	session, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            urn.NewUserSubject("admin-refresh-corrupt-subject"),
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  "ciphertext",
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: conv.PtrToPGText(&bogus),
		RefreshExpiresAt:      pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		Scopes:                []string{},
	})
	require.NoError(t, err)

	_, err = ti.service.RefreshSession(ctx, &orggen.RefreshSessionPayload{
		ID:           session.ID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "Unable to refresh:")
}

// TestRefreshSession_UpstreamRejected maps an upstream rejection of the refresh
// token (non-2xx, e.g. invalid_grant) to an actionable bad-request, not an
// internal error.
func TestRefreshSession_UpstreamRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enc := testenv.NewEncryptionClient(t)

	sessionID := seedRefreshableOrgSession(t, ctx, ti, enc, "admin-refresh-reject", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	_, err := ti.service.RefreshSession(ctx, &orggen.RefreshSessionPayload{
		ID:           sessionID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestRefreshSession_RBACForbidden proves the refresh requires org:admin;
// org:read is insufficient.
func TestRefreshSession_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.RefreshSession(ctx, &orggen.RefreshSessionPayload{
		ID:           uuid.New().String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// TestRefreshSession_NotFound returns NotFound for an unknown session id.
func TestRefreshSession_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.RefreshSession(ctx, &orggen.RefreshSessionPayload{
		ID:           uuid.New().String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestRefreshSession_CrossOrgNotFound enforces org scoping: an admin acting in
// one organization cannot refresh a session that belongs to another. The
// org-scoped lookup filters it out, so it resolves to NotFound rather than a
// cross-tenant read.
func TestRefreshSession_CrossOrgNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Seed a session in the caller's original organization.
	issuerID := createRemoteIssuer(t, ctx, ti, "admin-refresh-xorg-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-refresh-xorg-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-refresh-xorg-client")
	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-refresh-xorg-subject"), userIssuerID.String(), clientID)

	// Re-bind the caller as an admin of a different organization.
	otherOrgID := createOrganization(t, ctx, ti.conn, "admin-refresh-xorg-other")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ActiveOrganizationID = otherOrgID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, otherOrgID),
	})

	// The session belongs to the original org, so this org's admin can't see it.
	_, err := ti.service.RefreshSession(ctx, &orggen.RefreshSessionPayload{
		ID:           session.ID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
