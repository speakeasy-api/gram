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

func TestOrganizationWithoutMetadataReturnsEmptyObject(t *testing.T) {
	t.Parallel()
	_, handler := openOrganizationEmulator(t, filepath.Join(t.TempDir(), "devidp.db"))
	created := organizationRequest(t, handler, http.MethodPost, "/organizations", `{"name":"Example"}`, http.StatusCreated)
	require.JSONEq(t, `{}`, string(created["metadata"]))
}

func TestOrganizationMetadataFollowsWorkOSUpdateSemantics(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "devidp.db")
	db, handler := openOrganizationEmulator(t, path)

	created := organizationRequest(t, handler, http.MethodPost, "/organizations",
		`{"name":"Example","metadata":{"partition":"eu","plan":"enterprise"}}`, http.StatusCreated)
	require.JSONEq(t, `{"partition":"eu","plan":"enterprise"}`, string(created["metadata"]))
	var id string
	require.NoError(t, json.Unmarshal(created["id"], &id))
	orgPath := "/organizations/" + id

	// An update without metadata, like the external_id back-fill, leaves it alone.
	updated := organizationRequest(t, handler, http.MethodPut, orgPath, `{"external_id":"gram-example"}`, http.StatusOK)
	require.JSONEq(t, `{"partition":"eu","plan":"enterprise"}`, string(updated["metadata"]))
	updated = organizationRequest(t, handler, http.MethodPut, orgPath, `{"metadata":null}`, http.StatusOK)
	require.JSONEq(t, `{"partition":"eu","plan":"enterprise"}`, string(updated["metadata"]))

	// Keys sent are merged in.
	updated = organizationRequest(t, handler, http.MethodPut, orgPath, `{"metadata":{"partition":"us"}}`, http.StatusOK)
	require.JSONEq(t, `{"partition":"us","plan":"enterprise"}`, string(updated["metadata"]))

	// A null value deletes one key.
	updated = organizationRequest(t, handler, http.MethodPut, orgPath, `{"metadata":{"plan":null}}`, http.StatusOK)
	require.JSONEq(t, `{"partition":"us"}`, string(updated["metadata"]))

	require.NoError(t, db.Close())
	_, reopened := openOrganizationEmulator(t, path)
	loaded := organizationRequest(t, reopened, http.MethodGet, orgPath, "", http.StatusOK)
	require.JSONEq(t, `{"partition":"us"}`, string(loaded["metadata"]))
	require.JSONEq(t, `"gram-example"`, string(loaded["external_id"]))

	// An empty object deletes every key.
	updated = organizationRequest(t, reopened, http.MethodPut, orgPath, `{"metadata":{}}`, http.StatusOK)
	require.JSONEq(t, `{}`, string(updated["metadata"]))
}

func TestOrganizationCreateRejectsNullMetadataValue(t *testing.T) {
	t.Parallel()
	_, handler := openOrganizationEmulator(t, filepath.Join(t.TempDir(), "devidp.db"))
	organizationRequest(t, handler, http.MethodPost, "/organizations",
		`{"name":"Rejected","metadata":{"partition":null}}`, http.StatusUnprocessableEntity)
}

func TestOrganizationUpdateAppliesExternalIDAndMetadataTogether(t *testing.T) {
	t.Parallel()
	_, handler := openOrganizationEmulator(t, filepath.Join(t.TempDir(), "devidp.db"))
	created := organizationRequest(t, handler, http.MethodPost, "/organizations",
		`{"name":"Example","metadata":{"partition":"us"}}`, http.StatusCreated)
	var id string
	require.NoError(t, json.Unmarshal(created["id"], &id))

	updated := organizationRequest(t, handler, http.MethodPut, "/organizations/"+id,
		`{"external_id":"gram-example","metadata":{"partition":"eu"}}`, http.StatusOK)
	require.JSONEq(t, `"gram-example"`, string(updated["external_id"]))
	require.JSONEq(t, `{"partition":"eu"}`, string(updated["metadata"]))
	require.JSONEq(t, `"Example"`, string(updated["name"]))
}

func TestOrganizationMetadataRejectsInvalidEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata string
	}{
		{name: "non-string value", metadata: `{"partition":1}`},
		{name: "not an object", metadata: `["eu"]`},
		{name: "empty key", metadata: `{"":"eu"}`},
		{name: "key too long", metadata: `{"` + strings.Repeat("k", 41) + `":"eu"}`},
		{name: "value too long", metadata: `{"partition":"` + strings.Repeat("v", 601) + `"}`},
		{name: "non-ASCII value", metadata: `{"partition":"éu"}`},
		{name: "non-ASCII key", metadata: `{"pärtition":"eu"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, handler := openOrganizationEmulator(t, filepath.Join(t.TempDir(), "devidp.db"))

			organizationRequest(t, handler, http.MethodPost, "/organizations",
				`{"name":"Rejected","metadata":`+tc.metadata+`}`, http.StatusUnprocessableEntity)

			created := organizationRequest(t, handler, http.MethodPost, "/organizations",
				`{"name":"Example","metadata":{"partition":"us"}}`, http.StatusCreated)
			var id string
			require.NoError(t, json.Unmarshal(created["id"], &id))
			organizationRequest(t, handler, http.MethodPut, "/organizations/"+id,
				`{"metadata":`+tc.metadata+`}`, http.StatusUnprocessableEntity)

			loaded := organizationRequest(t, handler, http.MethodGet, "/organizations/"+id, "", http.StatusOK)
			require.JSONEq(t, `{"partition":"us"}`, string(loaded["metadata"]))
		})
	}
}

func TestOrganizationMetadataAcceptsLimitSizedEntries(t *testing.T) {
	t.Parallel()
	_, handler := openOrganizationEmulator(t, filepath.Join(t.TempDir(), "devidp.db"))
	key := strings.Repeat("k", 40)
	value := strings.Repeat("v", 600)
	created := organizationRequest(t, handler, http.MethodPost, "/organizations",
		`{"name":"Example","metadata":{"`+key+`":"`+value+`"}}`, http.StatusCreated)
	require.JSONEq(t, `{"`+key+`":"`+value+`"}`, string(created["metadata"]))
}
