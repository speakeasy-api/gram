package mcp_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// runIDPCallback seeds an in-flight challenge for a private toolset and
// drives the IDP callback through it with a successful code exchange.
func runIDPCallback(t *testing.T, mock *mockIdentityResolver) (context.Context, *testInstance, string, *httptest.ResponseRecorder, error) {
	t.Helper()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	challengeID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.EndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
		},
		ClientID:            "test-client",
		RedirectURI:         "http://localhost:3000/callback",
		State:               "client-state",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	}))

	q := url.Values{"state": {challengeID}, "code": {"idp-auth-code"}}
	req := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?"+q.Encode(), nil).WithContext(ctx)
	w := httptest.NewRecorder()
	if err := ti.service.HandleIDPCallback(w, req); err != nil {
		return ctx, ti, challengeID, w, fmt.Errorf("handle idp callback: %w", err)
	}
	return ctx, ti, challengeID, w, nil
}

func memberMock() *mockIdentityResolver {
	return &mockIdentityResolver{
		exchangeResult: &identity.IDPUserInfo{Sub: "workos-user-member", Email: "member@example.com", Name: "Member"},
		upsertResult:   "user-" + uuid.New().String()[:8],
		hasAccessOK:    true,
	}
}

func TestHandleIDPCallback_SyncsMembershipsBeforeAccessCheck(t *testing.T) {
	t.Parallel()

	mock := memberMock()
	_, _, _, w, err := runIDPCallback(t, mock)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, w.Code)

	require.Equal(t, []string{"CompleteIDPLogin", "IsOrganizationMember"}, mock.calls)
	require.Equal(t, []identity.IDPLoginOptions{{SkipMembershipSync: false}}, mock.loginOptions)
	require.Equal(t, []string{mock.upsertResult}, mock.memberChecks, "the gate must judge the user the bootstrap resolved")
}

func TestHandleIDPCallback_MembershipLookupFailureFailsClosed(t *testing.T) {
	t.Parallel()

	mock := memberMock()
	mock.hasAccessErr = errors.New("membership database unavailable")
	_, _, _, w, err := runIDPCallback(t, mock)
	require.Error(t, err)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnexpected, shareable.Code)
	require.Equal(t, []string{"CompleteIDPLogin", "IsOrganizationMember"}, mock.calls)
	require.Equal(t, []string{mock.upsertResult}, mock.memberChecks)
	require.Empty(t, w.Header().Get("Location"))
}

func TestHandleIDPCallback_BootstrapFailureFailsClosed(t *testing.T) {
	t.Parallel()

	mock := memberMock()
	mock.upsertErr = errors.New("workos membership listing unavailable")
	_, _, _, w, err := runIDPCallback(t, mock)
	require.Error(t, err)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnexpected, shareable.Code)
	require.Equal(t, []string{"CompleteIDPLogin"}, mock.calls, "membership must not be checked on unverified data")
	require.Empty(t, w.Header().Get("Location"))
}
