// tokenservice_credentials_internal_test.go verifies that client credentials
// are form-urlencoded before entering the Basic authorization header (RFC
// 6749 §2.3.1). White-box so the shared request builder is exercised directly.

package remotesessions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type recordingAssertionSigner struct {
	requests []ClientAssertionRequest
}

func (s *recordingAssertionSigner) SignClientAssertion(_ context.Context, request ClientAssertionRequest) (string, error) {
	s.requests = append(s.requests, request)
	return fmt.Sprintf("assertion-%d", len(s.requests)), nil
}

func TestNewTokenEndpointRequest_BasicAuthEncodesCredentials(t *testing.T) {
	t.Parallel()

	req, err := newTokenEndpointRequest(
		t.Context(),
		"https://idp.example.com/token",
		url.Values{},
		tokenEndpointClientAuth{
			Method:                TokenEndpointAuthMethodBasic,
			RemoteSessionClientID: uuid.Nil,
			OrganizationID:        "",
			JSONWebKeySetID:       uuid.Nil,
			ClientID:              "ab+cd/ef=",
			ClientSecret:          "s+e/c==",
			AssertionAudience:     "",
			AssertionSigner:       unavailableTokenEndpointAssertionSigner{},
		},
	)
	require.NoError(t, err)

	user, pass, ok := req.BasicAuth()
	require.True(t, ok)
	require.Equal(t, "ab%2Bcd%2Fef%3D", user, "client_id must be form-urlencoded per RFC 6749 §2.3.1")
	require.Equal(t, "s%2Be%2Fc%3D%3D", pass, "client_secret must be form-urlencoded per RFC 6749 §2.3.1")
}

func TestNewTokenEndpointRequest_PrivateKeyJWT(t *testing.T) {
	t.Parallel()

	clientRowID := uuid.New()
	keySetID := uuid.New()
	signer := &recordingAssertionSigner{requests: nil}
	auth := tokenEndpointClientAuth{
		Method:                TokenEndpointAuthMethodPrivateKeyJWT,
		RemoteSessionClientID: clientRowID,
		OrganizationID:        "org_123",
		JSONWebKeySetID:       keySetID,
		ClientID:              "oauth-client",
		ClientSecret:          "retained-but-unused",
		AssertionAudience:     "https://idp.example.com/",
		AssertionSigner:       signer,
	}

	first, err := newTokenEndpointRequest(t.Context(), "https://idp.example.com/token", url.Values{"client_secret": {"caller-seeded"}}, auth)
	require.NoError(t, err)
	second, err := newTokenEndpointRequest(t.Context(), "https://idp.example.com/token", url.Values{}, auth)
	require.NoError(t, err)

	require.Len(t, signer.requests, 2, "each HTTP attempt must mint its own assertion")
	require.Equal(t, ClientAssertionRequest{
		RemoteSessionClientID: clientRowID,
		OrganizationID:        "org_123",
		JSONWebKeySetID:       keySetID,
		ClientID:              "oauth-client",
		Audience:              "https://idp.example.com/",
	}, signer.requests[0])

	firstForm, err := url.ParseQuery(mustReadBody(t, first))
	require.NoError(t, err)
	require.Equal(t, "oauth-client", firstForm.Get("client_id"))
	require.Equal(t, clientAssertionType, firstForm.Get("client_assertion_type"))
	require.Equal(t, "assertion-1", firstForm.Get("client_assertion"))
	require.Empty(t, firstForm.Get("client_secret"))
	_, _, basic := first.BasicAuth()
	require.False(t, basic)

	secondForm, err := url.ParseQuery(mustReadBody(t, second))
	require.NoError(t, err)
	require.Equal(t, "assertion-2", secondForm.Get("client_assertion"))
}

func mustReadBody(t *testing.T, req *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return string(body)
}
