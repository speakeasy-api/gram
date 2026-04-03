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

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// fakeWorkOS is a lightweight HTTP handler that mimics the WorkOS API endpoints
// used by Client. It stores state in memory.
type fakeWorkOS struct {
	mu             sync.Mutex
	roles          map[string][]roles.Role // orgID → roles
	memberships    []usermanagement.OrganizationMembership
	users          map[string]common.User // userID → user
	orgUsers       map[string][]string    // orgID → []userID
	invitations    []usermanagement.Invitation
	memberPageSize int // if > 0, paginates ListMembers responses
	invitePageSize int // if > 0, paginates ListInvitations responses
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

	// SDK: POST /user_management/invitations
	case r.Method == http.MethodPost && path == "/user_management/invitations":
		f.handleSendInvitation(w, r)

	// SDK: GET /user_management/invitations
	case r.Method == http.MethodGet && path == "/user_management/invitations":
		f.handleListInvitations(w, r)

	// SDK: GET /user_management/invitations/by_token/{token}
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/user_management/invitations/by_token/"):
		f.handleFindInvitationByToken(w, strings.TrimPrefix(path, "/user_management/invitations/by_token/"))

	// SDK: POST /user_management/invitations/{id}/revoke
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/user_management/invitations/") && strings.HasSuffix(path, "/revoke"):
		f.handleRevokeInvitation(w, extractSegment(path, "/user_management/invitations/", "/revoke"))

	// SDK: POST /user_management/invitations/{id}/resend
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/user_management/invitations/") && strings.HasSuffix(path, "/resend"):
		f.handleResendInvitation(w, extractSegment(path, "/user_management/invitations/", "/resend"))

	// SDK: GET /user_management/invitations/{id}
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/user_management/invitations/") && !strings.Contains(strings.TrimPrefix(path, "/user_management/invitations/"), "/"):
		f.handleGetInvitation(w, strings.TrimPrefix(path, "/user_management/invitations/"))

	// SDK: DELETE /user_management/organization_memberships/{id}
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/user_management/organization_memberships/"):
		f.handleDeleteMembership(w, strings.TrimPrefix(path, "/user_management/organization_memberships/"))

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
	userID := r.URL.Query().Get("user_id")
	afterCursor := r.URL.Query().Get("after")

	f.mu.Lock()
	var filtered []usermanagement.OrganizationMembership
	for _, m := range f.memberships {
		if (orgID == "" || m.OrganizationID == orgID) && (userID == "" || m.UserID == userID) {
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
	email := r.URL.Query().Get("email")

	f.mu.Lock()
	var all []common.User
	if orgID == "" {
		for _, u := range f.users {
			if email == "" || u.Email == email {
				all = append(all, u)
			}
		}
	} else {
		for _, userID := range f.orgUsers[orgID] {
			if u, ok := f.users[userID]; ok {
				all = append(all, u)
			}
		}
	}
	f.mu.Unlock()

	writeJSON(w, map[string]any{
		"data":          all,
		"list_metadata": common.ListMetadata{},
	})
}

func (f *fakeWorkOS) handleSendInvitation(w http.ResponseWriter, r *http.Request) {
	var opts workos.SendInvitationOpts
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	invite := usermanagement.Invitation{
		ID:                  fmt.Sprintf("inv_%d", len(f.invitations)+1),
		Email:               opts.Email,
		State:               usermanagement.Pending,
		Token:               fmt.Sprintf("token_%d", len(f.invitations)+1),
		AcceptInvitationUrl: fmt.Sprintf("https://example.com/invite/%d", len(f.invitations)+1),
		OrganizationID:      opts.OrganizationID,
		InviterUserID:       opts.InviterUserID,
		ExpiresAt:           "2026-04-02T00:00:00Z",
		CreatedAt:           "2026-03-26T00:00:00Z",
		UpdatedAt:           "2026-03-26T00:00:00Z",
	}
	f.invitations = append(f.invitations, invite)

	writeJSON(w, invite)
}

func (f *fakeWorkOS) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("organization_id")
	afterCursor := r.URL.Query().Get("after")

	f.mu.Lock()
	var filtered []usermanagement.Invitation
	for _, inv := range f.invitations {
		if inv.OrganizationID == orgID {
			filtered = append(filtered, inv)
		}
	}
	pageSize := f.invitePageSize
	f.mu.Unlock()

	start := 0
	if afterCursor != "" {
		for i, inv := range filtered {
			if inv.ID == afterCursor {
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

func (f *fakeWorkOS) handleGetInvitation(w http.ResponseWriter, invitationID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, inv := range f.invitations {
		if inv.ID == invitationID {
			writeJSON(w, inv)
			return
		}
	}
	http.Error(w, "invitation not found", http.StatusNotFound)
}

func (f *fakeWorkOS) handleFindInvitationByToken(w http.ResponseWriter, token string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, inv := range f.invitations {
		if inv.Token == token {
			writeJSON(w, inv)
			return
		}
	}
	http.Error(w, "invitation not found", http.StatusNotFound)
}

func (f *fakeWorkOS) handleRevokeInvitation(w http.ResponseWriter, invitationID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, inv := range f.invitations {
		if inv.ID == invitationID {
			f.invitations[i].State = usermanagement.Revoked
			f.invitations[i].RevokedAt = "2026-03-27T00:00:00Z"
			writeJSON(w, f.invitations[i])
			return
		}
	}
	http.Error(w, "invitation not found", http.StatusNotFound)
}

func (f *fakeWorkOS) handleResendInvitation(w http.ResponseWriter, invitationID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, inv := range f.invitations {
		if inv.ID == invitationID {
			f.invitations[i].UpdatedAt = "2026-03-27T00:00:00Z"
			writeJSON(w, f.invitations[i])
			return
		}
	}
	http.Error(w, "invitation not found", http.StatusNotFound)
}

func (f *fakeWorkOS) handleDeleteMembership(w http.ResponseWriter, membershipID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, m := range f.memberships {
		if m.ID == membershipID {
			f.memberships = append(f.memberships[:i], f.memberships[i+1:]...)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	http.Error(w, "membership not found", http.StatusNotFound)
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

func newTestClient(t *testing.T, fake *fakeWorkOS) (*workos.Client, *fakeWorkOS) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	client := workos.NewClient(guardianPolicy, "test-api-key", workos.ClientOpts{
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
		{ID: "role_a", Name: "Admin", Slug: "admin", Type: "EnvironmentRole", CreatedAt: "2026-04-04T12:30:00Z", UpdatedAt: "2026-04-05T10:43:00Z"},
		{ID: "role_b", Name: "Editor", Slug: "org-editor", Type: "OrganizationRole", CreatedAt: "2026-04-04T12:30:00Z", UpdatedAt: "2026-04-05T10:43:00Z"},
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
		{ID: "role_1", Name: "Old Name", Slug: "org-old", Type: "OrganizationRole", CreatedAt: "2026-04-04T12:30:00Z", UpdatedAt: "2026-04-05T10:43:00Z"},
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

func TestRoleClient_ListUsersInOrg(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.users["user_1"] = common.User{ID: "user_1", FirstName: "Alice", Email: "alice@example.com"}
	fake.users["user_2"] = common.User{ID: "user_2", FirstName: "Bob", Email: "bob@example.com"}
	fake.orgUsers["org_1"] = []string{"user_1", "user_2"}
	client, _ := newTestClient(t, fake)

	users, err := client.ListUsersInOrg(context.Background(), "org_1")
	require.NoError(t, err)
	require.Len(t, users, 2)
	require.Equal(t, "Alice", users[0].FirstName)
	require.Equal(t, "Bob", users[1].FirstName)
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

func TestRoleClient_GetUserByEmail(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.users["user_1"] = common.User{
		ID:        "user_1",
		FirstName: "Jane",
		LastName:  "Doe",
		Email:     "jane@example.com",
	}
	client, _ := newTestClient(t, fake)

	user, err := client.GetUserByEmail(context.Background(), "jane@example.com")
	require.NoError(t, err)
	require.Equal(t, "user_1", user.ID)
	require.Equal(t, "Jane", user.FirstName)
}

func TestRoleClient_ListUserMemberships(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.memberships = []usermanagement.OrganizationMembership{
		{ID: "mem_1", UserID: "user_1", OrganizationID: "org_1", Role: common.RoleResponse{Slug: "admin"}, Status: usermanagement.Active},
		{ID: "mem_2", UserID: "user_1", OrganizationID: "org_2", Role: common.RoleResponse{Slug: "member"}, Status: usermanagement.Active},
		{ID: "mem_3", UserID: "user_2", OrganizationID: "org_1", Role: common.RoleResponse{Slug: "member"}, Status: usermanagement.Active},
	}
	client, _ := newTestClient(t, fake)

	// Should return only memberships for user_1 across all orgs in one call.
	memberships, err := client.ListUserMemberships(context.Background(), "user_1")
	require.NoError(t, err)
	require.Len(t, memberships, 2)
	require.Equal(t, "mem_1", memberships[0].ID)
	require.Equal(t, "org_1", memberships[0].OrganizationID)
	require.Equal(t, "mem_2", memberships[1].ID)
	require.Equal(t, "org_2", memberships[1].OrganizationID)
}

func TestRoleClient_GetOrgMembership(t *testing.T) {
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
		{
			ID:             "mem_2",
			UserID:         "user_2",
			OrganizationID: "org_1",
			Role:           common.RoleResponse{Slug: "member"},
			Status:         usermanagement.Active,
		},
	}
	client, _ := newTestClient(t, fake)

	membership, err := client.GetOrgMembership(context.Background(), "user_1", "org_1")
	require.NoError(t, err)
	require.Equal(t, "mem_1", membership.ID)
	require.Equal(t, "admin", membership.RoleSlug)
}

func TestRoleClient_SendInvitation(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, newFakeWorkOS())

	invite, err := client.SendInvitation(context.Background(), workos.SendInvitationOpts{
		Email:          "invitee@example.com",
		OrganizationID: "org_1",
		InviterUserID:  "user_1",
		ExpiresInDays:  7,
	})
	require.NoError(t, err)
	require.Equal(t, "invitee@example.com", invite.Email)
	require.Equal(t, workos.InvitationStatePending, invite.State)
	require.Equal(t, "org_1", invite.OrganizationID)
}

func TestRoleClient_ListInvitations(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.invitations = []usermanagement.Invitation{
		{ID: "inv_1", Email: "alice@example.com", State: usermanagement.Pending, OrganizationID: "org_1", Token: "tok_1"},
		{ID: "inv_2", Email: "bob@example.com", State: usermanagement.Accepted, OrganizationID: "org_1", Token: "tok_2"},
	}
	client, _ := newTestClient(t, fake)

	invites, err := client.ListInvitations(context.Background(), "org_1")
	require.NoError(t, err)
	require.Len(t, invites, 2)
	require.Equal(t, workos.InvitationStatePending, invites[0].State)
	require.Equal(t, workos.InvitationStateAccepted, invites[1].State)
}

func TestRoleClient_RevokeInvitation(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.invitations = []usermanagement.Invitation{{ID: "inv_1", Email: "alice@example.com", State: usermanagement.Pending, OrganizationID: "org_1", Token: "tok_1"}}
	client, _ := newTestClient(t, fake)

	invite, err := client.RevokeInvitation(context.Background(), "inv_1")
	require.NoError(t, err)
	require.Equal(t, workos.InvitationStateRevoked, invite.State)
	require.NotEmpty(t, invite.RevokedAt)
}

func TestRoleClient_ResendInvitation(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.invitations = []usermanagement.Invitation{{ID: "inv_1", Email: "alice@example.com", State: usermanagement.Pending, OrganizationID: "org_1", Token: "tok_1", UpdatedAt: "2026-03-26T00:00:00Z"}}
	client, _ := newTestClient(t, fake)

	invite, err := client.ResendInvitation(context.Background(), "inv_1")
	require.NoError(t, err)
	require.Equal(t, "2026-03-27T00:00:00Z", invite.UpdatedAt)
}

func TestRoleClient_FindInvitationByToken(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.invitations = []usermanagement.Invitation{{ID: "inv_1", Email: "alice@example.com", State: usermanagement.Pending, OrganizationID: "org_1", Token: "tok_1"}}
	client, _ := newTestClient(t, fake)

	invite, err := client.FindInvitationByToken(context.Background(), "tok_1")
	require.NoError(t, err)
	require.Equal(t, "inv_1", invite.ID)
	require.Equal(t, "tok_1", invite.Token)
}

func TestRoleClient_FindInvitationByToken_NotFound(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	client, _ := newTestClient(t, fake)

	invite, err := client.FindInvitationByToken(context.Background(), "nonexistent-token")
	require.True(t, workos.IsNotFound(err))
	require.Nil(t, invite)
}

func TestRoleClient_GetInvitation(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.invitations = []usermanagement.Invitation{{ID: "inv_1", Email: "alice@example.com", State: usermanagement.Pending, OrganizationID: "org_1", Token: "tok_1"}}
	client, _ := newTestClient(t, fake)

	invite, err := client.GetInvitation(context.Background(), "inv_1")
	require.NoError(t, err)
	require.Equal(t, "alice@example.com", invite.Email)
}

func TestRoleClient_GetInvitation_NotFound(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	client, _ := newTestClient(t, fake)

	invite, err := client.GetInvitation(context.Background(), "nonexistent-id")
	require.True(t, workos.IsNotFound(err))
	require.Nil(t, invite)
}

func TestRoleClient_DeleteOrganizationMembership(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.memberships = []usermanagement.OrganizationMembership{{ID: "mem_1", UserID: "user_1", OrganizationID: "org_1", Status: usermanagement.Active}}
	client, _ := newTestClient(t, fake)

	err := client.DeleteOrganizationMembership(context.Background(), "mem_1")
	require.NoError(t, err)
	require.Empty(t, fake.memberships)
}

func TestRoleClient_ListOrgMemberships(t *testing.T) {
	t.Parallel()
	fake := newFakeWorkOS()
	fake.memberships = []usermanagement.OrganizationMembership{
		{ID: "mem_1", UserID: "user_1", OrganizationID: "org_1", OrganizationName: "Org One", Role: common.RoleResponse{Slug: "admin"}, Status: usermanagement.Active, CreatedAt: "2026-03-26T00:00:00Z", UpdatedAt: "2026-03-26T00:00:00Z"},
		{ID: "mem_2", UserID: "user_2", OrganizationID: "org_1", OrganizationName: "Org One", Role: common.RoleResponse{Slug: "member"}, Status: usermanagement.Inactive, CreatedAt: "2026-03-27T00:00:00Z", UpdatedAt: "2026-03-27T00:00:00Z"},
	}
	client, _ := newTestClient(t, fake)

	memberships, err := client.ListOrgMemberships(context.Background(), "org_1")
	require.NoError(t, err)
	require.Len(t, memberships, 2)
	require.Equal(t, "Org One", memberships[0].Organization)
	require.Equal(t, "active", memberships[0].Status)
	require.Equal(t, "inactive", memberships[1].Status)
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
