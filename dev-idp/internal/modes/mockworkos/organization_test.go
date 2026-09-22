package mockworkos_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func organizationRequest(t *testing.T, handler http.Handler, method, path, body string, status int) map[string]json.RawMessage {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(t.Context())
	handler.ServeHTTP(recorder, req)
	require.Equal(t, status, recorder.Code, recorder.Body.String())
	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	return result
}

func TestOrganizationDomainsPersist(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "devidp.db")
	db, handler := openOrganizationEmulator(t, path)
	created := organizationRequest(t, handler, http.MethodPost, "/organizations",
		`{"name":"www.example.com","domain_data":[{"domain":"www.example.com","state":"verified"}]}`, http.StatusCreated)
	var id string
	require.NoError(t, json.Unmarshal(created["id"], &id))
	require.NotEmpty(t, id)
	var domains []struct {
		ID             string `json:"id"`
		OrganizationID string `json:"organization_id"`
		Domain         string `json:"domain"`
		State          string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(created["domains"], &domains))
	require.Len(t, domains, 1)
	require.NotEmpty(t, domains[0].ID)
	require.Equal(t, id, domains[0].OrganizationID)
	require.Equal(t, "www.example.com", domains[0].Domain)
	require.Equal(t, "verified", domains[0].State)
	updated := organizationRequest(t, handler, http.MethodPut, "/organizations/"+id, `{"external_id":"gram-example"}`, http.StatusOK)
	require.JSONEq(t, string(created["domains"]), string(updated["domains"]))
	require.JSONEq(t, `"www.example.com"`, string(updated["name"]))
	require.JSONEq(t, `"gram-example"`, string(updated["external_id"]))
	require.NoError(t, db.Close())
	_, reopened := openOrganizationEmulator(t, path)
	loaded := organizationRequest(t, reopened, http.MethodGet, "/organizations/"+id, "", http.StatusOK)
	require.JSONEq(t, string(updated["domains"]), string(loaded["domains"]))
	require.JSONEq(t, string(updated["external_id"]), string(loaded["external_id"]))
	require.JSONEq(t, string(updated["name"]), string(loaded["name"]))
}

func TestNameOnlyOrganizationRemainsDomainFree(t *testing.T) {
	t.Parallel()
	_, handler := openOrganizationEmulator(t, filepath.Join(t.TempDir(), "devidp.db"))
	created := organizationRequest(t, handler, http.MethodPost, "/organizations", `{"name":"Example"}`, http.StatusCreated)
	require.JSONEq(t, `[]`, string(created["domains"]))
	var id string
	require.NoError(t, json.Unmarshal(created["id"], &id))
	updated := organizationRequest(t, handler, http.MethodPut, "/organizations/"+id, `{"external_id":"gram-example"}`, http.StatusOK)
	require.JSONEq(t, `[]`, string(updated["domains"]))
	loaded := organizationRequest(t, handler, http.MethodGet, "/organizations/"+id, "", http.StatusOK)
	require.JSONEq(t, `[]`, string(loaded["domains"]))
}
