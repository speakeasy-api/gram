package cataloglocalclient

import (
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestValidateReset(t *testing.T) {
	t.Parallel()
	const id = "11111111-1111-4111-8111-111111111111"
	for _, action := range []string{"preview", "status"} {
		require.NoError(t, ValidateReset(action, "", ""))
		require.Error(t, ValidateReset(action, id, "RESET local"))
	}
	require.Error(t, ValidateReset("bootstrap", "", ""))
	require.Error(t, ValidateReset("apply", id, ""))
	require.Error(t, ValidateReset("apply", "", "RESET local"))
	require.NoError(t, ValidateReset("apply", id, "RESET local "+id))
}

func TestResetAuthenticatedActions(t *testing.T) {
	t.Parallel()
	const id = "11111111-1111-4111-8111-111111111111"
	const confirmation = "RESET local " + id
	for _, tc := range []struct {
		action, response, wantError string
		code                        int
	}{
		{action: "preview", response: `{"operation":"reset","destination":"local"}`},
		{action: "status", response: `{}`},
		{action: "apply", response: `{"state":"completed"}`},
		{action: "apply", response: `{"state":"completed_with_conflicts"}`, wantError: "incomplete"},
		{action: "apply", code: 409, wantError: "stale"},
	} {
		t.Run(tc.action+tc.wantError, func(t *testing.T) {
			t.Parallel()
			var adminURL string
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = fmt.Fprint(w, `<form method="POST" action="/authorize"><input name="state_token" value="token"><input name="user_email" value="eng@speakeasyapi.dev"></form>`)
					return
				}
				http.Redirect(w, r, adminURL+"/admin/auth.callback?state=state&code=code", http.StatusFound)
			}))
			defer provider.Close()
			admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/admin/auth.login":
					http.Redirect(w, r, provider.URL+"/authorize?"+url.Values{"state": {"state"}, "redirect_uri": {adminURL + "/admin/auth.callback"}}.Encode(), http.StatusFound)
				case "/admin/auth.callback":
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "verified", Path: "/"})
					http.Redirect(w, r, "/", http.StatusFound)
				case "/admin/catalog.reset":
					calls.Add(1)
					cookie, err := r.Cookie("session")
					assert.NoError(t, err)
					if cookie != nil {
						assert.Equal(t, "verified", cookie.Value)
					}
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, adminURL, r.Header.Get("Origin"))
					var body map[string]string
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					expected := map[string]string{"action": tc.action}
					if tc.action == "apply" {
						expected["planId"] = id
						expected["confirmation"] = confirmation
					}
					assert.Equal(t, expected, body)
					if tc.code != 0 {
						w.WriteHeader(tc.code)
						return
					}
					_, _ = fmt.Fprint(w, tc.response)
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer admin.Close()
			adminURL = admin.URL
			planID, text := "", ""
			if tc.action == "apply" {
				planID, text = id, confirmation
			}
			result, err := RunReset(t.Context(), adminURL, provider.URL, "", tc.action, planID, text)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
			if tc.wantError == "incomplete" {
				require.NotNil(t, result)
			}
			require.EqualValues(t, 1, calls.Load())
		})
	}
}
