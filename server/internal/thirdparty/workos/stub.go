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
	invites     map[string]Invitation
	inviteOrder []string
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

func (s *StubClient) SendInvitation(_ context.Context, opts SendInvitationOpts) (*Invitation, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(opts.OrganizationID)
	now := s.nowFn().UTC().Format(time.RFC3339)
	invite := Invitation{
		ID:                  s.nextInviteID(),
		Email:               opts.Email,
		State:               InvitationStatePending,
		AcceptedAt:          "",
		RevokedAt:           "",
		Token:               fmt.Sprintf("token_%d", s.next),
		AcceptInvitationURL: "",
		OrganizationID:      opts.OrganizationID,
		InviterUserID:       opts.InviterUserID,
		ExpiresAt:           now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	state.invites[invite.ID] = invite
	state.inviteOrder = append(state.inviteOrder, invite.ID)

	return &invite, nil
}

func (s *StubClient) ListInvitations(_ context.Context, orgID string) ([]Invitation, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	state := s.orgState(orgID)
	invites := make([]Invitation, 0, len(state.inviteOrder))
	for _, inviteID := range state.inviteOrder {
		invites = append(invites, state.invites[inviteID])
	}

	return invites, nil
}

func (s *StubClient) RevokeInvitation(_ context.Context, invitationID string) (*Invitation, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	for _, state := range s.orgs {
		invite, ok := state.invites[invitationID]
		if !ok {
			continue
		}
		now := s.nowFn().UTC().Format(time.RFC3339)
		invite.State = InvitationStateRevoked
		invite.RevokedAt = now
		invite.UpdatedAt = now
		state.invites[invitationID] = invite
		return &invite, nil
	}

	return nil, fmt.Errorf("invitation %q not found", invitationID)
}

func (s *StubClient) ResendInvitation(_ context.Context, invitationID string) (*Invitation, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	for _, state := range s.orgs {
		invite, ok := state.invites[invitationID]
		if !ok {
			continue
		}
		invite.UpdatedAt = s.nowFn().UTC().Format(time.RFC3339)
		state.invites[invitationID] = invite
		return &invite, nil
	}

	return nil, fmt.Errorf("invitation %q not found", invitationID)
}

func (s *StubClient) FindInvitationByToken(_ context.Context, token string) (*Invitation, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	for _, state := range s.orgs {
		for _, invite := range state.invites {
			if invite.Token == token {
				return &invite, nil
			}
		}
	}

	return nil, nil
}

func (s *StubClient) GetInvitation(_ context.Context, invitationID string) (*Invitation, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	for _, state := range s.orgs {
		invite, ok := state.invites[invitationID]
		if ok {
			return &invite, nil
		}
	}

	return nil, nil
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
		invites:     make(map[string]Invitation),
		inviteOrder: make([]string, 0),
	}
	s.orgs[orgID] = state

	return state
}

func (s *StubClient) nextRoleID() string {
	id := fmt.Sprintf("stub_role_%d", s.next)
	s.next++
	return id
}

func (s *StubClient) nextInviteID() string {
	id := fmt.Sprintf("stub_invite_%d", s.next)
	s.next++
	return id
}

const stubRoleTimestamp = "2024-12-28T07:55:09Z"
