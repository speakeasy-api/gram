package identityproviderconnections

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

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

func (c *stubClient) ListAppUsers(context.Context, okta.ListAppUsersRequest) ([]okta.AppUser, error) {
	return nil, c.failing["ListAppUsers"]
}

func (c *stubClient) ListGroups(context.Context, okta.ListGroupsRequest) ([]okta.Group, error) {
	return nil, c.failing["ListGroups"]
}

func TestVerifyConnection_CollectsEveryReason(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		Client:       nil,
		verification: &okta.ScopeVerification{Granted: []string{"okta.apps.read", "okta.groups.read"}, Missing: []string{"okta.users.read"}, DPoPBound: false},
		failing:      map[string]error{"ListGroups": &okta.APIError{StatusCode: http.StatusForbidden, ErrorCode: "E0000006"}},
	}
	outcome, err := verifyConnection(t.Context(), client)
	require.NoError(t, err)
	require.Equal(t, StatusDegraded, outcome.Status)
	require.Equal(t, []string{ReasonMissingScope, ReasonDPoPNotBound, ReasonReadFailedGroup}, outcome.Reasons)
	require.Equal(t, "missing_scope,dpop_not_bound,read_failed:okta.groups.read", outcome.lastError())
}

func TestVerifyConnection_CredentialRejectionDuringReadsIsAnError(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		Client:       nil,
		verification: &okta.ScopeVerification{Granted: []string{"okta.apps.read"}, Missing: nil, DPoPBound: true},
		failing:      map[string]error{"ListApps": &okta.APIError{StatusCode: http.StatusUnauthorized, ErrorCode: "invalid_client"}},
	}
	_, err := verifyConnection(t.Context(), client)
	require.ErrorIs(t, err, ErrCredentialRejected)
}

func TestParseLastError(t *testing.T) {
	t.Parallel()

	reasons, failure := parseLastError("credential_rejected,key_not_fetched")
	require.Equal(t, []string{ReasonKeyNotFetched}, reasons)
	require.Equal(t, LastErrorCredentialRejected, failure)

	reasons, failure = parseLastError("missing_scope,read_failed:okta.apps.read,bogus")
	require.Equal(t, []string{ReasonMissingScope, ReasonReadFailedApps}, reasons)
	require.Empty(t, failure)

	reasons, failure = parseLastError("")
	require.Empty(t, reasons)
	require.Empty(t, failure)

	require.Equal(t, LastErrorOktaUnreachable, failureLastError(errors.New("dial tcp")))
}
