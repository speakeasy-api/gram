package workos_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/workos/workos-go/v6/pkg/common"
	"github.com/workos/workos-go/v6/pkg/roles"
	"github.com/workos/workos-go/v6/pkg/usermanagement"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// fakeWorkOS is a lightweight HTTP handler that mimics the WorkOS API endpoints
// used by RoleClient. It stores state in memory.
type fakeWorkOS struct {
	mu             sync.Mutex
	roles          map[string][]roles.Role // orgID → roles
	memberships    []usermanagement.OrganizationMembership
	users          map[string]common.User // userID → user
	orgUsers       map[string][]string    // orgID → []userID
	memberPageSize int                    // if > 0, paginates ListMembers responses
	nextRoleID     int
}

func newFakeWorkOS() *fakeWorkOS {
	return &fakeWorkOS{
		roles:    make(map[string][]roles.Role),
		users:    make(map[string]common.User),
		orgUsers: make(map[string][]string),
	}
}

func (f *fakeWorkOS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	// SDK: GET /organizations/{orgID}/roles
	case r.Method == http.MethodGet && matchPath(path, "/organizations/", "/roles"):
		orgID := extractSegment(path, "/organizations/", "/roles")
		f.handleListRoles(w, orgID)

	// Raw HTTP: POST /authorization/organizations/{orgID}/roles
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/authorization/organizations/") && strings.HasSuffix(path, "/roles"):
		orgID := extractSegment(path, "/authorization/organizations/", "/roles")
		f.handleCreateRole(w, r, orgID)

	// Raw HTTP: PATCH /authorization/organizations/{orgID}/roles/{slug}
	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/authorization/organizations/"):
		parts := strings.Split(strings.TrimPrefix(path, "/authorization/organizations/"), "/")
		if len(parts) >= 3 && parts[1] == "roles" {
			f.handleUpdateRole(w, r, parts[0], parts[2])
		} else {
			http.NotFound(w, r)
		}

	// Raw HTTP: DELETE /authorization/organizations/{orgID}/roles/{slug}
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/authorization/organizations/"):
		parts := strings.Split(strings.TrimPrefix(path, "/authorization/organizations/"), "/")
		if len(parts) >= 3 && parts[1] == "roles" {
			f.handleDeleteRole(w, parts[0], parts[2])
		} else {
			http.NotFound(w, r)
		}

	// SDK: GET /user_management/organization_memberships
	case r.Method == http.MethodGet && path == "/user_management/organization_memberships":
		f.handleListMembers(w, r)

	// SDK: PUT /user_management/organization_memberships/{id}
	case r.Method == http.MethodPut && strings.HasPrefix(path, "/user_management/organization_memberships/"):
		membershipID := strings.TrimPrefix(path, "/user_management/organization_memberships/")
		f.handleUpdateMemberRole(w, r, membershipID)

	// SDK: GET /user_management/users/{id}
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/user_management/users/") && !strings.Contains(path[len("/user_management/users/"):], "/"):
		userID := strings.TrimPrefix(path, "/user_management/users/")
		f.handleGetUser(w, userID)

	// SDK: GET /user_management/users (list)
	case r.Method == http.MethodGet && path == "/user_management/users":
		f.handleListUsers(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (f *fakeWorkOS) handleListRoles(w http.ResponseWriter, orgID string) {
	f.mu.Lock()
	rr := f.roles[orgID]
	f.mu.Unlock()

	writeJSON(w, map[string]any{"data": rr})
}

func (f *fakeWorkOS) handleCreateRole(w http.ResponseWriter, r *http.Request, orgID string) {
	var opts workos.CreateRoleOpts
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.nextRoleID++
	role := roles.Role{
		ID:          fmt.Sprintf("role_%d", f.nextRoleID),
		Name:        opts.Name,
		Slug:        opts.Slug,
		Description: opts.Description,
		Type:        "OrganizationRole",
		CreatedAt:   "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-01-01T00:00:00Z",
	}
	f.roles[orgID] = append(f.roles[orgID], role)

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, role)
}

func (f *fakeWorkOS) handleUpdateRole(w http.ResponseWriter, r *http.Request, orgID, slug string) {
	var opts workos.UpdateRoleOpts
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	for i, role := range f.roles[orgID] {
		if role.Slug == slug {
			if opts.Name != nil {
				f.roles[orgID][i].Name = *opts.Name
			}
			if opts.Description != nil {
				f.roles[orgID][i].Description = *opts.Description
			}
			f.roles[orgID][i].UpdatedAt = "2026-01-02T00:00:00Z"
			writeJSON(w, f.roles[orgID][i])
			return
		}
	}
	http.Error(w, "role not found", http.StatusNotFound)
}

func (f *fakeWorkOS) handleDeleteRole(w http.ResponseWriter, orgID, slug string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	rr := f.roles[orgID]
	for i, role := range rr {
		if role.Slug == slug {
			f.roles[orgID] = append(rr[:i], rr[i+1:]...)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	http.Error(w, "role not found", http.StatusNotFound)
}

func (f *fakeWorkOS) handleListMembers(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("organization_id")
	afterCursor := r.URL.Query().Get("after")

	f.mu.Lock()
	var filtered []usermanagement.OrganizationMembership
	for _, m := range f.memberships {
		if m.OrganizationID == orgID {
			filtered = append(filtered, m)
		}
	}
	pageSize := f.memberPageSize
	f.mu.Unlock()

	// Advance past the cursor if set.
	start := 0
	if afterCursor != "" {
		for i, m := range filtered {
			if m.ID == afterCursor {
				start = i + 1
				break
			}
		}
	}
	page := filtered[start:]

	var nextCursor string
	if pageSize > 0 && len(page) > pageSize {
		page = page[:pageSize]
		nextCursor = page[len(page)-1].ID
	}

	writeJSON(w, map[string]any{
		"data":          page,
		"list_metadata": common.ListMetadata{Before: "", After: nextCursor},
	})
}

func (f *fakeWorkOS) handleUpdateMemberRole(w http.ResponseWriter, r *http.Request, membershipID string) {
	var opts struct {
		RoleSlug string `json:"role_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	for i, m := range f.memberships {
		if m.ID == membershipID {
			f.memberships[i].Role = common.RoleResponse{Slug: opts.RoleSlug}
			writeJSON(w, f.memberships[i])
			return
		}
	}
	http.Error(w, "membership not found", http.StatusNotFound)
}

func (f *fakeWorkOS) handleGetUser(w http.ResponseWriter, userID string) {
	f.mu.Lock()
	u, ok := f.users[userID]
	f.mu.Unlock()

	if !ok {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	writeJSON(w, u)
}

func (f *fakeWorkOS) handleListUsers(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("organization_id")

	f.mu.Lock()
	var all []common.User
	for _, userID := range f.orgUsers[orgID] {
		if u, ok := f.users[userID]; ok {
			all = append(all, u)
		}
	}
	f.mu.Unlock()

	writeJSON(w, map[string]any{
		"data":          all,
		"list_metadata": common.ListMetadata{},
	})
}

// --- helpers ---

func matchPath(path, prefix, suffix string) bool {
	return strings.HasPrefix(path, prefix) && strings.HasSuffix(path, suffix) && len(path) > len(prefix)+len(suffix)
}

func extractSegment(path, prefix, suffix string) string {
	return strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// --- test setup ---

func newTestClient(t *testing.T, fake *fakeWorkOS) (*workos.RoleClient, *fakeWorkOS) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	client := workos.NewRoleClient("test-api-key", workos.RoleClientOpts{
		Endpoint:   srv.URL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	})
	return client, fake
}

// --- tests ---

func TestRoleClient_ListRoles(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.roles["org_1"] = []roles.Role{
		{ID: "role_a", Name: "Admin", Slug: "admin", Type: "EnvironmentRole"},
		{ID: "role_b", Name: "Editor", Slug: "org-editor", Type: "OrganizationRole"},
	}
	client, _ := newTestClient(t, fake)

	result, err := client.ListRoles(context.Background(), "org_1")
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, "Admin", result[0].Name)
	require.Equal(t, "Editor", result[1].Name)
}

func TestRoleClient_ListRoles_EmptyOrg(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, newFakeWorkOS())

	result, err := client.ListRoles(context.Background(), "org_empty")
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestRoleClient_CreateRole(t *testing.T) {
	t.Parallel()
	client, fake := newTestClient(t, newFakeWorkOS())

	role, err := client.CreateRole(context.Background(), "org_1", workos.CreateRoleOpts{
		Name:        "Deployer",
		Slug:        "org-deployer",
		Description: "Can deploy",
	})
	require.NoError(t, err)
	require.NotEmpty(t, role.ID)
	require.Equal(t, "Deployer", role.Name)
	require.Equal(t, "org-deployer", role.Slug)

	// Verify it was stored.
	fake.mu.Lock()
	require.Len(t, fake.roles["org_1"], 1)
	fake.mu.Unlock()
}

func TestRoleClient_UpdateRole(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.roles["org_1"] = []roles.Role{
		{ID: "role_1", Name: "Old Name", Slug: "org-old", Type: "OrganizationRole"},
	}
	client, _ := newTestClient(t, fake)

	newName := "New Name"
	newDesc := "Updated description"
	updated, err := client.UpdateRole(context.Background(), "org_1", "org-old", workos.UpdateRoleOpts{
		Name:        &newName,
		Description: &newDesc,
	})
	require.NoError(t, err)
	require.Equal(t, "New Name", updated.Name)
	require.Equal(t, "Updated description", updated.Description)
}

func TestRoleClient_UpdateRole_NotFound(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, newFakeWorkOS())

	newName := "Ghost"
	_, err := client.UpdateRole(context.Background(), "org_1", "org-nonexistent", workos.UpdateRoleOpts{
		Name: &newName,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
}

func TestRoleClient_DeleteRole(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.roles["org_1"] = []roles.Role{
		{ID: "role_1", Name: "Temp", Slug: "org-temp"},
	}
	client, _ := newTestClient(t, fake)

	err := client.DeleteRole(context.Background(), "org_1", "org-temp")
	require.NoError(t, err)

	// Verify removed.
	fake.mu.Lock()
	require.Empty(t, fake.roles["org_1"])
	fake.mu.Unlock()
}

func TestRoleClient_DeleteRole_NotFound(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, newFakeWorkOS())

	err := client.DeleteRole(context.Background(), "org_1", "org-nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
}

func TestRoleClient_ListMembers(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.memberships = []usermanagement.OrganizationMembership{
		{
			ID:             "mem_1",
			UserID:         "user_1",
			OrganizationID: "org_1",
			Role:           common.RoleResponse{Slug: "admin"},
			Status:         usermanagement.Active,
		},
	}
	client, _ := newTestClient(t, fake)

	members, err := client.ListMembers(context.Background(), "org_1")
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, "user_1", members[0].UserID)
	require.Equal(t, "admin", members[0].RoleSlug)
}

func TestRoleClient_ListMembers_Pagination(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.memberPageSize = 2
	fake.memberships = []usermanagement.OrganizationMembership{
		{ID: "mem_1", UserID: "user_1", OrganizationID: "org_1", Status: usermanagement.Active},
		{ID: "mem_2", UserID: "user_2", OrganizationID: "org_1", Status: usermanagement.Active},
		{ID: "mem_3", UserID: "user_3", OrganizationID: "org_1", Status: usermanagement.Active},
	}
	client, _ := newTestClient(t, fake)

	members, err := client.ListMembers(context.Background(), "org_1")
	require.NoError(t, err)
	require.Len(t, members, 3)
	require.Equal(t, "user_1", members[0].UserID)
	require.Equal(t, "user_2", members[1].UserID)
	require.Equal(t, "user_3", members[2].UserID)
}

func TestRoleClient_UpdateMemberRole(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.memberships = []usermanagement.OrganizationMembership{
		{
			ID:     "mem_1",
			UserID: "user_1",
			Role:   common.RoleResponse{Slug: "admin"},
		},
	}
	client, _ := newTestClient(t, fake)

	updated, err := client.UpdateMemberRole(context.Background(), "mem_1", "org-editor")
	require.NoError(t, err)
	require.Equal(t, "org-editor", updated.RoleSlug)
}

func TestRoleClient_GetUser(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.users["user_1"] = common.User{
		ID:        "user_1",
		FirstName: "Jane",
		LastName:  "Doe",
		Email:     "jane@example.com",
	}
	client, _ := newTestClient(t, fake)

	user, err := client.GetUser(context.Background(), "user_1")
	require.NoError(t, err)
	require.Equal(t, "Jane", user.FirstName)
	require.Equal(t, "jane@example.com", user.Email)
}

func TestRoleClient_GetUser_NotFound(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, newFakeWorkOS())

	_, err := client.GetUser(context.Background(), "user_nonexistent")
	require.Error(t, err)
}

func TestRoleClient_ListOrgUsers(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.users["user_1"] = common.User{ID: "user_1", FirstName: "Alice", Email: "alice@example.com"}
	fake.users["user_2"] = common.User{ID: "user_2", FirstName: "Bob", Email: "bob@example.com"}
	fake.users["user_3"] = common.User{ID: "user_3", FirstName: "Carol", Email: "carol@example.com"}
	fake.orgUsers["org_1"] = []string{"user_1", "user_2"}
	// user_3 belongs to a different org — should not appear in results
	fake.orgUsers["org_2"] = []string{"user_3"}
	client, _ := newTestClient(t, fake)

	users, err := client.ListOrgUsers(context.Background(), "org_1")
	require.NoError(t, err)
	require.Len(t, users, 2)
	require.Equal(t, "Alice", users["user_1"].FirstName)
	require.Equal(t, "Bob", users["user_2"].FirstName)
	require.NotContains(t, users, "user_3")
}

func TestRoleClient_NilClient(t *testing.T) {
	t.Parallel()

	var rc *workos.RoleClient
	_, err := rc.ListRoles(context.Background(), "org_1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not initialized")
}
