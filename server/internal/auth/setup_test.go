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

// MockUserInfo represents the user info returned by the mock auth server
type MockUserInfo struct {
	UserID          string                  `json:"user_id"`
	Email           string                  `json:"email"`
	Admin           bool                    `json:"admin"`
	UserWhitelisted bool                    `json:"user_whitelisted"`
	Organizations   []MockOrganizationEntry `json:"organizations"`
}

type MockOrganizationEntry struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Slug               string   `json:"slug"`
	WorkosID           *string  `json:"workos_id"`
	UserWorkspaceSlugs []string `json:"user_workspace_slugs"`
}

// createMockAuthServer creates an httptest.Server that serves mock auth responses
func createMockAuthServer(userInfo *MockUserInfo) *httptest.Server {
	mux := http.NewServeMux()
	idToken := fmt.Sprintf("mock_id_token_%p", userInfo)

	// Mock the validate endpoint that sessions.GetUserInfoFromSpeakeasy calls
	mux.HandleFunc("/v1/speakeasy_provider/validate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Convert our mock user info to the expected format
		validateResp := struct {
			User struct {
				ID          string `json:"id"`
				Email       string `json:"email"`
				DisplayName string `json:"display_name"`
				Admin       bool   `json:"admin"`
				Whitelisted bool   `json:"whitelisted"`
			} `json:"user"`
			Organizations []struct {
				ID                 string   `json:"id"`
				Name               string   `json:"name"`
				Slug               string   `json:"slug"`
				AccountType        string   `json:"account_type"`
				WorkOSID           *string  `json:"workos_id,omitempty"`
				UserWorkspaceSlugs []string `json:"user_workspace_slugs"`
			} `json:"organizations"`
		}{
			User: struct {
				ID          string `json:"id"`
				Email       string `json:"email"`
				DisplayName string `json:"display_name"`
				Admin       bool   `json:"admin"`
				Whitelisted bool   `json:"whitelisted"`
			}{
				ID:          "",
				Email:       "",
				DisplayName: "",
				Admin:       false,
				Whitelisted: false,
			},
			Organizations: []struct {
				ID                 string   `json:"id"`
				Name               string   `json:"name"`
				Slug               string   `json:"slug"`
				AccountType        string   `json:"account_type"`
				WorkOSID           *string  `json:"workos_id,omitempty"`
				UserWorkspaceSlugs []string `json:"user_workspace_slugs"`
			}{},
		}

		validateResp.User.ID = userInfo.UserID
		validateResp.User.Email = userInfo.Email
		validateResp.User.DisplayName = userInfo.Email
		validateResp.User.Admin = userInfo.Admin
		validateResp.User.Whitelisted = userInfo.UserWhitelisted

		validateResp.Organizations = make([]struct {
			ID                 string   `json:"id"`
			Name               string   `json:"name"`
			Slug               string   `json:"slug"`
			AccountType        string   `json:"account_type"`
			WorkOSID           *string  `json:"workos_id,omitempty"`
			UserWorkspaceSlugs []string `json:"user_workspace_slugs"`
		}, len(userInfo.Organizations))

		for i, org := range userInfo.Organizations {
			validateResp.Organizations[i].ID = org.ID
			validateResp.Organizations[i].Name = org.Name
			validateResp.Organizations[i].Slug = org.Slug
			validateResp.Organizations[i].AccountType = "scale-up"
			validateResp.Organizations[i].WorkOSID = org.WorkosID
			validateResp.Organizations[i].UserWorkspaceSlugs = org.UserWorkspaceSlugs
		}

		if err := json.NewEncoder(w).Encode(validateResp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	// Mock the register endpoint that CreateOrgFromSpeakeasy calls
	mux.HandleFunc("/v1/speakeasy_provider/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Create a new organization and return updated user info
		newOrg := MockOrganizationEntry{
			ID:                 "new-org-123",
			Name:               "New Organization",
			Slug:               "new-org",
			UserWorkspaceSlugs: []string{"new-workspace"},
		}

		// Add the new organization to the existing user info
		updatedUserInfo := *userInfo
		updatedUserInfo.Organizations = append(updatedUserInfo.Organizations, newOrg)

		// Convert to validate response format
		validateResp := struct {
			User struct {
				ID          string `json:"id"`
				Email       string `json:"email"`
				DisplayName string `json:"display_name"`
				Admin       bool   `json:"admin"`
				Whitelisted bool   `json:"whitelisted"`
			} `json:"user"`
			Organizations []struct {
				ID                 string   `json:"id"`
				Name               string   `json:"name"`
				Slug               string   `json:"slug"`
				AccountType        string   `json:"account_type"`
				WorkOSID           *string  `json:"workos_id,omitempty"`
				UserWorkspaceSlugs []string `json:"user_workspace_slugs"`
			} `json:"organizations"`
		}{
			User: struct {
				ID          string `json:"id"`
				Email       string `json:"email"`
				DisplayName string `json:"display_name"`
				Admin       bool   `json:"admin"`
				Whitelisted bool   `json:"whitelisted"`
			}{
				ID:          "",
				Email:       "",
				DisplayName: "",
				Admin:       false,
				Whitelisted: false,
			},
			Organizations: []struct {
				ID                 string   `json:"id"`
				Name               string   `json:"name"`
				Slug               string   `json:"slug"`
				AccountType        string   `json:"account_type"`
				WorkOSID           *string  `json:"workos_id,omitempty"`
				UserWorkspaceSlugs []string `json:"user_workspace_slugs"`
			}{},
		}

		validateResp.User.ID = updatedUserInfo.UserID
		validateResp.User.Email = updatedUserInfo.Email
		validateResp.User.DisplayName = updatedUserInfo.Email
		validateResp.User.Admin = updatedUserInfo.Admin
		validateResp.User.Whitelisted = updatedUserInfo.UserWhitelisted

		validateResp.Organizations = make([]struct {
			ID                 string   `json:"id"`
			Name               string   `json:"name"`
			Slug               string   `json:"slug"`
			AccountType        string   `json:"account_type"`
			WorkOSID           *string  `json:"workos_id,omitempty"`
			UserWorkspaceSlugs []string `json:"user_workspace_slugs"`
		}, len(updatedUserInfo.Organizations))

		for i, org := range updatedUserInfo.Organizations {
			validateResp.Organizations[i].ID = org.ID
			validateResp.Organizations[i].Name = org.Name
			validateResp.Organizations[i].Slug = org.Slug
			validateResp.Organizations[i].AccountType = "free"
			validateResp.Organizations[i].WorkOSID = org.WorkosID
			validateResp.Organizations[i].UserWorkspaceSlugs = org.UserWorkspaceSlugs
		}

		if err := json.NewEncoder(w).Encode(validateResp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	// Mock the exchange endpoint for code to token exchange
	mux.HandleFunc("/v1/speakeasy_provider/exchange", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Return a mock token response
		tokenResp := struct {
			IDToken string `json:"id_token"`
		}{
			IDToken: idToken,
		}

		if err := json.NewEncoder(w).Encode(tokenResp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	// Mock the login endpoint
	mux.HandleFunc("/v1/speakeasy_provider/login", func(w http.ResponseWriter, r *http.Request) {
		returnURL := r.URL.Query().Get("return_url")
		if returnURL == "" {
			http.Error(w, "missing return_url", http.StatusBadRequest)
			return
		}
		// Simulate redirect to callback with mock auth code
		http.Redirect(w, r, returnURL+"?code=mock_code", http.StatusFound)
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

	// Create mock auth server
	mockServer := createMockAuthServer(userInfo)
	t.Cleanup(mockServer.Close)

	pylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := sessions.NewManager(logger, testenv.NewTracerProvider(t), guardianPolicy, conn, redisClient, cache.Suffix("gram-test"), mockServer.URL, "test-secret-key", pylon, posthog, billingClient, nil)

	authConfigs := auth.AuthConfigurations{
		SpeakeasyServerAddress: mockServer.URL,
		GramServerURL:          "http://localhost:8080",
		SignInRedirectURL:      "http://localhost:3000/dashboard",
		Environment:            "test",
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

	mockServer := createMockAuthServer(userInfo)
	t.Cleanup(mockServer.Close)

	pylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	posthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := sessions.NewManager(logger, testenv.NewTracerProvider(t), guardianPolicy, conn, redisClient, cache.Suffix("gram-test"), mockServer.URL, "test-secret-key", pylon, posthog, billingClient, nil)

	authConfigs := auth.AuthConfigurations{
		SpeakeasyServerAddress: mockServer.URL,
		GramServerURL:          "http://localhost:8080",
		SignInRedirectURL:      "http://localhost:3000/dashboard",
		Environment:            "test",
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

// createTestOrganization creates an organization record in the database for testing
func (ti *testInstance) createTestOrganization(ctx context.Context, org MockOrganizationEntry) error {
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
