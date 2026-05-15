package workos

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/workos/workos-go/v6/pkg/common"
	"github.com/workos/workos-go/v6/pkg/events"
)

type StubClient struct {
	mut                   sync.Mutex
	orgs                  map[string]*stubOrgState
	orgOrder              []string
	globalRoles           map[string]Role
	globalOrder           []string
	eventPages            [][]events.Event
	eventCalls            []events.ListEventsOpts
	userExternalIDUpdates []UserExternalIDUpdate
	next                  int
	nowFn                 func() time.Time
}

type UserExternalIDUpdate struct {
	WorkOSUserID string
	ExternalID   string
}

type stubOrgState struct {
	organization Organization
	roles        map[string]Role
	roleOrder    []string
	memberships  map[string]Member
	users        map[string]User
	invites      map[string]Invitation
	inviteOrder  []string
}

func NewStubClient() *StubClient {
	return &StubClient{
		mut:                   sync.Mutex{},
		orgs:                  make(map[string]*stubOrgState),
		orgOrder:              make([]string, 0),
		globalRoles:           stubDefaultGlobalRoles(),
		globalOrder:           []string{"admin", "member"},
		eventPages:            nil,
		eventCalls:            make([]events.ListEventsOpts, 0),
		userExternalIDUpdates: make([]UserExternalIDUpdate, 0),
		next:                  1,
		nowFn:                 time.Now,
	}
}

func (s *StubClient) UpsertOrganization(org Organization) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(org.ID)
	state.organization = org
}

func (s *StubClient) GetOrganization(_ context.Context, orgID string) (*Organization, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state, ok := s.orgs[orgID]
	if !ok {
		return nil, &APIError{Method: "GET", Path: "/stub/organizations/" + orgID, StatusCode: 404, Body: "organization not found"}
	}

	org := state.organization
	return &org, nil
}

func (s *StubClient) ListOrganizations(_ context.Context) ([]Organization, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	orgs := make([]Organization, 0, len(s.orgOrder))
	for _, orgID := range s.orgOrder {
		orgs = append(orgs, s.orgs[orgID].organization)
	}

	return orgs, nil
}

func (s *StubClient) SetEventPages(pages [][]events.Event) {
	s.mut.Lock()
	defer s.mut.Unlock()

	s.eventPages = append([][]events.Event(nil), pages...)
	s.eventCalls = s.eventCalls[:0]
}

func (s *StubClient) EventCalls() []events.ListEventsOpts {
	s.mut.Lock()
	defer s.mut.Unlock()

	return append([]events.ListEventsOpts(nil), s.eventCalls...)
}

func (s *StubClient) UserExternalIDUpdates() []UserExternalIDUpdate {
	s.mut.Lock()
	defer s.mut.Unlock()

	return append([]UserExternalIDUpdate(nil), s.userExternalIDUpdates...)
}

func (s *StubClient) ListEvents(_ context.Context, opts events.ListEventsOpts) (events.ListEventsResponse, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	s.eventCalls = append(s.eventCalls, opts)

	idx := len(s.eventCalls) - 1
	if idx >= len(s.eventPages) {
		return events.ListEventsResponse{Data: nil, ListMetadata: common.ListMetadata{Before: "", After: ""}}, nil
	}

	return events.ListEventsResponse{Data: s.eventPages[idx], ListMetadata: common.ListMetadata{Before: "", After: ""}}, nil
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
	role := Role{ID: s.nextRoleID(), Name: opts.Name, Slug: opts.Slug, Description: opts.Description, Type: "OrganizationRole", CreatedAt: now, UpdatedAt: now}
	state.roles[role.Slug] = role
	state.roleOrder = append(state.roleOrder, role.Slug)

	return &role, nil
}

func (s *StubClient) ListGlobalRoles(_ context.Context) ([]Role, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	roles := make([]Role, 0, len(s.globalOrder))
	for _, slug := range s.globalOrder {
		roles = append(roles, s.globalRoles[slug])
	}

	return roles, nil
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

func (s *StubClient) UpsertOrganizationMembership(member Member) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(member.OrganizationID)
	state.memberships[member.ID] = member
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

func (s *StubClient) UpdateUserExternalID(_ context.Context, workosUserID, externalID string) error {
	s.mut.Lock()
	defer s.mut.Unlock()

	s.userExternalIDUpdates = append(s.userExternalIDUpdates, UserExternalIDUpdate{WorkOSUserID: workosUserID, ExternalID: externalID})
	return nil
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

func (s *StubClient) CreateOrganization(_ context.Context, name, gramOrgID string) (string, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	workosOrgID := fmt.Sprintf("org_%s", gramOrgID)
	s.orgState(workosOrgID) // initialize

	return workosOrgID, nil
}

func (s *StubClient) CreateOrganizationMembership(_ context.Context, workosUserID, workosOrgID string) error {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(workosOrgID)
	membershipID := fmt.Sprintf("om_%d", s.next)
	s.next++
	state.memberships[membershipID] = Member{
		ID:             membershipID,
		UserID:         workosUserID,
		OrganizationID: workosOrgID,
		Organization:   "",
		RoleSlug:       "admin",
		Status:         "active",
		CreatedAt:      "",
		UpdatedAt:      "",
	}

	return nil
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
		organization: Organization{ID: orgID, Name: orgID, ExternalID: "", CreatedAt: stubRoleTimestamp, UpdatedAt: stubRoleTimestamp},
		roles: map[string]Role{
			"admin":  {ID: "role_admin", Name: "Admin", Slug: "admin", Description: "", Type: "EnvironmentRole", CreatedAt: stubRoleTimestamp, UpdatedAt: stubRoleTimestamp},
			"member": {ID: "role_member", Name: "Member", Slug: "member", Description: "", Type: "EnvironmentRole", CreatedAt: stubRoleTimestamp, UpdatedAt: stubRoleTimestamp},
		},
		roleOrder:   []string{"admin", "member"},
		memberships: make(map[string]Member),
		users:       make(map[string]User),
		invites:     make(map[string]Invitation),
		inviteOrder: nil,
	}
	s.orgs[orgID] = state
	s.orgOrder = append(s.orgOrder, orgID)

	return state
}

func stubDefaultGlobalRoles() map[string]Role {
	return map[string]Role{
		"admin":  {ID: "role_admin", Name: "Admin", Slug: "admin", Description: "", Type: "EnvironmentRole", CreatedAt: stubRoleTimestamp, UpdatedAt: stubRoleTimestamp},
		"member": {ID: "role_member", Name: "Member", Slug: "member", Description: "", Type: "EnvironmentRole", CreatedAt: stubRoleTimestamp, UpdatedAt: stubRoleTimestamp},
	}
}

func (s *StubClient) nextRoleID() string {
	id := fmt.Sprintf("stub_role_%d", s.next)
	s.next++
	return id
}

const stubRoleTimestamp = "2024-12-28T07:55:09Z"
