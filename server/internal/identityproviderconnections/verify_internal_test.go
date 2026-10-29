package identityproviderconnections

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

// stubClient answers VerifyScopes from a fixed verification and fails the
// reads listed in failing; every other read succeeds with one app.
type stubClient struct {
	okta.Client

	verification *okta.ScopeVerification
	failing      map[string]error
}

func (c *stubClient) VerifyScopes(context.Context, []string) (*okta.ScopeVerification, error) {
	return c.verification, nil
}

func (c *stubClient) ListApps(context.Context, okta.ListAppsRequest) ([]okta.App, error) {
	if err := c.failing["ListApps"]; err != nil {
		return nil, err
	}
	return []okta.App{{ID: "0oaapp00000000000001"}}, nil
}

func TestVerifyConnection_CollectsEveryReason(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		Client:       nil,
		verification: &okta.ScopeVerification{Granted: []string{"okta.apps.read"}, Missing: []string{"okta.example.read"}, DPoPBound: false},
		failing:      map[string]error{"ListApps": &okta.APIError{StatusCode: http.StatusForbidden, ErrorCode: "E0000006"}},
	}
	outcome, err := verifyConnection(t.Context(), client, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, false)
	require.NoError(t, err)
	require.Equal(t, StatusDegraded, outcome.Status)
	require.Equal(t, []string{ReasonMissingScope, ReasonDPoPNotBound, ReasonReadFailedApps, ReasonMissingRole}, outcome.Reasons)
	require.Equal(t, "missing_scope,dpop_not_bound,read_failed:okta.apps.read,missing_role", outcome.lastError())
}

func TestVerifyConnection_CredentialRejectionDuringReadsIsAnError(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		Client:       nil,
		verification: &okta.ScopeVerification{Granted: []string{"okta.apps.read"}, Missing: nil, DPoPBound: true},
		failing:      map[string]error{"ListApps": &okta.APIError{StatusCode: http.StatusUnauthorized, ErrorCode: "invalid_client"}},
	}
	_, err := verifyConnection(t.Context(), client, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, false)
	require.ErrorIs(t, err, ErrCredentialRejected)
}

func TestVerifyConnection_PageCapIsNotAFailure(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		Client:       nil,
		verification: &okta.ScopeVerification{Granted: []string{"okta.apps.read"}, Missing: nil, DPoPBound: true},
		failing:      map[string]error{"ListApps": okta.ErrTooManyPages},
	}
	outcome, err := verifyConnection(t.Context(), client, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, false)
	require.NoError(t, err)
	require.Equal(t, StatusVerified, outcome.Status)
	require.Empty(t, outcome.Reasons)
}

func TestVerifyConnection_ManagementUnauthorizedIsAReadFailure(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		Client:       nil,
		verification: &okta.ScopeVerification{Granted: []string{"okta.apps.read"}, Missing: nil, DPoPBound: true},
		failing:      map[string]error{"ListApps": &okta.APIError{StatusCode: http.StatusUnauthorized, ErrorCode: "E0000011"}},
	}
	outcome, err := verifyConnection(t.Context(), client, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, false)
	require.NoError(t, err)
	require.Equal(t, []string{ReasonReadFailedApps}, outcome.Reasons)
}

func TestParseLastError(t *testing.T) {
	t.Parallel()

	reasons, failure := parseLastError("credential_rejected,key_not_fetched")
	require.Equal(t, []string{ReasonKeyNotFetched}, reasons)
	require.Equal(t, LastErrorCredentialRejected, failure)

	reasons, failure = parseLastError("missing_scope,read_failed:okta.apps.read,bogus")
	require.Equal(t, []string{ReasonMissingScope, ReasonReadFailedApps}, reasons)
	require.Empty(t, failure)

	// Stored before the users and groups reads were retired.
	reasons, failure = parseLastError("read_failed:okta.users.read,read_failed:okta.groups.read")
	require.Equal(t, []string{ReasonReadFailedUsers, ReasonReadFailedGroup}, reasons)
	require.Empty(t, failure)

	reasons, failure = parseLastError("")
	require.Empty(t, reasons)
	require.Empty(t, failure)

	require.Equal(t, LastErrorOktaUnreachable, failureLastError(errors.New("dial tcp"), remotesessions.TokenEndpointAuthMethodPrivateKeyJWT))
}

