package auth_test

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/cache"
	"github.com/speakeasy-api/gram/internal/testenv"
	"github.com/speakeasy-api/gram/internal/thirdparty/pylon"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background())
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
	SsoConnectionID    *string  `json:"sso_connection_id"`
	UserWorkspaceSlugs []string `json:"user_workspace_slugs"`
}

// createMockAuthServer creates an httptest.Server that serves mock auth responses
func createMockAuthServer(userInfo *MockUserInfo) *httptest.Server {
	mux := http.NewServeMux()

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
				SSOConnectionID    *string  `json:"sso_connection_id,omitempty"`
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
				SSOConnectionID    *string  `json:"sso_connection_id,omitempty"`
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
			SSOConnectionID    *string  `json:"sso_connection_id,omitempty"`
			UserWorkspaceSlugs []string `json:"user_workspace_slugs"`
		}, len(userInfo.Organizations))

		for i, org := range userInfo.Organizations {
			validateResp.Organizations[i].ID = org.ID
			validateResp.Organizations[i].Name = org.Name
			validateResp.Organizations[i].Slug = org.Slug
			validateResp.Organizations[i].AccountType = "scale-up"
			validateResp.Organizations[i].SSOConnectionID = org.SsoConnectionID
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
			SsoConnectionID:    nil,
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
				SSOConnectionID    *string  `json:"sso_connection_id,omitempty"`
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
				SSOConnectionID    *string  `json:"sso_connection_id,omitempty"`
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
			SSOConnectionID    *string  `json:"sso_connection_id,omitempty"`
			UserWorkspaceSlugs []string `json:"user_workspace_slugs"`
		}, len(updatedUserInfo.Organizations))

		for i, org := range updatedUserInfo.Organizations {
			validateResp.Organizations[i].ID = org.ID
			validateResp.Organizations[i].Name = org.Name
			validateResp.Organizations[i].Slug = org.Slug
			validateResp.Organizations[i].AccountType = "free"
			validateResp.Organizations[i].SSOConnectionID = org.SsoConnectionID
			validateResp.Organizations[i].UserWorkspaceSlugs = org.UserWorkspaceSlugs
		}

		if err := json.NewEncoder(w).Encode(validateResp); err != nil {
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
		// Simulate redirect to callback with mock ID token
		http.Redirect(w, r, returnURL+"?id_token=mock_token", http.StatusFound)
	})

	return httptest.NewServer(mux)
}

func newTestAuthService(t *testing.T, userInfo *MockUserInfo) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "authtest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	// Create mock auth server
	mockServer := createMockAuthServer(userInfo)
	t.Cleanup(mockServer.Close)

	pylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	// Create session manager with mock server URL
	sessionManager := sessions.NewManager(logger, conn, redisClient, cache.Suffix("gram-test"), mockServer.URL, "test-secret-key", pylon)

	authConfigs := auth.AuthConfigurations{
		SpeakeasyServerAddress: mockServer.URL,
		GramServerURL:          "http://localhost:8080",
		SignInRedirectURL:      "http://localhost:3000/dashboard",
	}

	svc := auth.NewService(logger, conn, sessionManager, authConfigs)

	return ctx, &testInstance{
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
				SsoConnectionID:    nil,
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
				SsoConnectionID:    nil,
				UserWorkspaceSlugs: []string{"speakeasy-workspace"},
			},
			{
				ID:                 "other-org-123",
				Name:               "Other Organization",
				Slug:               "other-org",
				SsoConnectionID:    nil,
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
				SsoConnectionID:    nil,
				UserWorkspaceSlugs: []string{"admin-workspace"},
			},
		},
	}
}
