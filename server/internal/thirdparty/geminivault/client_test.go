package geminivault

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto/md5"
	"encoding/base64"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func credentialFixture(t *testing.T) map[string]any {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return map[string]any{"delegated_user": "admin@example.com", "org_unit_id": "root-ou", "service_account": map[string]any{"type": "service_account", "client_email": "service@example.iam.gserviceaccount.com", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": "https://oauth2.googleapis.com/token"}}
}

func encode(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func TestCredentials(t *testing.T) {
	t.Parallel()
	fixture := credentialFixture(t)
	raw := encode(t, fixture)
	credentials, err := ParseCredentials(raw)
	require.NoError(t, err)
	require.Equal(t, "admin@example.com", credentials.DelegatedUser)
	tests := []struct {
		name string
		edit func(map[string]any)
	}{
		{"missing OU", func(c map[string]any) { delete(c, "org_unit_id") }},
		{"blank OU", func(c map[string]any) { c["org_unit_id"] = " \t\n" }},
		{"missing subject", func(c map[string]any) { c["delegated_user"] = " " }},
		{"missing service account", func(c map[string]any) { delete(c, "service_account") }},
		{"wrong type", func(c map[string]any) { testAccount(c)["type"] = "authorized_user" }},
		{"bad key", func(c map[string]any) { testAccount(c)["private_key"] = "SECRET" }},
	}
	for _, endpoint := range []string{"https://attacker.example/token", "https://oauth2.googleapis.com/token?x=1", "http://oauth2.googleapis.com/token", "https://oauth2.googleapis.com/token/", "https://oauth2.googleapis.com@attacker.example/token"} {
		tests = append(tests, struct {
			name string
			edit func(map[string]any)
		}{endpoint, func(c map[string]any) { testAccount(c)["token_uri"] = endpoint }})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var c map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &c))
			tc.edit(c)
			_, err := ParseCredentials(encode(t, c))
			require.Error(t, err)
			require.NotContains(t, err.Error(), "SECRET")
		})
	}
	for _, endpoint := range []string{"https://accounts.google.com/o/oauth2/token", "https://oauth2.googleapis.com/token"} {
		testAccount(fixture)["token_uri"] = endpoint
		_, err := ParseCredentials(encode(t, fixture))
		require.NoError(t, err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(ec)
	require.NoError(t, err)
	testAccount(fixture)["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	_, err = ParseCredentials(encode(t, fixture))
	require.ErrorContains(t, err, "RSA")
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)
	der, err = x509.MarshalPKCS8PrivateKey(weak)
	require.NoError(t, err)
	testAccount(fixture)["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	_, err = ParseCredentials(encode(t, fixture))
	require.ErrorContains(t, err, "at least 2048 bits")
	_, err = ParseCredentials("{SECRET")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRET")
}

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	// Exercise the same OAuth bearer transport without a live token exchange.
	client := server.Client()
	client.Transport = &oauth2.Transport{Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"}), Base: client.Transport}
	client.CheckRedirect = denyRedirect
	return &Client{httpClient: client, matterID: "matter", orgUnitID: "root-ou", baseURL: server.URL + "/v1", storageURL: server.URL + "/storage/v1", maxAPIBytes: 4096, maxDownloadBytes: 1024}
}

func TestNewClient(t *testing.T) {
	t.Parallel()
	raw := encode(t, credentialFixture(t))
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	client, err := NewClient(raw, "00000000-0000-4000-8000-000000000001", policy)
	require.NoError(t, err)
	require.Equal(t, "https://vault.googleapis.com/v1", client.baseURL)
	require.Equal(t, "https://storage.googleapis.com/storage/v1", client.storageURL)
	_, err = NewClient(raw, "bad/id", policy)
	require.Error(t, err)
	_, err = NewClient(raw, "00000000-0000-4000-8000-000000000001", nil)
	require.Error(t, err)
}

func TestExportRequests(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/matters/matter":
			assert.Equal(t, "BASIC", r.URL.Query().Get("view"))
			_, _ = fmt.Fprint(w, `{"state":"OPEN"}`)
		case "POST /v1/matters/matter/exports":
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			var body map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, map[string]any{"name": "deterministic-name", "query": map[string]any{"corpus": "GEMINI", "dataScope": "ALL_DATA", "searchMethod": "ORG_UNIT", "orgUnitInfo": map[string]any{"orgUnitId": "root-ou"}, "startTime": start.Format(time.RFC3339Nano), "endTime": start.Add(time.Hour).Format(time.RFC3339Nano)}, "exportOptions": map[string]any{"geminiOptions": map[string]any{"exportFormat": "XML"}}}, body)
			_, _ = fmt.Fprint(w, `{"id":"export-id","status":"IN_PROGRESS"}`)
		case "GET /v1/matters/matter/exports/export-id":
			_, _ = fmt.Fprint(w, `{"id":"export-id","status":"COMPLETED","stats":{"exportedArtifactCount":"2","totalArtifactCount":"2"},"cloudStorageSink":{"files":[{"bucketName":"bucket","objectName":"folder/file.zip","size":"12","md5Hash":"digest"}]}}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	})
	require.NoError(t, client.GetMatter(t.Context()))
	job, err := client.CreateExport(t.Context(), "deterministic-name", start, start.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, "IN_PROGRESS", job.Status)
	job, err = client.GetExport(t.Context(), "export-id")
	require.NoError(t, err)
	require.Equal(t, int64(2), job.ExportedArtifactCount)
	require.Equal(t, int64(2), job.TotalArtifactCount)
	require.Equal(t, int64(12), job.Files[0].Size)
	client.orgUnitID = ""
	_, err = client.CreateExport(t.Context(), "name", start, start.Add(time.Hour))
	require.ErrorContains(t, err, "org_unit_id")
	client.orgUnitID = "root"
	_, err = client.CreateExport(t.Context(), "name", start, start)
	require.Error(t, err)
	_, err = client.GetExport(t.Context(), "..")
	require.Error(t, err)
}

func TestFindExportPagination(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"wanted", "absent"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			count := 0
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				count++
				assert.Equal(t, "100", r.URL.Query().Get("pageSize"))
				if count == 1 {
					assert.Empty(t, r.URL.Query().Get("pageToken"))
					_, _ = fmt.Fprint(w, `{"exports":[{"name":"other"}],"nextPageToken":"next+token"}`)
					return
				}
				assert.Equal(t, "next+token", r.URL.Query().Get("pageToken"))
				_, _ = fmt.Fprint(w, `{"exports":[{"name":"wanted","id":"found","status":"IN_PROGRESS"}]}`)
			})
			job, err := client.FindExport(t.Context(), name)
			require.NoError(t, err)
			require.Equal(t, 2, count)
			if name == "wanted" {
				require.Equal(t, "found", job.ID)
			} else {
				require.Nil(t, job)
			}
		})
	}
}

func TestDownloadEscaping(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/storage/v1/b/bucket/o/a%2Fb%20%3F%23.zip", r.URL.EscapedPath())
		assert.Equal(t, "alt=media", r.URL.RawQuery)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		_, _ = fmt.Fprint(w, "archive")
	})
	data, err := client.Download(t.Context(), File{BucketName: "bucket", ObjectName: "a/b ?#.zip", Size: 7})
	require.NoError(t, err)
	require.Equal(t, "archive", string(data))
	for _, file := range []File{{}, {BucketName: "bucket", ObjectName: "x", Size: 1025}, {BucketName: "bucket", ObjectName: "x", Size: -1}, {BucketName: "bucket", ObjectName: ".."}} {
		_, err = client.Download(t.Context(), file)
		require.Error(t, err)
	}
}

func TestRequestSafety(t *testing.T) {
	t.Parallel()
	for _, status := range []int{301, 302, 307, 400, 401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			calls := 0
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "http://attacker.example/SECRET")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, "SECRET")
			})
			_, err := client.GetExport(t.Context(), "export")
			require.Error(t, err)
			require.Contains(t, err.Error(), fmt.Sprint(status))
			require.NotContains(t, err.Error(), "SECRET")
			require.Equal(t, 1, calls)
		})
	}
	t.Run("size", func(t *testing.T) {
		t.Parallel()
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, strings.Repeat("x", 1025)) })
		_, err := client.Download(t.Context(), File{BucketName: "b", ObjectName: "o"})
		require.ErrorContains(t, err, "limit")
	})
	t.Run("chunked size", func(t *testing.T) {
		t.Parallel()
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			flusher, ok := w.(http.Flusher)
			assert.True(t, ok)
			if ok {
				flusher.Flush()
			}
			_, _ = fmt.Fprint(w, strings.Repeat("x", 1025))
		})
		_, err := client.Download(t.Context(), File{BucketName: "b", ObjectName: "o"})
		require.ErrorContains(t, err, "limit")
	})
	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) {})
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := client.GetExport(ctx, "export")
		require.Error(t, err)
	})
	for _, response := range []string{`SECRET`, `{}`, `{"id":"x","status":"COMPLETED","cloudStorageSink":{"files":[{"size":"invalid"}]}}`} {
		t.Run("invalid export", func(t *testing.T) {
			t.Parallel()
			_, err := parseExport([]byte(response))
			require.Error(t, err)
			require.NotContains(t, err.Error(), "SECRET")
		})
	}
	for _, state := range []string{"CLOSED", "DELETED", ""} {
		t.Run("matter "+state, func(t *testing.T) {
			t.Parallel()
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprintf(w, `{"state":%q}`, state) })
			require.ErrorIs(t, client.GetMatter(t.Context()), ErrMatterNotOpen)
		})
	}
}

func TestDeleteExport(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v1/matters/matter/exports/export%2Fid", r.URL.EscapedPath())
		w.WriteHeader(204)
	})
	require.NoError(t, client.DeleteExport(t.Context(), "export/id"))
	require.Error(t, client.DeleteExport(t.Context(), ""))
	require.Error(t, client.DeleteExport(t.Context(), ".."))
	missing := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404); _, _ = fmt.Fprint(w, "SECRET") })
	var status *HTTPError
	require.ErrorAs(t, missing.DeleteExport(t.Context(), "id"), &status)
	require.Equal(t, 404, status.StatusCode)
}

func TestFindExportRejectsAmbiguityAndRepeatedPages(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"exports":[{"name":"wanted","id":"one","status":"IN_PROGRESS"},{"name":"wanted","id":"two","status":"IN_PROGRESS"}]}`,
		`{"nextPageToken":"repeat"}`,
		`SECRET`,
	} {
		t.Run("list failure", func(t *testing.T) {
			t.Parallel()
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, body) })
			_, err := client.FindExport(t.Context(), "wanted")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "SECRET")
		})
	}
}