func TestVerifyConnection_ForbiddenReadsReportMissingRoleOnce(t *testing.T) {
	t.Parallel()
	client := &stubClient{
		verification: &okta.ScopeVerification{Granted: RequiredOktaScopes, DPoPBound: true},
		failing:      map[string]error{},
	}
	client.failing["ListApps"] = &okta.APIError{StatusCode: http.StatusForbidden, ErrorCode: "E0000006"}
	outcome, err := verifyConnection(t.Context(), client, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, false)
	require.NoError(t, err)
	require.Equal(t, []string{ReasonReadFailedApps, ReasonMissingRole}, outcome.Reasons)
	reasons, failure := parseLastError(outcome.lastError())
	require.Equal(t, outcome.Reasons, reasons)
	require.Empty(t, failure)
}

func TestVerifyConnection_IgnoresUnrequiredGrants(t *testing.T) {
	t.Parallel()
	client := okta.NewFake(okta.Fixtures{GrantedScopes: []string{"okta.apps.read", "okta.users.read", "okta.groups.read"}})
	outcome, err := verifyConnection(t.Context(), client, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, false)
	require.NoError(t, err)
	require.Equal(t, []string{"okta.apps.read"}, outcome.Granted)
	require.Equal(t, []string{"VerifyScopes", "ListApps"}, client.Calls())
}

func TestVerifyConnection_CredentialProofRequiresMintedToken(t *testing.T) {
	t.Parallel()
	for _, granted := range [][]string{nil, {"okta.apps.read"}} {
		client := okta.NewFake(okta.Fixtures{GrantedScopes: granted})
		outcome, err := verifyConnection(t.Context(), client, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, false)
		require.NoError(t, err)
		require.Equal(t, len(granted) > 0, outcome.CredentialProven)
	}
}

func TestVerifyConnection_BasicAcceptsBearerToken(t *testing.T) {
	t.Parallel()
	client := &stubClient{
		Client:       nil,
		verification: &okta.ScopeVerification{Granted: RequiredOktaScopes, Missing: nil, DPoPBound: false},
		failing:      map[string]error{},
	}
	outcome, err := verifyConnection(t.Context(), client, remotesessions.TokenEndpointAuthMethodBasic, false)
	require.NoError(t, err)
	require.Equal(t, StatusVerified, outcome.Status)
	require.Empty(t, outcome.Reasons)
}

func TestVerifyConnection_BasicPinnedRefusesBearerToken(t *testing.T) {
	t.Parallel()
	readErr := &okta.APIError{StatusCode: http.StatusForbidden, ErrorCode: "E0000006"}
	client := &stubClient{
		Client:       nil,
		verification: &okta.ScopeVerification{Granted: RequiredOktaScopes, Missing: nil, DPoPBound: false},
		failing:      map[string]error{"ListApps": readErr},
	}
	outcome, err := verifyConnection(t.Context(), client, remotesessions.TokenEndpointAuthMethodBasic, true)
	require.NoError(t, err)
	require.Equal(t, StatusDegraded, outcome.Status)
	require.Equal(t, []string{ReasonDPoPNotBound}, outcome.Reasons, "reads are skipped once the unbound token is refused")
}

func TestVerifyConnection_PinnedUnboundKeepsMissingScope(t *testing.T) {
	t.Parallel()
	readErr := &okta.APIError{StatusCode: http.StatusForbidden, ErrorCode: "E0000006"}
	client := &stubClient{
		Client:       nil,
		verification: &okta.ScopeVerification{Granted: []string{"okta.apps.read"}, Missing: []string{"okta.example.read"}, DPoPBound: false},
		failing:      map[string]error{"ListApps": readErr},
	}
	outcome, err := verifyConnection(t.Context(), client, remotesessions.TokenEndpointAuthMethodBasic, true)
	require.NoError(t, err)
	require.Equal(t, StatusDegraded, outcome.Status)
	require.Equal(t, []string{ReasonMissingScope, ReasonDPoPNotBound}, outcome.Reasons)
}

func TestFailureLastError_CredentialRejectedByMethod(t *testing.T) {
	t.Parallel()
	require.Equal(t, "credential_rejected,secret_rejected", failureLastError(ErrCredentialRejected, remotesessions.TokenEndpointAuthMethodBasic))
	require.Equal(t, "credential_rejected,key_not_fetched", failureLastError(ErrCredentialRejected, remotesessions.TokenEndpointAuthMethodPrivateKeyJWT))
	require.Equal(t, LastErrorOktaUnreachable, failureLastError(errors.New("dial tcp"), remotesessions.TokenEndpointAuthMethodBasic))
}
