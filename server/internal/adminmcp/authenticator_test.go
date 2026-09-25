package adminmcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	staffIssuer   = "https://staff.example.test/admin-mcp/oauth"
	staffAudience = "https://staff.example.test/admin-mcp"
	staffClient   = "staff-client"
	staffSubject  = "staff-subject"
)

type fakeStaffAccessStore struct {
	session staffAccessSession
	err     error
	calls   int
}

func (s *fakeStaffAccessStore) ActiveSession(_ context.Context, _ string) (staffAccessSession, error) {
	s.calls++
	return s.session, s.err
}

type fakeAdminVerifier struct {
	result *contextvalues.AdminAuthContext
	err    error
	calls  int
	key    string
}

func (v *fakeAdminVerifier) Authorize(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	v.calls++
	v.key = key
	if key == "" || scheme == nil || scheme.Name != constants.AdminAuthSecurityScheme {
		return ctx, errors.New("invalid admin verification input")
	}
	if v.err != nil {
		return ctx, v.err
	}
	return contextvalues.SetAdminAuthContext(ctx, v.result), nil
}

func staffAuthFixture(t *testing.T) (*StaffAuthenticator, *fakeStaffAccessStore, *fakeAdminVerifier, string) {
	t.Helper()
	signer := sessiontokens.NewSigner("staff-test-signing-key")
	cipher, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	linkedSession, err := cipher.Encrypt([]byte("linked-browser-session"))
	require.NoError(t, err)
	subject := urn.NewUserSubject(staffSubject)
	token, jti, err := signer.Mint(sessiontokens.MintParams{
		Subject: subject, Audience: staffAudience, Issuer: staffIssuer, Lifetime: time.Hour, ClientID: staffClient,
	})
	require.NoError(t, err)
	store := &fakeStaffAccessStore{session: staffAccessSession{
		Subject: subject.String(), ClientID: staffClient, ConnectionID: "connection-1",
		Generation: "generation-1", ActiveGeneration: "generation-1", ResourceURI: staffAudience,
		Scopes: []string{"admin:read"}, AdminSessionEnc: linkedSession, ExpiresAt: time.Now().Add(time.Hour),
	}}
	verifier := &fakeAdminVerifier{result: &contextvalues.AdminAuthContext{
		SessionID: "linked-browser-session", OIDCSubject: staffSubject, Email: "staff@example.test",
	}}
	auth := &StaffAuthenticator{signer: signer, store: store, cipher: cipher, verifier: verifier, issuer: staffIssuer, audience: staffAudience}
	require.NotEmpty(t, jti)
	return auth, store, verifier, token
}

func TestStaffAuthenticatorRequiresLiveMatchingStaff(t *testing.T) {
	t.Parallel()
	auth, store, verifier, token := staffAuthFixture(t)
	principal, err := auth.Authenticate(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, Principal{Subject: "user:" + staffSubject, Email: "staff@example.test", ClientID: staffClient, ConnectionID: "connection-1", Scopes: []string{"admin:read"}}, principal)
	require.Equal(t, "linked-browser-session", verifier.key)
	require.Equal(t, 1, verifier.calls)
	require.Equal(t, 1, store.calls)
}

func TestStaffAuthenticatorRejectsInvalidTokenBeforeDatabase(t *testing.T) {
	t.Parallel()
	auth, store, verifier, token := staffAuthFixture(t)
	for _, presented := range []string{"", "not-a-token", token + "tampered"} {
		_, err := auth.Authenticate(t.Context(), presented)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrAuthUnavailable)
	}
	foreign, _, err := sessiontokens.NewSigner("different-key").Mint(sessiontokens.MintParams{
		Subject: urn.NewUserSubject(staffSubject), Audience: staffAudience, Issuer: staffIssuer, Lifetime: time.Hour, ClientID: staffClient,
	})
	require.NoError(t, err)
	_, err = auth.Authenticate(t.Context(), foreign)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAuthUnavailable)
	foreign, _, err = auth.signer.Mint(sessiontokens.MintParams{
		Subject: urn.NewUserSubject(staffSubject), Audience: staffAudience, Issuer: "https://other.example.test/admin-mcp/oauth", Lifetime: time.Hour, ClientID: staffClient,
	})
	require.NoError(t, err)
	_, err = auth.Authenticate(t.Context(), foreign)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAuthUnavailable)
	require.Zero(t, store.calls)
	require.Zero(t, verifier.calls)
}

func TestStaffAuthenticatorRejectsConnectionAndIdentityMismatch(t *testing.T) {
	t.Parallel()
	for _, modify := range []func(*StaffAuthenticator, *fakeStaffAccessStore, *fakeAdminVerifier){
		func(_ *StaffAuthenticator, s *fakeStaffAccessStore, _ *fakeAdminVerifier) {
			s.session.ClientID = "other-client"
		},
		func(_ *StaffAuthenticator, s *fakeStaffAccessStore, _ *fakeAdminVerifier) {
			s.session.Generation = "stale-generation"
		},
		func(_ *StaffAuthenticator, s *fakeStaffAccessStore, _ *fakeAdminVerifier) {
			s.session.ResourceURI = "https://other.example.test"
		},
		func(_ *StaffAuthenticator, s *fakeStaffAccessStore, _ *fakeAdminVerifier) {
			s.session.Scopes = []string{"admin:write"}
		},
		func(_ *StaffAuthenticator, s *fakeStaffAccessStore, _ *fakeAdminVerifier) {
			s.session.ExpiresAt = time.Now().Add(-time.Second)
		},
		func(_ *StaffAuthenticator, s *fakeStaffAccessStore, _ *fakeAdminVerifier) {
			s.session.AdminSessionEnc = "invalid"
		},
		func(_ *StaffAuthenticator, _ *fakeStaffAccessStore, v *fakeAdminVerifier) {
			v.result.OIDCSubject = "other-staff"
		},
		func(_ *StaffAuthenticator, _ *fakeStaffAccessStore, v *fakeAdminVerifier) {
			v.result.SessionID = "other-session"
		},
	} {
		auth, store, verifier, token := staffAuthFixture(t)
		modify(auth, store, verifier)
		_, err := auth.Authenticate(t.Context(), token)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrAuthUnavailable)
	}
}

func TestStaffAuthenticatorFailsClosedOnDependencyErrors(t *testing.T) {
	t.Parallel()
	auth, store, verifier, token := staffAuthFixture(t)
	store.err = pgx.ErrNoRows
	_, err := auth.Authenticate(t.Context(), token)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAuthUnavailable)
	store.err = errors.New("database down")
	_, err = auth.Authenticate(t.Context(), token)
	require.ErrorIs(t, err, ErrAuthUnavailable)
	store.err = nil
	verifier.err = oops.C(oops.CodeUnauthorized)
	_, err = auth.Authenticate(t.Context(), token)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAuthUnavailable)
	verifier.err = errors.New("Google unavailable")
	_, err = auth.Authenticate(t.Context(), token)
	require.ErrorIs(t, err, ErrAuthUnavailable)
	verifier.err = nil
	auth.cipher = nil
	_, err = auth.Authenticate(t.Context(), token)
	require.ErrorIs(t, err, ErrAuthUnavailable)
}
