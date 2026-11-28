package github

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCreatePrivateRepository(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		status   int
		body     string
		wantID   int64
		conflict bool
		taken    bool
	}{
		{"created", http.StatusCreated, `{"id":9007199254740993,"full_name":"Org/Repo","private":true}`, 9007199254740993, false, false},
		{"existing", http.StatusUnprocessableEntity, `{"message":"Validation Failed","errors":[{"resource":"Repository","code":"custom","field":"name","message":"name already exists on this account"}]}`, 0, true, true},
		{"existing structured", http.StatusUnprocessableEntity, `{"errors":[{"resource":"Repository","code":"already_exists","field":"name"}]}`, 0, true, true},
		{"unrelated validation error", http.StatusUnprocessableEntity, `{"errors":[{"resource":"Repository","code":"invalid","field":"name"}]}`, 0, false, false},
		{"wrong status", http.StatusOK, `{"id":42,"full_name":"org/repo","private":true}`, 0, false, false},
		{"wrong name", http.StatusCreated, `{"id":42,"full_name":"org/other","private":true}`, 0, true, false},
		{"public", http.StatusCreated, `{"id":42,"full_name":"org/repo","private":false}`, 0, true, false},
		{"internal", http.StatusCreated, `{"id":42,"full_name":"org/repo","private":true,"visibility":"internal"}`, 0, true, false},
		{"missing ID", http.StatusCreated, `{"full_name":"org/repo","private":true}`, 0, true, false},
		{"redirect", http.StatusTemporaryRedirect, `null`, 0, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			client := uploadTestClient(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, apiBase+"/orgs/org/repos", req.URL.String())
				require.Equal(t, "token test-token", req.Header.Get("Authorization"))
				var body struct {
					Name     string `json:"name"`
					Private  bool   `json:"private"`
					AutoInit bool   `json:"auto_init"`
				}
				require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
				require.Equal(t, "repo", body.Name)
				require.True(t, body.Private)
				require.True(t, body.AutoInit)
				resp := jsonResponse(t, tt.status, json.RawMessage(tt.body))
				if tt.status == http.StatusTemporaryRedirect {
					resp.Header.Set("Location", "/orgs/other/repos")
				}
				return resp, nil
			})
			id, err := client.CreatePrivateRepository(t.Context(), 1, "org", "repo")
			if tt.conflict {
				require.ErrorIs(t, err, ErrRepositoryConflict)
			} else if tt.wantID == 0 {
				require.Error(t, err)
				require.NotErrorIs(t, err, ErrRepositoryConflict)
			} else {
				require.NoError(t, err)
			}
			if tt.taken {
				require.ErrorIs(t, err, ErrRepositoryNameTaken)
			} else if err != nil {
				require.NotErrorIs(t, err, ErrRepositoryNameTaken)
			}
			require.Equal(t, tt.wantID, id)
			require.Equal(t, 1, calls)
		})
	}
}

func TestForRepository(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, tt := range []struct {
		name         string
		mintStatus   int
		verifyStatus int
		body         string
		wantOK       bool
		conflict     bool
	}{
		{"scoped", http.StatusCreated, http.StatusOK, `{"id":42,"full_name":"Org/Repo","private":true}`, true, false},
		{"wrong ID", http.StatusCreated, http.StatusOK, `{"id":43,"full_name":"org/repo","private":true}`, false, true},
		{"wrong name", http.StatusCreated, http.StatusOK, `{"id":42,"full_name":"org/renamed","private":true}`, false, true},
		{"public", http.StatusCreated, http.StatusOK, `{"id":42,"full_name":"org/repo","private":false}`, false, true},
		{"mint denied", http.StatusForbidden, 0, `null`, false, false},
		{"mint redirect", http.StatusTemporaryRedirect, 0, `null`, false, false},
		{"verify denied", http.StatusCreated, http.StatusNotFound, `null`, false, false},
		{"verify redirect", http.StatusCreated, http.StatusMovedPermanently, `null`, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			client := uploadTestClient(func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.Method+" "+req.URL.Path)
				switch req.URL.Path {
				case "/app/installations/1/access_tokens":
					require.Contains(t, req.Header.Get("Authorization"), "Bearer ")
					require.Equal(t, "application/json", req.Header.Get("Content-Type"))
					var payload map[string][]int64
					require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
					require.Equal(t, map[string][]int64{"repository_ids": {42}}, payload)
					resp := jsonResponse(t, tt.mintStatus, map[string]any{"token": "scoped-token", "expires_at": time.Now().Add(time.Hour)})
					if tt.mintStatus == http.StatusTemporaryRedirect {
						resp.Header.Set("Location", "/app/installations/2/access_tokens")
					}
					return resp, nil
				case "/repos/org/repo":
					require.Equal(t, "token scoped-token", req.Header.Get("Authorization"))
					resp := jsonResponse(t, tt.verifyStatus, json.RawMessage(tt.body))
					if tt.verifyStatus == http.StatusMovedPermanently {
						resp.Header.Set("Location", "/repositories/42")
					}
					return resp, nil
				case "/repos/org/repo/contents/item":
					require.Equal(t, "token scoped-token", req.Header.Get("Authorization"))
					resp := jsonResponse(t, http.StatusTemporaryRedirect, nil)
					resp.Header.Set("Location", "/repos/org/replacement/contents/item")
					return resp, nil
				default:
					t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
					return nil, nil
				}
			})
			client.appID = 1
			client.privateKey = key

			scoped, err := client.ForRepository(t.Context(), 1, 42, "org", "repo")
			wantRequests := []string{"POST /app/installations/1/access_tokens"}
			if tt.mintStatus == http.StatusCreated {
				wantRequests = append(wantRequests, "GET /repos/org/repo")
			}
			if tt.wantOK {
				require.NoError(t, err)
				token, err := scoped.InstallationToken(t.Context(), 1)
				require.NoError(t, err)
				require.Equal(t, "scoped-token", token)
				_, err = scoped.InstallationToken(t.Context(), 2)
				require.Error(t, err)
				_, err = scoped.GetFileContent(t.Context(), 1, "org", "repo-other", "", "item")
				require.ErrorContains(t, err, "scoped to a different repository")
				_, err = scoped.GetFileContent(t.Context(), 1, "org", "repo", "", "item")
				require.ErrorIs(t, err, ErrRepositoryConflict)
				wantRequests = append(wantRequests, "GET /repos/org/repo/contents/item")
			} else {
				require.Error(t, err)
				if tt.conflict {
					require.ErrorIs(t, err, ErrRepositoryConflict)
				} else {
					require.NotErrorIs(t, err, ErrRepositoryConflict)
				}
				require.Nil(t, scoped)
			}
			token, err := client.InstallationToken(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, "test-token", token)
			require.Equal(t, wantRequests, requests)
		})
	}
}
