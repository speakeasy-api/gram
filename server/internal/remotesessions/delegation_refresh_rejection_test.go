package remotesessions

import (
	"bytes"
	"crypto/x509"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// refusalRetryWait is past the delegation service's retry delay, whose value is private to it.
const refusalRetryWait = 10 * time.Minute

// fakeTokenEndpoint answers each refresh with the current response and records
// the refresh token it was sent.
type fakeTokenEndpoint struct {
	mu        sync.Mutex
	status    int
	body      string
	submitted []string
}

func (e *fakeTokenEndpoint) answer(status int, body string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status, e.body = status, body
}

func (e *fakeTokenEndpoint) received() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.submitted...)
}

// rejectionFixture drives the real Postgres store and the real HTTP client
// against a fake provider. The clock starts refusalRetryWait in the past
// because the claim query checks retry_after against the database clock.
func rejectionFixture(t *testing.T) (*delegationAdapterFixture, *delegationAdapterStore, DelegationBinding, DelegationAuthorizer, *fakeTokenEndpoint, *bytes.Buffer) {
	t.Helper()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	endpoint := &fakeTokenEndpoint{}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read refresh request: %v", err)
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			t.Errorf("parse refresh request: %v", err)
		}
		endpoint.mu.Lock()
		defer endpoint.mu.Unlock()
		endpoint.submitted = append(endpoint.submitted, form.Get("refresh_token"))
		w.WriteHeader(endpoint.status)
		_, _ = io.WriteString(w, endpoint.body)
	}))
	t.Cleanup(upstream.Close)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)
	p.issuer.Issuer = upstream.URL
	p.metadata.Issuer = upstream.URL
	p.metadata.AuthorizationEndpoint = upstream.URL + "/authorize"
	p.metadata.TokenEndpoint = upstream.URL + "/token"
	p.metadata.JwksURI = upstream.URL + "/jwks"
	p.client.ClientSecretEncrypted.String, err = s.enc.Encrypt([]byte("secret-client"))
	require.NoError(t, err)
	logs := &bytes.Buffer{}
	manager := &ChallengeManager{enc: s.enc, policy: policy, logger: slog.New(slog.NewJSONHandler(logs, nil))}
	s.refreshIdentity = manager.RefreshFederatedIdentity
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-refusalRetryWait)
	s.now = func() time.Time { return start }
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, start, "secret-id", "secret-refresh", 30*time.Second), true))
	return s, store, b, allow, endpoint, logs
}

func TestDelegationUnexpectedRejectionKeepsTokenUntilProviderRecovers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "invalid client", status: http.StatusUnauthorized, body: `{"error":"invalid_client","error_description":"secret-upstream"}`},
		{name: "unauthorized client", status: http.StatusBadRequest, body: `{"error":"unauthorized_client","error_description":"secret-upstream"}`},
		{name: "html forbidden", status: http.StatusForbidden, body: `<html>secret-upstream</html>`},
		{name: "endpoint not found", status: http.StatusNotFound, body: ``},
		{name: "unknown code", status: http.StatusBadRequest, body: `{"error":"secret-upstream"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, b, allow, endpoint, logs := rejectionFixture(t)
			endpoint.answer(tc.status, tc.body)

			_, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationConfiguration)
			row, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, "configuration_failure", row.ObservationStatus.String)
			require.Equal(t, "secret-refresh", delegationPlain(t, s, row.RefreshTokenEncrypted.String))
			require.False(t, row.RefreshClaimID.Valid)

			_, err = s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationConfiguration)
			require.Len(t, endpoint.received(), 1, "the pause must hold without another provider request")

			endpoint.answer(http.StatusOK, `{"access_token":"secret-access","refresh_token":"secret-rotated"}`)
			recovered := s.now().Add(refusalRetryWait)
			s.now = func() time.Time { return recovered }
			_, err = s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationReauthentication, "this response has no ID token to renew the assertion with")
			require.Equal(t, []string{"secret-refresh", "secret-refresh"}, endpoint.received())
			row, err = store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, "secret-rotated", delegationPlain(t, s, row.RefreshTokenEncrypted.String))
			require.NotEqual(t, "configuration_failure", row.ObservationStatus.String)

			require.Contains(t, logs.String(), "federated refresh rejected")
			require.NotContains(t, logs.String(), "secret-")
		})
	}
}

func TestDelegationInvalidGrantClearsSavedToken(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "standard", body: `{"error":"invalid_grant","error_description":"secret-upstream"}`},
		{name: "vendor envelope", body: `{"errors":["invalid_grant - secret-upstream"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, b, allow, endpoint, logs := rejectionFixture(t)
			endpoint.answer(http.StatusBadRequest, tc.body)

			_, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationReauthentication)
			row, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Empty(t, row.RefreshTokenEncrypted.String)
			require.Equal(t, "reauthentication_required", row.ObservationStatus.String)

			later := s.now().Add(refusalRetryWait)
			s.now = func() time.Time { return later }
			_, err = s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationReauthentication)
			require.Len(t, endpoint.received(), 1)
			require.NotContains(t, logs.String(), "secret-")
		})
	}
}