type failingTokenSource struct{ err error }

func (s failingTokenSource) Token() (*oauth2.Token, error) { return nil, s.err }

func TestTokenErrorsNeverExposeSecrets(t *testing.T) {
	t.Parallel()
	secret := "private-assertion-and-upstream-body"
	for _, tc := range []struct {
		tokenErr     error
		wantError    string
		wantStatus   int
		wantRejected bool
	}{
		// unauthorized_client: domain-wide delegation does not grant the scopes.
		{&oauth2.RetrieveError{Response: &http.Response{StatusCode: 401}, Body: []byte(secret), ErrorCode: "unauthorized_client", ErrorDescription: secret}, "vault token exchange returned HTTP 401", 401, true},
		// invalid_grant: a deleted key or unknown delegated user.
		{fmt.Errorf("refresh failed: %w", &oauth2.RetrieveError{Response: &http.Response{StatusCode: 400}, Body: []byte(secret)}), "vault token exchange returned HTTP 400", 400, true},
		// Throttling and outages stay retryable.
		{&oauth2.RetrieveError{Response: &http.Response{StatusCode: 429}, Body: []byte(secret)}, "vault token exchange returned HTTP 429", 429, false},
		{&oauth2.RetrieveError{Response: &http.Response{StatusCode: 503}, Body: []byte(secret)}, "vault token exchange returned HTTP 503", 503, false},
		{&oauth2.RetrieveError{Body: []byte(secret)}, "vault request failed", 0, false},
		{errors.New(secret), "vault request failed", 0, false},
	} {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("failed token must not reach API") })
		client.httpClient.Transport = &oauth2.Transport{Source: failingTokenSource{err: tc.tokenErr}, Base: client.httpClient.Transport}
		_, err := client.GetExport(t.Context(), "id")
		require.EqualError(t, err, tc.wantError)
		require.NotContains(t, err.Error(), secret)
		status, ok := errors.AsType[*HTTPError](err)
		require.Equal(t, tc.wantStatus != 0, ok)
		if ok {
			require.True(t, status.TokenExchange)
			require.Equal(t, tc.wantStatus, status.StatusCode)
			require.Equal(t, tc.wantRejected, status.CredentialsRejected())
		}
	}
	// A Vault or Cloud Storage status is not a token-exchange rejection.
	require.False(t, (&HTTPError{StatusCode: 401}).CredentialsRejected())
}

