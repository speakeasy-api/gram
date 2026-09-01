package netingress

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

type fakeWorkloadVerifier struct {
	ingress Ingress
	err     error
	token   string
	source  string
}

func (f *fakeWorkloadVerifier) Verify(_ context.Context, token, source string) (Ingress, error) {
	f.token = token
	f.source = source
	return f.ingress, f.err
}

func TestMiddlewareStampsPrivateOriginAndStripsAttestation(t *testing.T) {
	t.Parallel()

	ingressID := uuid.New()
	verifier := &fakeWorkloadVerifier{ingress: Ingress{
		ID: ingressID, OrganizationID: "org_123", Provider: ProviderTailscale,
		DNSName: "private.example.ts.net", IdentityRequired: true,
	}}
	var gotOrigin requestorigin.Origin
	var gotOriginOK bool
	var gotHeaders http.Header
	next := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotOrigin, gotOriginOK = requestorigin.FromContext(request.Context())
		gotHeaders = request.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	})
	handler := Middleware(verifier, IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}})(next)

	request := httptest.NewRequest(http.MethodPost, "http://private.example.ts.net/mcp/server", nil)
	request.Host = "PRIVATE.EXAMPLE.TS.NET:443"
	request.Header.Set(AttestationHeader, "bEaReR projected-token")
	request.Header.Set("Authorization", "Bearer mcp-token")
	request.Header.Set(TailscaleUserLoginHeader, "user@example.com")
	request.Header.Set(TailscaleUserNameHeader, "Example User")
	request.Header.Set("Tailscale-App-Capabilities", "unsupported")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, "projected-token", verifier.token)
	require.Equal(t, "192.0.2.1", verifier.source)
	require.True(t, gotOriginOK)
	require.Empty(t, gotHeaders.Get(AttestationHeader))
	require.Equal(t, "Bearer mcp-token", gotHeaders.Get("Authorization"))
	require.Empty(t, gotHeaders.Get("Tailscale-App-Capabilities"))
	require.Empty(t, gotHeaders.Get(TailscaleUserLoginHeader))
	require.Empty(t, gotHeaders.Get(TailscaleUserNameHeader))
	require.Empty(t, gotHeaders.Get(TailscaleUserProfilePicHeader))
	require.Equal(t, requestorigin.SurfacePrivateNetwork, gotOrigin.Surface)
	require.Equal(t, "https://private.example.ts.net", gotOrigin.BaseURL)
	require.Equal(t, "org_123", gotOrigin.OrganizationID)
	require.Equal(t, ingressID, gotOrigin.NetworkIngressID)
	require.Equal(t, &requestorigin.NetworkIdentity{Login: "user@example.com", Name: "Example User"}, gotOrigin.NetworkIdentity)
}

func TestMiddlewareAllowsTaggedNodeWhenIdentityOptional(t *testing.T) {
	t.Parallel()

	verifier := &fakeWorkloadVerifier{ingress: Ingress{
		ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale,
		DNSName: "private.example.ts.net", IdentityRequired: false,
	}}
	var gotOrigin requestorigin.Origin
	var gotOriginOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotOrigin, gotOriginOK = requestorigin.FromContext(request.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	request := privateRequest()
	response := httptest.NewRecorder()

	Middleware(verifier, IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}})(next).ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.True(t, gotOriginOK)
	require.Nil(t, gotOrigin.NetworkIdentity)
}

func TestMiddlewareFailsClosed(t *testing.T) {
	t.Parallel()

	baseIngress := Ingress{
		ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale,
		DNSName: "private.example.ts.net",
	}
	for _, test := range []struct {
		name       string
		prepare    func(*http.Request)
		verifier   *fakeWorkloadVerifier
		parsers    IdentityParsers
		wantStatus int
	}{
		{
			name: "missing attestation", prepare: func(request *http.Request) { request.Header.Del(AttestationHeader) },
			verifier: &fakeWorkloadVerifier{ingress: baseIngress}, parsers: IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}}, wantStatus: http.StatusUnauthorized,
		},
		{
			name: "duplicate attestation", prepare: func(request *http.Request) { request.Header.Add(AttestationHeader, "Bearer other") },
			verifier: &fakeWorkloadVerifier{ingress: baseIngress}, parsers: IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}}, wantStatus: http.StatusUnauthorized,
		},
		{
			name: "invalid token", verifier: &fakeWorkloadVerifier{err: ErrAttestationRejected},
			parsers: IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}}, wantStatus: http.StatusUnauthorized,
		},
		{
			name: "token review unavailable", verifier: &fakeWorkloadVerifier{err: errors.New("api unavailable")},
			parsers: IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}}, wantStatus: http.StatusServiceUnavailable,
		},
		{
			name: "host mismatch", prepare: func(request *http.Request) { request.Host = "other.example.ts.net" },
			verifier: &fakeWorkloadVerifier{ingress: baseIngress}, parsers: IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}}, wantStatus: http.StatusNotFound,
		},
		{
			name: "unsupported provider", verifier: &fakeWorkloadVerifier{ingress: Ingress{
				ID: uuid.New(), OrganizationID: "org_123", Provider: "unknown", DNSName: "private.example.ts.net",
			}}, parsers: IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}}, wantStatus: http.StatusServiceUnavailable,
		},
		{
			name: "identity required", verifier: &fakeWorkloadVerifier{ingress: Ingress{
				ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale,
				DNSName: "private.example.ts.net", IdentityRequired: true,
			}}, parsers: IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}}, wantStatus: http.StatusUnauthorized,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := privateRequest()
			if test.prepare != nil {
				test.prepare(request)
			}
			response := httptest.NewRecorder()
			nextCalled := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true })

			Middleware(test.verifier, test.parsers)(next).ServeHTTP(response, request)

			require.Equal(t, test.wantStatus, response.Code)
			require.False(t, nextCalled)
			require.NotContains(t, response.Body.String(), "projected-token")
		})
	}
}

func privateRequest() *http.Request {
	request := httptest.NewRequest(http.MethodPost, "http://private.example.ts.net/mcp/server", nil)
	request.RemoteAddr = "192.0.2.1:4321"
	request.Host = "private.example.ts.net"
	request.Header.Set(AttestationHeader, "Bearer projected-token")
	return request
}
