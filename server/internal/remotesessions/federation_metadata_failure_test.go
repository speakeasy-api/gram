package remotesessions

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestFederatedMetadataMalformedSuccessIsConfiguration(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"invalid JSON", "invalid scope type", "invalid endpoint"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			p := federatedFixture(t)
			encoded, err := json.Marshal(p.metadata)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &fields))
			switch kind {
			case "invalid scope type":
				fields["scopes_supported"] = json.RawMessage(`42`)
			case "invalid endpoint":
				fields["token_endpoint"] = json.RawMessage(`"http://idp.example.test/token"`)
			}
			encoded, err = json.Marshal(fields)
			require.NoError(t, err)
			if kind == "invalid JSON" {
				encoded = []byte(`{"issuer":`)
			}
			doer := federatedHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
			})
			_, probeErr := attemptIssuerProbe(t.Context(), doer, p.issuer.Issuer+"/.well-known/openid-configuration")
			require.ErrorIs(t, probeErr, errInvalidDiscoveryDocument)
			require.Equal(t, http.StatusOK, probeErr.Status)
			m := &ChallengeManager{policy: federatedPublicPolicy(t)}
			_, err = m.loadFederatedMetadata(t.Context(), p.organizationID, p.issuer, doer)
			require.ErrorIs(t, err, ErrFederatedConfiguration)
			require.NotErrorIs(t, err, ErrFederatedUnavailable)
		})
	}
}

func TestFederatedMetadataTransientResponseRemainsUnavailable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		status         int
		bodyError      error
		transportError error
	}{
		{name: "partial HTTP 200 body", status: http.StatusOK, bodyError: io.ErrUnexpectedEOF},
		{name: "HTTP 503", status: http.StatusServiceUnavailable},
		{name: "HTTP 502", status: http.StatusBadGateway},
		{name: "network timeout", transportError: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := federatedFixture(t)
			doer := federatedHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
				if tc.transportError != nil {
					return nil, tc.transportError
				}
				var body io.Reader = strings.NewReader(`{"error":"unavailable"}`)
				if tc.bodyError != nil {
					body = io.MultiReader(strings.NewReader(`{"issuer":`), iotest.ErrReader(tc.bodyError))
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(body)}, nil
			})
			_, probeErr := attemptIssuerProbe(t.Context(), doer, p.issuer.Issuer+"/.well-known/openid-configuration")
			require.Error(t, probeErr)
			require.NotErrorIs(t, probeErr, errInvalidDiscoveryDocument)
			m := &ChallengeManager{policy: federatedPublicPolicy(t)}
			_, err := m.loadFederatedMetadata(t.Context(), p.organizationID, p.issuer, doer)
			require.ErrorIs(t, err, ErrFederatedUnavailable)
			require.NotErrorIs(t, err, ErrFederatedConfiguration)
			if tc.bodyError != nil {
				require.ErrorIs(t, err, tc.bodyError)
			}
			if tc.transportError != nil {
				require.ErrorIs(t, err, tc.transportError)
			}
		})
	}
}

func TestFederatedMetadataTemporaryDNSWithoutTimeout(t *testing.T) {
	t.Parallel()
	cause := &net.DNSError{Err: "temporary resolver outage", Name: "idp.example.test", IsTemporary: true}
	require.False(t, cause.Timeout())
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) { return nil, cause },
	})))
	m := &ChallengeManager{policy: policy}
	err := m.validateFederatedHost(t.Context(), "https://idp.example.test", false)
	require.ErrorIs(t, err, ErrFederatedUnavailable)
	require.ErrorIs(t, err, cause)
	require.NotErrorIs(t, err, ErrFederatedConfiguration)
}
