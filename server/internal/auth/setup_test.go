package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
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

type testInstance struct {
	service        *auth.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	mockAuthServer *httptest.Server
	authConfigs    auth.AuthConfigurations
}

// MockUserInfo represents the user info used by the mock OIDC server
type MockUserInfo struct {
	UserID          string
	Email           string
	Admin           bool
	UserWhitelisted bool
	Organizations   []MockOrganizationEntry
}

type MockOrganizationEntry struct {
	ID                 string
	Name               string
	Slug               string
	WorkosID           *string
	UserWorkspaceSlugs []string
}

// createMockOIDCServer creates an httptest.Server that serves mock OIDC responses.
// It returns access tokens that map back to the provided userInfo.
func createMockOIDCServer(userInfo *MockUserInfo) *httptest.Server {
	mux := http.NewServeMux()
	accessToken := fmt.Sprintf("mock_access_token_%p", userInfo)

	// Mock OIDC token endpoint
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]string{
			"access_token": accessToken,
			"token_type":   "Bearer",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Mock OIDC userinfo endpoint
	mux.HandleFunc("GET /oauth2/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := struct {
			Sub     string  `json:"sub"`
			Email   string  `json:"email"`
			Name    string  `json:"name"`
			Picture *string `json:"picture,omitempty"`
		}{
			Sub:   userInfo.UserID,
			Email: userInfo.Email,
			Name:  userInfo.Email,
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	return httptest.NewServer(mux)
}

func newTestAuthService(t *testing.T, userInfo *MockUserInfo) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "authtest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	// Create mock OIDC server
	mockServer := createMockOIDCServer(userInfo)
	t.Cleanup(mockServer.Close)

	pylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := sessions.NewManager(logger, testenv.NewTracerProvider(t), guardianPolicy, conn, redisClient, cache.Suffix("gram-test"), mockServer.URL+"/oauth2", pylon, posthog, billingClient)

	authConfigs := auth.AuthConfigurations{
		IDPBaseURL:        mockServer.URL + "/oauth2",
		GramServerURL:     "http://localhost:8080",
		SignInRedirectURL: "http://localhost:3000/dashboard",
		Environment:       "test",
	}

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	svc := auth.NewService(logger, tracerProvider, conn, sessionManager, authConfigs, authzEngine)

	return ctx, newTestAuthServiceResult(t, svc, conn, sessionManager, mockServer, authConfigs)
}

func newTestAuthServiceWithAuthz(t *testing.T, userInfo *MockUserInfo) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "authtest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	mockServer := createMockOIDCServer(userInfo)
	t.Cleanup(mockServer.Close)

	pylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := sessions.NewManager(logger, testenv.NewTracerProvider(t), guardianPolicy, conn, redisClient, cache.Suffix("gram-test"), mockServer.URL+"/oauth2", pylon, posthog, billingClient)

	authConfigs := auth.AuthConfigurations{
		IDPBaseURL:        mockServer.URL + "/oauth2",
		GramServerURL:     "http://localhost:8080",
		SignInRedirectURL: "http://localhost:3000/dashboard",
		Environment:       "test",
	}

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	svc := auth.NewService(logger, tracerProvider, conn, sessionManager, authConfigs, authzEngine)

	return ctx, newTestAuthServiceResult(t, svc, conn, sessionManager, mockServer, authConfigs)
}

func newTestAuthServiceResult(_ *testing.T, svc *auth.Service, conn *pgxpool.Pool, sessionManager *sessions.Manager, mockServer *httptest.Server, authConfigs auth.AuthConfigurations) *testInstance {
	return &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		mockAuthServer: mockServer,
		authConfigs:    authConfigs,
	}
}

// Helper function to create a default mock user info
func defaultMockUserInfo() *MockUserInfo {
	return &MockUserInfo{
		UserID:          "test-user-123",
		Email:           "test@example.com",
		Admin:           false,
		UserWhitelisted: true,
		Organizations: []MockOrganizationEntry{
			{
				ID:                 "org-123",
				Name:               "Test Organization",
				Slug:               "test-org",
				UserWorkspaceSlugs: []string{"workspace1", "workspace2"},
			},
		},
	}
}

// Helper function to create a speakeasy user mock info
func speakeasyMockUserInfo() *MockUserInfo {
	return &MockUserInfo{
		UserID:          "speakeasy-user-123",
		Email:           "test@speakeasy.com",
		Admin:           false,
		UserWhitelisted: true,
		Organizations: []MockOrganizationEntry{
			{
				ID:                 "speakeasy-team-123",
				Name:               "Speakeasy Team",
				Slug:               "speakeasy-team",
				UserWorkspaceSlugs: []string{"speakeasy-workspace"},
			},
			{
				ID:                 "other-org-123",
				Name:               "Other Organization",
				Slug:               "other-org",
				UserWorkspaceSlugs: []string{"other-workspace"},
			},
		},
	}
}

// Helper function to create an admin user mock info
func adminMockUserInfo() *MockUserInfo {
	return &MockUserInfo{
		UserID:          "admin-user-123",
		Email:           "admin@speakeasyapi.dev",
		Admin:           true,
		UserWhitelisted: true,
		Organizations: []MockOrganizationEntry{
			{
				ID:                 "admin-org-123",
				Name:               "Admin Organization",
				Slug:               "admin-org",
				UserWorkspaceSlugs: []string{"admin-workspace"},
			},
		},
	}
}

// createTestUser creates a user record in the database for testing
func (ti *testInstance) createTestUser(ctx context.Context, userInfo *MockUserInfo) error {
	usersQueries := usersRepo.New(ti.conn)
	_, err := usersQueries.UpsertUser(ctx, usersRepo.UpsertUserParams{
		ID:          userInfo.UserID,
		Email:       userInfo.Email,
		DisplayName: userInfo.Email,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       userInfo.Admin,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert user: %w", err)
	}
	return nil
}

// createTestOrganization creates an organization record and org-user membership in the database for testing.
func (ti *testInstance) createTestOrganization(ctx context.Context, org MockOrganizationEntry, userID string) error {
	orgQueries := orgRepo.New(ti.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:       org.ID,
		Name:     org.Name,
		Slug:     org.Slug,
		WorkosID: conv.PtrToPGText(org.WorkosID),
	})
	if err != nil {
		return fmt.Errorf("failed to upsert organization metadata: %w", err)
	}
	if userID != "" {
		_, err = orgQueries.UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: org.ID,
			UserID:         userID,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert organization user relationship: %w", err)
		}
	}
	return nil
}

// createTestProject creates a project record in the database for testing and returns its ID.
func (ti *testInstance) createTestProject(ctx context.Context, orgID, name, slug string) (projectsRepo.Project, error) {
	q := projectsRepo.New(ti.conn)
	project, err := q.CreateProject(ctx, projectsRepo.CreateProjectParams{
		OrganizationID: orgID,
		Name:           name,
		Slug:           slug,
	})
	if err != nil {
		return project, fmt.Errorf("failed to create project: %w", err)
	}
	return project, nil
}
