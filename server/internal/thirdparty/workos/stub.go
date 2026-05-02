package workos

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"
)

type StubClient struct {
	mut   sync.Mutex
	orgs  map[string]*stubOrgState
	next  int
	nowFn func() time.Time
}

type stubOrgState struct {
	roles       map[string]Role
	roleOrder   []string
	memberships map[string]Member
	users       map[string]User
}

func NewStubClient() *StubClient {
	return &StubClient{
		mut:   sync.Mutex{},
		orgs:  make(map[string]*stubOrgState),
		next:  1,
		nowFn: time.Now,
	}
}

func (s *StubClient) ListRoles(_ context.Context, orgID string) ([]Role, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(orgID)
	roles := make([]Role, 0, len(state.roleOrder))
	for _, slug := range state.roleOrder {
		roles = append(roles, state.roles[slug])
	}

	return roles, nil
}

func (s *StubClient) CreateRole(_ context.Context, orgID string, opts CreateRoleOpts) (*Role, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(orgID)
	if _, exists := state.roles[opts.Slug]; exists {
		return nil, &APIError{Method: "POST", Path: "/stub/roles", StatusCode: 409, Body: "role already exists"}
	}

	now := s.nowFn().UTC().Format(time.RFC3339)
	role := Role{ID: s.nextRoleID(), Name: opts.Name, Slug: opts.Slug, Description: opts.Description, CreatedAt: now, UpdatedAt: now}
	state.roles[role.Slug] = role
	state.roleOrder = append(state.roleOrder, role.Slug)

	return &role, nil
}

func (s *StubClient) UpdateRole(_ context.Context, orgID string, roleSlug string, opts UpdateRoleOpts) (*Role, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(orgID)
	role, ok := state.roles[roleSlug]
	if !ok {
		return nil, fmt.Errorf("role %q not found", roleSlug)
	}
	if opts.Name != nil {
		role.Name = *opts.Name
	}
	if opts.Description != nil {
		role.Description = *opts.Description
	}
	role.UpdatedAt = s.nowFn().UTC().Format(time.RFC3339)
	state.roles[roleSlug] = role

	return &role, nil
}

func (s *StubClient) DeleteRole(_ context.Context, orgID string, roleSlug string) error {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(orgID)
	if _, ok := state.roles[roleSlug]; !ok {
		return fmt.Errorf("role %q not found", roleSlug)
	}
	delete(state.roles, roleSlug)
	for i, slug := range state.roleOrder {
		if slug == roleSlug {
			state.roleOrder = append(state.roleOrder[:i], state.roleOrder[i+1:]...)
			break
		}
	}
	for membershipID, member := range state.memberships {
		if member.RoleSlug == roleSlug {
			member.RoleSlug = "member"
			state.memberships[membershipID] = member
		}
	}

	return nil
}

func (s *StubClient) ListMembers(_ context.Context, orgID string) ([]Member, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(orgID)
	members := make([]Member, 0, len(state.memberships))
	for _, member := range state.memberships {
		members = append(members, member)
	}

	return members, nil
}

func (s *StubClient) UpdateMemberRole(_ context.Context, membershipID string, roleSlug string) (*Member, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	for _, state := range s.orgs {
		member, ok := state.memberships[membershipID]
		if !ok {
			continue
		}
		if _, ok := state.roles[roleSlug]; !ok {
			return nil, fmt.Errorf("role %q not found", roleSlug)
		}
		member.RoleSlug = roleSlug
		state.memberships[membershipID] = member
		return &member, nil
	}

	return nil, fmt.Errorf("membership %q not found", membershipID)
}

func (s *StubClient) GetUser(_ context.Context, userID string) (*User, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	for _, state := range s.orgs {
		user, ok := state.users[userID]
		if ok {
			return &user, nil
		}
	}

	return nil, nil
}

func (s *StubClient) ListUsersInOrg(_ context.Context, orgID string) ([]User, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(orgID)
	users := make([]User, 0, len(state.users))
	for _, user := range state.users {
		users = append(users, user)
	}

	return users, nil
}

func (s *StubClient) CreatePasswordlessSession(_ context.Context, opts CreatePasswordlessSessionOpts) (*PasswordlessSession, error) {
	return &PasswordlessSession{
		ID:        fmt.Sprintf("stub_pwl_%d", s.next),
		Email:     opts.Email,
		ExpiresAt: s.nowFn().UTC().Add(time.Duration(opts.ExpiresIn) * time.Second).Format(time.RFC3339),
		Link:      fmt.Sprintf("https://stub.workos.com/passwordless/%d", s.next),
	}, nil
}

func (s *StubClient) AuthenticateWithInviteLink(_ context.Context, _ string) (*InviteLinkProfile, error) {
	return &InviteLinkProfile{
		Email:     "stub@example.com",
		FirstName: "Stub",
		LastName:  "User",
	}, nil
}

func (s *StubClient) DeleteOrganizationMembership(_ context.Context, membershipID string) error {
	s.mut.Lock()
	defer s.mut.Unlock()

	for _, state := range s.orgs {
		if _, ok := state.memberships[membershipID]; ok {
			delete(state.memberships, membershipID)
			return nil
		}
	}

	return fmt.Errorf("membership %q not found", membershipID)
}

func (s *StubClient) ListOrgMemberships(_ context.Context, orgID string) ([]Member, error) {
	return s.ListMembers(context.Background(), orgID)
}

func (s *StubClient) GetOrgMembership(_ context.Context, workOSUserID, workOSOrgID string) (*Member, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state, ok := s.orgs[workOSOrgID]
	if !ok {
		return nil, nil
	}
	for _, m := range state.memberships {
		if m.UserID == workOSUserID && m.OrganizationID == workOSOrgID {
			cp := m
			return &cp, nil
		}
	}

	return nil, nil
}

func (s *StubClient) ListOrgUsers(_ context.Context, orgID string) (map[string]User, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(orgID)
	users := make(map[string]User, len(state.users))
	maps.Copy(users, state.users)

	return users, nil
}

func (s *StubClient) GetUserByEmail(_ context.Context, email string) (*User, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	for _, state := range s.orgs {
		for _, user := range state.users {
			if user.Email == email {
				return &user, nil
			}
		}
	}

	return nil, nil
}

func (s *StubClient) orgState(orgID string) *stubOrgState {
	state, ok := s.orgs[orgID]
	if ok {
		return state
	}

	state = &stubOrgState{
		roles: map[string]Role{
			"admin":  {ID: "role_admin", Name: "Admin", Slug: "admin", Description: "", CreatedAt: stubRoleTimestamp, UpdatedAt: stubRoleTimestamp},
			"member": {ID: "role_member", Name: "Member", Slug: "member", Description: "", CreatedAt: stubRoleTimestamp, UpdatedAt: stubRoleTimestamp},
		},
		roleOrder:   []string{"admin", "member"},
		memberships: make(map[string]Member),
		users:       make(map[string]User),
	}
	s.orgs[orgID] = state

	return state
}

func (s *StubClient) nextRoleID() string {
	id := fmt.Sprintf("stub_role_%d", s.next)
	s.next++
	return id
}

const stubRoleTimestamp = "2024-12-28T07:55:09Z"
