package workos

import (
	"context"
	"maps"
	"os"
	"strconv"
	"sync"
)

type MockRoleProvider struct {
	mu              sync.Mutex
	roles           map[string][]Role
	members         map[string][]Member
	users           map[string]User
	nextID          int
	errCreateRole   error
	errDeleteRole   error
	errUpdateRole   error
	errListRoles    error
	errListMembers  error
	errListOrgUsers error
	afterCreateRole func()
}

func NewMockRoleProvider() *MockRoleProvider {
	return &MockRoleProvider{
		mu:              sync.Mutex{},
		roles:           make(map[string][]Role),
		members:         make(map[string][]Member),
		users:           make(map[string]User),
		nextID:          0,
		errCreateRole:   nil,
		errDeleteRole:   nil,
		errUpdateRole:   nil,
		errListRoles:    nil,
		errListMembers:  nil,
		errListOrgUsers: nil,
		afterCreateRole: nil,
	}
}

func (m *MockRoleProvider) ListRoles(_ context.Context, orgID string) ([]Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errListRoles != nil {
		return nil, m.errListRoles
	}

	return append([]Role(nil), m.roles[orgID]...), nil
}

func (m *MockRoleProvider) CreateRole(_ context.Context, orgID string, opts CreateRoleOpts) (*Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errCreateRole != nil {
		return nil, m.errCreateRole
	}

	m.nextID++
	role := Role{ID: "role_" + strconv.Itoa(m.nextID), Name: opts.Name, Slug: opts.Slug, Description: opts.Description}
	m.roles[orgID] = append(m.roles[orgID], role)
	afterCreateRole := m.afterCreateRole
	created := m.roles[orgID][len(m.roles[orgID])-1]
	if afterCreateRole != nil {
		afterCreateRole()
	}
	return &created, nil
}

func (m *MockRoleProvider) UpdateRole(_ context.Context, orgID string, roleSlug string, opts UpdateRoleOpts) (*Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errUpdateRole != nil {
		return nil, m.errUpdateRole
	}

	for i, role := range m.roles[orgID] {
		if role.Slug != roleSlug {
			continue
		}
		if opts.Name != nil {
			m.roles[orgID][i].Name = *opts.Name
		}
		if opts.Description != nil {
			m.roles[orgID][i].Description = *opts.Description
		}
		updated := m.roles[orgID][i]
		return &updated, nil
	}

	return nil, os.ErrNotExist
}

func (m *MockRoleProvider) DeleteRole(_ context.Context, orgID string, roleSlug string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errDeleteRole != nil {
		return m.errDeleteRole
	}

	roles := m.roles[orgID]
	for i, role := range roles {
		if role.Slug != roleSlug {
			continue
		}
		m.roles[orgID] = append(roles[:i], roles[i+1:]...)
		return nil
	}

	return os.ErrNotExist
}

func (m *MockRoleProvider) ListMembers(_ context.Context, orgID string) ([]Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errListMembers != nil {
		return nil, m.errListMembers
	}

	return append([]Member(nil), m.members[orgID]...), nil
}

func (m *MockRoleProvider) UpdateMemberRole(_ context.Context, membershipID string, roleSlug string) (*Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for orgID, members := range m.members {
		for i, member := range members {
			if member.ID != membershipID {
				continue
			}
			m.members[orgID][i].RoleSlug = roleSlug
			updated := m.members[orgID][i]
			return &updated, nil
		}
	}

	return nil, os.ErrNotExist
}

func (m *MockRoleProvider) GetUser(_ context.Context, userID string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, ok := m.users[userID]
	if !ok {
		return nil, os.ErrNotExist
	}

	return &user, nil
}

func (m *MockRoleProvider) ListOrgUsers(_ context.Context, _ string) (map[string]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errListOrgUsers != nil {
		return nil, m.errListOrgUsers
	}

	users := make(map[string]User, len(m.users))
	maps.Copy(users, m.users)
	return users, nil
}

func (m *MockRoleProvider) AddRole(orgID string, role Role) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.roles[orgID] = append(m.roles[orgID], role)
}

func (m *MockRoleProvider) AddSystemRole(orgID, id, name, slug string) {
	m.AddRole(orgID, Role{ID: id, Name: name, Slug: slug, Description: ""})
}

func (m *MockRoleProvider) AddMember(orgID, membershipID, userID, roleSlug string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.members[orgID] = append(m.members[orgID], Member{ID: membershipID, UserID: userID, OrganizationID: orgID, RoleSlug: roleSlug})
}

func (m *MockRoleProvider) AddUser(user User) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.users[user.ID] = user
}

func (m *MockRoleProvider) SetListRolesError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errListRoles = err
}

func (m *MockRoleProvider) SetCreateRoleError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errCreateRole = err
}

func (m *MockRoleProvider) SetUpdateRoleError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errUpdateRole = err
}

func (m *MockRoleProvider) SetDeleteRoleError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errDeleteRole = err
}

func (m *MockRoleProvider) SetAfterCreateRole(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.afterCreateRole = fn
}

func (m *MockRoleProvider) SetListMembersError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errListMembers = err
}

func (m *MockRoleProvider) SetListOrgUsersError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errListOrgUsers = err
}
