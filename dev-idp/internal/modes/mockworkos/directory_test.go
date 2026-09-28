package mockworkos_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/directorysync"
)

// The emulator is exercised through the real workos-go client so the wire
// shapes stay decodable by Gram's WorkOS wrapper.
func TestDirectorySyncThroughSDK(t *testing.T) {
	t.Parallel()
	_, handler := openOrganizationEmulator(t, filepath.Join(t.TempDir(), "devidp.db"))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	created := organizationRequest(t, handler, http.MethodPost, "/organizations", `{"name":"Directory Example"}`, http.StatusCreated)
	var orgID string
	require.NoError(t, json.Unmarshal(created["id"], &orgID))
	directoryID := "directory_mock_" + orgID

	client := &directorysync.Client{APIKey: "test", Endpoint: server.URL, HTTPClient: server.Client()}
	ctx := t.Context()

	var groups []directorysync.Group
	after := ""
	for {
		page, err := client.ListGroups(ctx, directorysync.ListGroupsOpts{Directory: directoryID, User: "", Limit: 2, Order: "", Before: "", After: after})
		require.NoError(t, err)
		groups = append(groups, page.Data...)
		if page.ListMetadata.After == "" {
			break
		}
		after = page.ListMetadata.After
	}
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
		require.Equal(t, directoryID, g.DirectoryID)
		require.Equal(t, orgID, g.OrganizationID)
	}
	slices.Sort(names)
	require.Equal(t, []string{"Design", "Engineering", "Sales", "Security", "Support"}, names)

	engineering := groups[slices.IndexFunc(groups, func(g directorysync.Group) bool { return g.Name == "Engineering" })]
	got, err := client.GetGroup(ctx, directorysync.GetGroupOpts{Group: engineering.ID})
	require.NoError(t, err)
	require.Equal(t, engineering.ID, got.ID)

	members, err := client.ListUsers(ctx, directorysync.ListUsersOpts{Directory: directoryID, Group: engineering.ID, Limit: 100, Order: "", Before: "", After: ""})
	require.NoError(t, err)
	emails := make([]string, 0, len(members.Data))
	for _, u := range members.Data {
		emails = append(emails, u.Email)
	}
	slices.Sort(emails)
	require.Equal(t, []string{"alex.rivera@example.com", "jordan.lee@example.com", "sam.taylor@example.com"}, emails)

	alex := members.Data[slices.IndexFunc(members.Data, func(u directorysync.User) bool { return u.Email == "alex.rivera@example.com" })]
	user, err := client.GetUser(ctx, directorysync.GetUserOpts{User: alex.ID})
	require.NoError(t, err)
	require.Equal(t, "active", string(user.State))
	var attrs map[string]string
	require.NoError(t, json.Unmarshal(user.CustomAttributes, &attrs))
	require.Equal(t, "Engineering", attrs["department_name"])
	require.Equal(t, "Staff Engineer", attrs["job_title"])

	alexGroups, err := client.ListGroups(ctx, directorysync.ListGroupsOpts{Directory: directoryID, User: alex.ID, Limit: 100, Order: "", Before: "", After: ""})
	require.NoError(t, err)
	alexGroupNames := make([]string, 0, len(alexGroups.Data))
	for _, g := range alexGroups.Data {
		alexGroupNames = append(alexGroupNames, g.Name)
	}
	slices.Sort(alexGroupNames)
	require.Equal(t, []string{"Engineering", "Security"}, alexGroupNames)
}

func TestDirectoryGroupsRequireDirectory(t *testing.T) {
	t.Parallel()
	_, handler := openOrganizationEmulator(t, filepath.Join(t.TempDir(), "devidp.db"))
	organizationRequest(t, handler, http.MethodGet, "/directory_groups", "", http.StatusBadRequest)
	organizationRequest(t, handler, http.MethodGet, "/directory_users/directory_user_devidp_nope", "", http.StatusNotFound)
}
