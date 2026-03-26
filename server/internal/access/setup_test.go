package access_test

import (
	"context"
	"fmt"
	"log"
	"maps"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/common"
	"github.com/workos/workos-go/v6/pkg/roles"
	"github.com/workos/workos-go/v6/pkg/usermanagement"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

// mockRoleProvider is an in-memory implementation of access.RoleProvider for
// tests. It stores roles and memberships in maps so tests can exercise the
// service layer without hitting WorkOS.
type mockRoleProvider struct {
	mu      sync.Mutex
	roles   map[string][]roles.Role // orgID → roles
	members map[string][]usermanagement.OrganizationMembership
	users   map[string]usermanagement.User
	nextID  int
}

func newMockRoleProvider() *mockRoleProvider {
	return &mockRoleProvider{
		roles:   make(map[string][]roles.Role),
		members: make(map[string][]usermanagement.OrganizationMembership),
		users:   make(map[string]usermanagement.User),
	}
}

func (m *mockRoleProvider) ListRoles(_ context.Context, orgID string) ([]roles.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.roles[orgID], nil
}

func (m *mockRoleProvider) CreateRole(_ context.Context, orgID string, opts workos.CreateRoleOpts) (*roles.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID++
	r := roles.Role{
		ID:          fmt.Sprintf("role_%d", m.nextID),
		Name:        opts.Name,
		Slug:        opts.Slug,
		Description: opts.Description,
		Type:        "OrganizationRole",
		CreatedAt:   "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-01-01T00:00:00Z",
	}
	m.roles[orgID] = append(m.roles[orgID], r)
	return &r, nil
}

func (m *mockRoleProvider) UpdateRole(_ context.Context, orgID string, roleSlug string, opts workos.UpdateRoleOpts) (*roles.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, r := range m.roles[orgID] {
		if r.Slug == roleSlug {
			if opts.Name != nil {
				m.roles[orgID][i].Name = *opts.Name
			}
			if opts.Description != nil {
				m.roles[orgID][i].Description = *opts.Description
			}
			m.roles[orgID][i].UpdatedAt = "2026-01-02T00:00:00Z"
			updated := m.roles[orgID][i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("role %q not found", roleSlug)
}

func (m *mockRoleProvider) DeleteRole(_ context.Context, orgID string, roleSlug string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rr := m.roles[orgID]
	for i, r := range rr {
		if r.Slug == roleSlug {
			m.roles[orgID] = append(rr[:i], rr[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("role %q not found", roleSlug)
}

func (m *mockRoleProvider) ListMembers(_ context.Context, orgID string) ([]usermanagement.OrganizationMembership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.members[orgID], nil
}

func (m *mockRoleProvider) UpdateMemberRole(_ context.Context, membershipID string, roleSlug string) (*usermanagement.OrganizationMembership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for orgID, members := range m.members {
		for i, mem := range members {
			if mem.ID == membershipID {
				m.members[orgID][i].Role = common.RoleResponse{Slug: roleSlug}
				updated := m.members[orgID][i]
				return &updated, nil
			}
		}
	}
	return nil, fmt.Errorf("membership %q not found", membershipID)
}

func (m *mockRoleProvider) GetUser(_ context.Context, userID string) (*usermanagement.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.users[userID]
	if !ok {
		return nil, fmt.Errorf("user %q not found", userID)
	}
	return &u, nil
}

func (m *mockRoleProvider) ListOrgUsers(_ context.Context, _ string) (map[string]usermanagement.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make(map[string]usermanagement.User, len(m.users))
	maps.Copy(cp, m.users)
	return cp, nil
}

// addMember injects a membership into the mock.
func (m *mockRoleProvider) addMember(orgID, membershipID, userID, roleSlug string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.members[orgID] = append(m.members[orgID], usermanagement.OrganizationMembership{
		ID:             membershipID,
		UserID:         userID,
		OrganizationID: orgID,
		Role:           common.RoleResponse{Slug: roleSlug},
		Status:         usermanagement.Active,
		CreatedAt:      "2026-01-01T00:00:00Z",
		UpdatedAt:      "2026-01-01T00:00:00Z",
	})
}

// addSystemRole adds a built-in "EnvironmentRole" to the mock (e.g. "member").
func (m *mockRoleProvider) addSystemRole(orgID, id, name, slug string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.roles[orgID] = append(m.roles[orgID], roles.Role{
		ID:        id,
		Name:      name,
		Slug:      slug,
		Type:      "EnvironmentRole",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	})
}

type testInstance struct {
	service *access.Service
	conn    *pgxpool.Pool
	mock    *mockRoleProvider
}

func newTestAccessService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	// Set a fake workos_id on the test org so workosOrgID() resolves.
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgQueries := orgrepo.New(conn)
	err = orgQueries.SetOrgWorkosID(ctx, orgrepo.SetOrgWorkosIDParams{
		WorkosID: pgtype.Text{String: "org_workos_test", Valid: true},
		ID:       ac.ActiveOrganizationID,
	})
	require.NoError(t, err)

	mock := newMockRoleProvider()
	svc := access.NewService(logger, tracerProvider, conn, sessionManager, mock)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
		mock:    mock,
	}
}
