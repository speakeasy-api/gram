package remotesessions

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/delegation"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

// These are production helper boundaries, NOT worker or persisted-grant E2E.
// newDelegationUnitFixture currently clones Postgres and writes fixtures, so it
// cannot be used here under the no-SQL-fixture constraint.
func TestAuthOperationalGapG11UnexpectedRejectionClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
		fatal  bool
	}{
		{"WAF403", 403, `<html>blocked by intermediary</html>`, false},
		{"invalid_client", 400, `{"error":"invalid_client"}`, false},
		{"moved_endpoint", 404, `endpoint moved`, false},
		{"invalid_grant_negative_control", 401, `{"error":"invalid_grant"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://issuer.example.test/token", nil)
			require.NoError(t, err)
			calls := 0
			_, _, err = postFederatedRefresh(federatedHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}), req)
			require.Equal(t, 1, calls)
			var classified *delegation.RefreshError
			require.ErrorAs(t, delegationRefreshError(err), &classified)
			if tc.fatal {
				require.Equal(t, delegation.RefreshInvalidGrant, classified.Kind)
				return
			}
			// These responses do not establish grant revocation. Configuration is
			// an appropriate admin-remediation category; it need not be retryable.
			// The separate memory Resolve tests investigate grant preservation,
			// not whether this classification itself is a defect. Explicit transient
			// responses exercise the retryable category in the recovery test below.
			require.NotEqual(t, delegation.RefreshInvalidGrant, classified.Kind)
			require.Equal(t, delegation.RefreshConfiguration, classified.Kind)
		})
	}
}

// This proves a retryable response followed by a successful protocol exchange,
// not Resolve/store preservation or the one-minute retry policy.
func TestAuthOperationalGapG11Transient403RecoveryProtocolBoundary(t *testing.T) {
	t.Parallel()
	calls := 0
	doer := federatedHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		calls++
		status, body := http.StatusForbidden, `{"error":"temporarily_unavailable"}`
		if calls == 2 {
			status, body = http.StatusOK, `{"access_token":"recovered","refresh_token":"rotated"}`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://issuer.example.test/token", nil)
	require.NoError(t, err)
	_, _, err = postFederatedRefresh(doer, req)
	var classified *delegation.RefreshError
	require.ErrorAs(t, delegationRefreshError(err), &classified)
	require.Equal(t, delegation.RefreshRetryable, classified.Kind)
	tok, receivedAt, err := postFederatedRefresh(doer, req.Clone(t.Context()))
	require.NoError(t, err)
	require.Equal(t, "recovered", tok.AccessToken)
	require.Equal(t, "rotated", federatedRefreshCredentials(tok, receivedAt).RefreshToken())
	require.Equal(t, 2, calls)
}

func TestAuthOperationalGapG6ResourceSelectionBoundaryNotWorkerE2E(t *testing.T) {
	t.Parallel()
	client, sibling := uuid.New(), uuid.New()
	const a = "https://upstream.example.test/mcp/"
	const b = "https://other.example.test/mcp"
	for _, tc := range []struct {
		name string
		rows []repo.ListOrganizationMcpServersForClientRow
		want string
	}{
		{"zero_claims", nil, ""},
		{"one_claim", []repo.ListOrganizationMcpServersForClientRow{{Url: a}}, a},
		{"multiple_claims", []repo.ListOrganizationMcpServersForClientRow{{Url: a}, {Url: b}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, clientUpstreamResource(tc.rows))
			attachments := map[uuid.UUID][]repo.ListOrganizationMcpServersForClientRow{client: tc.rows}
			// Empty upstream models the absence of a foreground caller resource.
			require.Equal(t, tc.want, resourceAmongSiblings(attachments, client, ""))
		})
	}
	attachments := map[uuid.UUID][]repo.ListOrganizationMcpServersForClientRow{
		client: {{Url: a}, {Url: b}},
	}
	require.Equal(t, a, resourceAmongSiblings(attachments, client, a))
	attachments[sibling] = []repo.ListOrganizationMcpServersForClientRow{{Url: a}}
	require.Empty(t, resourceAmongSiblings(attachments, client, a), "competing sibling must prevent a claim")
	require.Empty(t, resourceAmongSiblings(attachments, client, ""), "no caller must not select the first attachment")
	attachments[client] = []repo.ListOrganizationMcpServersForClientRow{{Url: b}}
	require.Equal(t, b, resourceAmongSiblings(attachments, client, a), "unique client resource is independent of caller resource")
}

func TestAuthOperationalGapG19ScopeDriftRotationParserBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, scope string
		reported    bool
		scopes      []string
	}{
		{"omitted", "", false, nil},
		{"explicit_empty", `,"scope":""`, true, []string{}},
		{"reduced_scope", `,"scope":"read"`, true, []string{"read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tok tokenResponse
			require.NoError(t, json.Unmarshal([]byte(`{"access_token":"new-access","refresh_token":"rotated"`+tc.scope+`}`), &tok))
			require.Equal(t, tc.reported, tok.ScopeReported())
			require.ElementsMatch(t, tc.scopes, tok.Scopes())
			credentials := federatedRefreshCredentials(tok, time.Now())
			require.Equal(t, "rotated", credentials.RefreshToken())
			// Parsing preserves omission versus reduction and rotation together. This
			// does not prove TokenService's scope merge, CAS persistence, or reconnect.
		})
	}
}

func TestAuthOperationalGapG14ZeroExpiryParserBoundary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, fields   string
		expiredRefresh bool
	}{
		{"omitted", "", false},
		{"numeric_zero", `,"expires_in":0`, false},
		{"string_zero", `,"expires_in":"0"`, false},
		{"standard_refresh_zero", `,"expires_in":0,"refresh_token_timeout":0`, true},
		{"authorization_zero", `,"authorization_expires_in":0`, true},
		{"provider_alias_zero", `,"refresh_expires_in":0`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tok tokenResponse
			require.NoError(t, json.Unmarshal([]byte(`{"access_token":"opaque","refresh_token":"refresh"`+tc.fields+`}`), &tok))
			require.Nil(t, tok.AccessExpiresAt(now), "zero access expiry is unreported for opaque tokens")
			expiry := federatedRefreshCredentials(tok, now).RefreshExpiresAt()
			if tc.expiredRefresh {
				require.NotNil(t, expiry)
				require.Equal(t, now, *expiry, "standard explicit zero must not become unlimited lifetime")
			} else {
				require.Nil(t, expiry)
			}
		})
	}
}
