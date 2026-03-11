package testenv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	MockSecretKey = "test-secret"
	MockUserID    = "test-user-1"
	MockUserEmail = "dev@example.com"
	MockOrgID     = "550e8400-e29b-41d4-a716-446655440000"
	MockOrgName   = "Local Dev Org"
	MockOrgSlug   = "local-dev-org"
)

// mockValidateResponse represents the response from the validate endpoint.
// We duplicate the structure here rather than importing unexported types from sessions.
type mockValidateResponse struct {
	User          mockUser           `json:"user"`
	Organizations []mockOrganization `json:"organizations"`
}

type mockUser struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	PhotoURL     *string   `json:"photo_url"`
	GithubHandle *string   `json:"github_handle"`
	Admin        bool      `json:"admin"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Whitelisted  bool      `json:"whitelisted"`
}

type mockOrganization struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Slug               string    `json:"slug"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	AccountType        string    `json:"account_type"`
	SSOConnectionID    *string   `json:"sso_connection_id,omitempty"`
	UserWorkspaceSlugs []string  `json:"user_workspaces_slugs"`
}

// NewMockIDP creates an httptest.Server that implements the Speakeasy provider
// endpoints needed for authentication testing. The server is automatically
// closed when the test completes.
func NewMockIDP(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/speakeasy_provider/exchange", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := struct {
			IDToken string `json:"id_token"`
		}{
			IDToken: "mock-token-" + uuid.NewString(),
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	mux.HandleFunc("GET /v1/speakeasy_provider/validate", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("speakeasy-auth-provider-id-token")
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		now := time.Now()
		resp := mockValidateResponse{
			User: mockUser{
				ID:           MockUserID,
				Email:        MockUserEmail,
				DisplayName:  "Dev User",
				PhotoURL:     nil,
				GithubHandle: nil,
				Admin:        true,
				CreatedAt:    now,
				UpdatedAt:    now,
				Whitelisted:  true,
			},
			Organizations: []mockOrganization{
				{
					ID:                 MockOrgID,
					Name:               MockOrgName,
					Slug:               MockOrgSlug,
					CreatedAt:          now,
					UpdatedAt:          now,
					AccountType:        "free",
					SSOConnectionID:    nil,
					UserWorkspaceSlugs: []string{},
				},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	mux.HandleFunc("POST /v1/speakeasy_provider/revoke", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /v1/speakeasy_provider/register", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("speakeasy-auth-provider-id-token")
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}

		var req struct {
			OrganizationName string `json:"organization_name"`
			AccountType      string `json:"account_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		now := time.Now()
		resp := mockValidateResponse{
			User: mockUser{
				ID:           MockUserID,
				Email:        MockUserEmail,
				DisplayName:  "Dev User",
				PhotoURL:     nil,
				GithubHandle: nil,
				Admin:        true,
				CreatedAt:    now,
				UpdatedAt:    now,
				Whitelisted:  true,
			},
			Organizations: []mockOrganization{
				{
					ID:                 MockOrgID,
					Name:               MockOrgName,
					Slug:               MockOrgSlug,
					CreatedAt:          now,
					UpdatedAt:          now,
					AccountType:        "free",
					SSOConnectionID:    nil,
					UserWorkspaceSlugs: []string{},
				},
				{
					ID:                 uuid.NewString(),
					Name:               req.OrganizationName,
					Slug:               "new-org",
					CreatedAt:          now,
					UpdatedAt:          now,
					AccountType:        req.AccountType,
					SSOConnectionID:    nil,
					UserWorkspaceSlugs: []string{},
				},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	mux.HandleFunc("GET /v1/speakeasy_provider/login", func(w http.ResponseWriter, r *http.Request) {
		returnURL := r.URL.Query().Get("return_url")
		state := r.URL.Query().Get("state")
		code := "mock-code-" + uuid.NewString()
		redirectURL := returnURL + "?code=" + code
		if state != "" {
			redirectURL += "&state=" + state
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