func TestCancellationPreservesSafeCause(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("cancelled request must not reach API") })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := client.GetExport(ctx, "id")
	require.ErrorIs(t, err, context.Canceled)
}

func testAccount(c map[string]any) map[string]any {
	account, ok := c["service_account"].(map[string]any)
	if !ok {
		panic("invalid test credential fixture")
	}
	return account
}

func TestDownloadIntegrity(t *testing.T) {
	t.Parallel()
	payload := "archive"
	digest := md5.Sum([]byte(payload))
	hash := base64.StdEncoding.EncodeToString(digest[:])
	for _, tc := range []struct {
		name      string
		size      int64
		hash      string
		wantError string
	}{
		{name: "matching", size: 7, hash: hash, wantError: ""},
		{name: "no advertised size", size: 0, hash: hash, wantError: ""},
		{name: "truncated", size: 8, hash: hash, wantError: "size mismatch"},
		{name: "wrong digest", size: 7, hash: "secret-invalid-digest", wantError: "checksum mismatch"},
		{name: "no checksum", size: 7, hash: "", wantError: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, payload) })
			data, err := client.Download(t.Context(), File{BucketName: "bucket", ObjectName: "object", Size: tc.size, MD5Hash: tc.hash})
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.NotContains(t, err.Error(), "secret-invalid-digest")
				require.Nil(t, data)
			} else {
				require.NoError(t, err)
				require.Equal(t, payload, string(data))
			}
		})
	}
}
