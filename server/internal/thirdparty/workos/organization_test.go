package workos_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/workos_errors"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const verifiedOrganizationResponse = `{"id":"org_example","name":"www.example.com","domains":[{"domain":"www.example.com","state":"verified"}]}`

func organizationHTTPClient(t *testing.T, handler http.HandlerFunc) *workos.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return workos.NewClient(policy, "test-api-key", workos.ClientOpts{Endpoint: server.URL, ClientID: "test-client"})
}

func TestVerifiedOrganizationRequests(t *testing.T) {
	t.Parallel()
	type request struct {
		method string
		path   string
		key    string
		body   []byte
		err    error
	}
	requests := make(chan request, 8)
	client := organizationHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		requests <- request{method: r.Method, path: r.URL.Path, key: r.Header.Get("Idempotency-Key"), body: body, err: err}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, verifiedOrganizationResponse)
	})
	created, err := orgprovision.CreateInWorkOSWithVerifiedDomain(t.Context(), client, "www.example.com")
	require.NoError(t, err)
	require.Equal(t, "org_example", created.WorkOSOrganizationID)
	require.Equal(t, orgid.FromWorkOSID("org_example"), created.GramOrganizationID)
	create := <-requests
	require.NoError(t, create.err)
	require.Equal(t, http.MethodPost, create.method)
	require.Equal(t, "/organizations", create.path)
	require.Empty(t, create.key)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(create.body, &payload))
	require.JSONEq(t, `"www.example.com"`, string(payload["name"]))
	require.JSONEq(t, `[{"domain":"www.example.com","state":"verified"}]`, string(payload["domain_data"]))
	require.JSONEq(t, `null`, string(payload["domains"]))
	require.NotContains(t, payload, "external_id")
	update := <-requests
	require.NoError(t, update.err)
	require.Equal(t, http.MethodPut, update.method)
	require.Equal(t, "/organizations/org_example", update.path)
	require.Empty(t, update.key)
	require.JSONEq(t, `{"external_id":"`+created.GramOrganizationID+`"}`, string(update.body))
	require.Empty(t, requests)

	_, err = orgprovision.CreateInWorkOS(t.Context(), client, "Example")
	require.NoError(t, err)
	plain := <-requests
	require.NoError(t, plain.err)
	require.Empty(t, plain.key)
	payload = nil
	require.NoError(t, json.Unmarshal(plain.body, &payload))
	require.JSONEq(t, `null`, string(payload["domains"]))
	require.JSONEq(t, `null`, string(payload["domain_data"]))
	plainUpdate := <-requests
	require.NoError(t, plainUpdate.err)
	require.JSONEq(t, string(update.body), string(plainUpdate.body))
	require.Empty(t, requests)
}

func TestVerifiedOrganizationRejectsUnexpectedResponses(t *testing.T) {
	t.Parallel()
	for _, response := range []string{
		`{"domains":[{"domain":"www.example.com","state":"verified"}]}`,
		`{"id":"org_example"}`,
		`{"id":"org_example","domains":[]}`,
		`{"id":"org_example","domains":[{"domain":"example.com","state":"verified"}]}`,
		`{"id":"org_example","domains":[{"domain":"www.example.com","state":"pending"}]}`,
		`{"id":"org_example","domains":[{"domain":"www.example.com","state":"failed"}]}`,
		`{"id":"org_example","domains":[{"domain":"www.example.com","state":"legacy_verified"}]}`,
		`{"id":"org_example","domains":[{"domain":"www.example.com","state":"unknown"}]}`,
		`{"id":"org_example","domains":[{"domain":"www.example.com"}]}`,
		`invalid json`,
	} {
		var calls atomic.Int32
		client := organizationHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, response)
		})
		created, err := orgprovision.CreateInWorkOSWithVerifiedDomain(t.Context(), client, "www.example.com")
		require.Error(t, err, response)
		require.NotErrorIs(t, err, workos.ErrOrganizationCreationRejected)
		require.Empty(t, created, response)
		require.EqualValues(t, 1, calls.Load(), "no update, retry, or cleanup: %s", response)
	}
}

func TestVerifiedOrganizationErrorsDoNotRetry(t *testing.T) {
	t.Parallel()
	for _, failingMethod := range []string{http.MethodPost, http.MethodPut} {
		for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError} {
			var creates, updates, other atomic.Int32
			client := organizationHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodPost:
					creates.Add(1)
				case http.MethodPut:
					updates.Add(1)
				default:
					other.Add(1)
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Method == failingMethod {
					w.Header().Set("X-Request-ID", "request_example")
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"message":"private provider detail","code":"unrecognized_code","unknown":"private field"}`)
					return
				}
				_, _ = io.WriteString(w, verifiedOrganizationResponse)
			})
			created, err := orgprovision.CreateInWorkOSWithVerifiedDomain(t.Context(), client, "www.example.com")
			require.Error(t, err)
			require.Empty(t, created)
			var providerError workos_errors.HTTPError
			require.ErrorAs(t, err, &providerError)
			require.Equal(t, status, providerError.Code)
			require.Equal(t, "request_example", providerError.RequestID)
			require.Contains(t, err.Error(), "request_example")
			require.Contains(t, providerError.RawBody, "private field")
			require.Equal(t, failingMethod == http.MethodPost && (status == http.StatusBadRequest || status == http.StatusUnprocessableEntity), errors.Is(err, workos.ErrOrganizationCreationRejected))
			require.EqualValues(t, 1, creates.Load())
			if failingMethod == http.MethodPost {
				require.Zero(t, updates.Load())
			} else {
				require.EqualValues(t, 1, updates.Load())
			}
			require.Zero(t, other.Load(), "no cleanup or lookup")
		}
	}
}

func TestExistingOrganizationOperationsStillRetry(t *testing.T) {
	t.Parallel()
	var creates, updates, reads atomic.Int32
	client := organizationHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		var attempts int32
		switch r.Method {
		case http.MethodPost:
			attempts = creates.Add(1)
		case http.MethodPut:
			attempts = updates.Add(1)
		case http.MethodGet:
			attempts = reads.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"message":"temporary failure"}`)
			return
		}
		_, _ = io.WriteString(w, verifiedOrganizationResponse)
	})
	_, err := orgprovision.CreateInWorkOS(t.Context(), client, "Example")
	require.NoError(t, err)
	_, err = client.GetOrganization(t.Context(), "org_example")
	require.NoError(t, err)
	require.EqualValues(t, 2, creates.Load())
	require.EqualValues(t, 2, updates.Load())
	require.EqualValues(t, 2, reads.Load())
}
