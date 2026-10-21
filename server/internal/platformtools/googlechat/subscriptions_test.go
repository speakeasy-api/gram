package googlechat

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionRenewal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name, target, state, operation string
		remaining                      time.Duration
		patchStatus                    int
		advance, renewed, wantError    bool
		reads, writes                  int
	}{
		{"healthy", "spaces/test", "ACTIVE", `{}`, 3 * time.Hour, 200, false, false, false, 1, 0},
		{"due", "spaces/test", "ACTIVE", `{"done":true}`, time.Hour, 200, true, true, false, 2, 1},
		{"pending but verified", "spaces/test", "ACTIVE", `{"done":false}`, time.Hour, 200, true, true, false, 2, 1},
		{"not extended", "spaces/test", "ACTIVE", `{"done":false}`, time.Hour, 200, false, false, true, 2, 1},
		{"wrong space", "spaces/other", "ACTIVE", `{}`, time.Hour, 200, false, false, true, 1, 0},
		{"suspended", "spaces/test", "SUSPENDED", `{}`, time.Hour, 200, false, false, true, 1, 0},
		{"permission denied", "spaces/test", "ACTIVE", `{}`, time.Hour, 403, false, false, true, 1, 1},
		{"operation failed", "spaces/test", "ACTIVE", `{"done":true,"error":{"code":7,"message":"DO_NOT_EXPOSE"}}`, time.Hour, 200, false, false, true, 1, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := credentials(t)
			reads, writes := 0, 0
			h := &guardian.HTTPClient{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == tokenURL {
					return response(200, `{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`), nil
				}
				require.Equal(t, "workspaceevents.googleapis.com", r.URL.Host)
				require.Equal(t, "/v1/subscriptions/test", r.URL.Path)
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				if r.Method == http.MethodPatch {
					writes++
					require.Equal(t, "ttl", r.URL.Query().Get("updateMask"))
					raw, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.JSONEq(t, `{"ttl":"14400s","etag":"version-1"}`, string(raw))
					if tt.patchStatus != 200 {
						return response(tt.patchStatus, `{"error":"DO_NOT_EXPOSE"}`), nil
					}
					return response(200, tt.operation), nil
				}
				require.Equal(t, http.MethodGet, r.Method)
				reads++
				expiration := now.Add(tt.remaining)
				if writes > 0 && tt.advance {
					expiration = now.Add(4 * time.Hour)
				}
				return response(200, fmt.Sprintf(`{"name":"subscriptions/test","targetResource":"//chat.googleapis.com/%s","state":%q,"expireTime":%q,"etag":"version-1","eventTypes":["google.workspace.chat.message.v1.created"],"payloadOptions":{"includeResourceData":true}}`, tt.target, tt.state, expiration.Format(time.RFC3339))), nil
			})}
			renewed, err := NewSubscriptionRenewer(h).Renew(t.Context(), env, "subscriptions/test", "spaces/test", now)
			if tt.wantError {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "DO_NOT_EXPOSE")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.renewed, renewed)
			require.Equal(t, tt.reads, reads)
			require.Equal(t, tt.writes, writes)
		})
	}
}

func TestSubscriptionRenewalRejectsUntrustedResources(t *testing.T) {
	t.Parallel()
	h := &guardian.HTTPClient{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid resource must never authenticate or send a request")
		return nil, nil
	})}
	for _, name := range []string{"https://attacker.invalid", "subscriptions/../other", "subscriptions/test?other=x"} {
		_, err := NewSubscriptionRenewer(h).Renew(t.Context(), credentials(t), name, "spaces/test", time.Now())
		require.Error(t, err)
	}
}

func TestSubscriptionRenewalRequiresReadableMessageConfiguration(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{
		`{"name":"subscriptions/test","targetResource":"//chat.googleapis.com/spaces/test","state":"ACTIVE","expireTime":"2026-10-01T13:00:00Z","eventTypes":["google.workspace.chat.message.v1.created"],"payloadOptions":{"includeResourceData":false}}`,
		`{"name":"subscriptions/test","targetResource":"//chat.googleapis.com/spaces/test","state":"ACTIVE","expireTime":"2026-10-01T13:00:00Z","eventTypes":["google.workspace.chat.message.v1.updated"],"payloadOptions":{"includeResourceData":true}}`,
	} {
		h := &guardian.HTTPClient{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() == tokenURL {
				return response(200, `{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`), nil
			}
			require.Equal(t, http.MethodGet, r.Method)
			return response(200, payload), nil
		})}
		_, err := NewSubscriptionRenewer(h).Renew(t.Context(), credentials(t), "subscriptions/test", "spaces/test", time.Now())
		require.Error(t, err)
		require.ErrorContains(t, err, "does not match")
	}
}
