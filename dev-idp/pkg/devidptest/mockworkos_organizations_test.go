package devidptest_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
)

// TestMockWorkos_CreateOrganization covers the endpoint Gram calls when a user
// signs up: an org is provisioned for a display name the emulator has never
// seen, and can be read back under the ID it handed out.
func TestMockWorkos_CreateOrganization(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableMockWorkos: true})

	created := createOrg(t, inst.MockWorkosURL, "Acme, Inc.")
	require.NotEmpty(t, created["id"], "response should carry the WorkOS org id")
	require.Equal(t, "Acme, Inc.", created["name"])

	fetched := getOrg(t, inst.MockWorkosURL, created["id"].(string))
	require.Equal(t, created["id"], fetched["id"], "the org should be addressable by the id we were given")
	require.Equal(t, "Acme, Inc.", fetched["name"])

	roles, err := inst.Repo.ListOrganizationRoles(t.Context(), mustOrgUUID(t, inst, created["id"].(string)))
	require.NoError(t, err)
	slugs := make([]string, 0, len(roles))
	for _, r := range roles {
		slugs = append(slugs, r.Slug)
	}
	require.ElementsMatch(t, []string{"admin", "member"}, slugs,
		"a new org should carry the two roles WorkOS provisions, since Gram's membership names one by slug")
}

// TestMockWorkos_CreateOrganizationDistinctNamesGetDistinctOrgs guards the slug
// column, which is unique and whose insert query is find-or-create: two names
// that reduce to the same slug — and a name in a script that reduces to nothing
// at all — must still get organizations of their own.
func TestMockWorkos_CreateOrganizationDistinctNamesGetDistinctOrgs(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableMockWorkos: true})

	first := createOrg(t, inst.MockWorkosURL, "Acme, Inc.")
	second := createOrg(t, inst.MockWorkosURL, "Acme Inc")
	japanese := createOrg(t, inst.MockWorkosURL, "アクメ株式会社")
	chinese := createOrg(t, inst.MockWorkosURL, "字节跳动")

	ids := []any{first["id"], second["id"], japanese["id"], chinese["id"]}
	seen := make(map[any]bool, len(ids))
	for _, id := range ids {
		require.NotEmpty(t, id)
		require.False(t, seen[id], "each organization should get its own id")
		seen[id] = true
	}

	require.Equal(t, "アクメ株式会社", getOrg(t, inst.MockWorkosURL, japanese["id"].(string))["name"])
	require.Equal(t, "字节跳动", getOrg(t, inst.MockWorkosURL, chinese["id"].(string))["name"])
}

// TestMockWorkos_UpdateOrganizationExternalID covers the call Gram makes right
// after creating an org to back-fill its own org ID as external_id.
func TestMockWorkos_UpdateOrganizationExternalID(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableMockWorkos: true})

	created := createOrg(t, inst.MockWorkosURL, "Bob's Bakery")

	body, err := json.Marshal(map[string]string{"external_id": "org_gram_123"})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut,
		inst.MockWorkosURL+"/organizations/"+created["id"].(string), bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	updated := doJSON(t, req, http.StatusOK)
	require.Equal(t, created["id"], updated["id"])
	require.Equal(t, "Bob's Bakery", updated["name"], "an update that omits the name should leave it alone")
	require.Equal(t, "org_gram_123", updated["external_id"])
}

func mustOrgUUID(t *testing.T, inst *devidptest.Instance, workosID string) uuid.UUID {
	t.Helper()

	org, err := inst.Repo.GetOrganizationByWorkosID(t.Context(), sql.NullString{String: workosID, Valid: true})
	require.NoError(t, err, "org %q should be stored under the workos id it was handed out as", workosID)
	return org.ID
}

func createOrg(t *testing.T, mockWorkosURL, name string) map[string]any {
	t.Helper()

	body, err := json.Marshal(map[string]string{"name": name})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		mockWorkosURL+"/organizations", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	return doJSON(t, req, http.StatusCreated)
}

func getOrg(t *testing.T, mockWorkosURL, id string) map[string]any {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		mockWorkosURL+"/organizations/"+id, nil)
	require.NoError(t, err)

	return doJSON(t, req, http.StatusOK)
}

func doJSON(t *testing.T, req *http.Request, wantStatus int) map[string]any {
	t.Helper()

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, wantStatus, resp.StatusCode, "unexpected status: %s", string(raw))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}
