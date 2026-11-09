package cataloglocalclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunActions(t *testing.T) {
	t.Parallel()
	const plan = "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name, action, status, response, wantErr string
		code                                    int
		want                                    []string
	}{
		{name: "preview", action: "preview", response: `{"id":"` + plan + `"}`, want: []string{"preview:"}},
		{name: "status", action: "status", response: `{}`, want: []string{"status:"}},
		{name: "apply exact ID", action: "apply", response: `{"state":"completed"}`, want: []string{"apply:" + plan}},
		{name: "stale never refetches", action: "apply", code: 409, wantErr: "stale", want: []string{"apply:" + plan}},
		{name: "unauthorized", action: "preview", code: 403, wantErr: "C1 (ConductorOne)", want: []string{"preview:"}},
		{name: "apply conflicts visible", action: "apply", response: `{"state":"completed_with_conflicts"}`, wantErr: "completed_with_conflicts", want: []string{"apply:" + plan}},
		{name: "bootstrap missing then repeat", action: "bootstrap", status: `{}`, response: `{"created":["example"],"conflicts":[]}`, want: []string{"status:", "sync", "status:"}},
		{name: "bootstrap unknown attempt never reports success", action: "bootstrap", status: `{"lastAttemptState":"interrupted"}`, wantErr: "failed", want: []string{"status:"}},
		{name: "bootstrap established", action: "bootstrap", status: `{"lastFullSuccessRevision":"revision"}`, want: []string{"status:", "status:"}},
		{name: "bootstrap conflict only", action: "bootstrap", status: `{"lastAttemptState":"completed_with_conflicts","lastAttemptRevision":"revision"}`, wantErr: "unresolved", want: []string{"status:"}},
		{name: "bootstrap failed initial attempt requires explicit recovery", action: "bootstrap", status: `{"lastAttemptState":"failed","failure":"source unavailable"}`, wantErr: "inspect --action status and --action preview", want: []string{"status:"}},
		{name: "bootstrap failed after baseline", action: "bootstrap", status: `{"lastFullSuccessRevision":"revision","lastAttemptState":"failed"}`, wantErr: "failed", want: []string{"status:"}},
		{name: "bootstrap initial conflicts", action: "bootstrap", status: `{}`, response: `{"conflicts":["example"]}`, wantErr: "unresolved", want: []string{"status:", "sync"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var adminURL string
			var mu sync.Mutex
			var calls []string
			status := tc.status
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Empty(t, r.Cookies())
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
				case "/admin/catalog.refresh", "/admin/catalog.sync":
					cookie, err := r.Cookie("session")
					assert.NoError(t, err)
					if cookie != nil {
						assert.Equal(t, "verified", cookie.Value)
					}
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, adminURL, r.Header.Get("Origin"))
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					var body map[string]string
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					mu.Lock()
					defer mu.Unlock()
					if r.URL.Path == "/admin/catalog.sync" {
						assert.Empty(t, body)
						calls = append(calls, "sync")
						status = `{"lastAttemptState":"completed","lastFullSuccessRevision":"revision"}`
					} else {
						calls = append(calls, body["action"]+":"+body["planId"])
						if body["action"] == "apply" {
							assert.Len(t, body, 2)
						} else {
							assert.Len(t, body, 1)
						}
						if tc.action == "bootstrap" {
							assert.Equal(t, "status", body["action"])
							_, _ = fmt.Fprint(w, status)
							return
						}
					}
					if tc.code != 0 {
						w.WriteHeader(tc.code)
						return
					}
					_, _ = fmt.Fprint(w, tc.response)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer admin.Close()
			adminURL = admin.URL
			id := ""
			if tc.action == "apply" {
				id = plan
			}
			_, err := Run(t.Context(), admin.URL, provider.URL, "", tc.action, id)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				if tc.action == "bootstrap" {
					_, err = Run(t.Context(), admin.URL, provider.URL, "", tc.action, "")
					require.NoError(t, err)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, tc.want, calls)
		})
	}
}
