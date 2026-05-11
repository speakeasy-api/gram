// Package testidp serves an in-process mock identity provider for tests.
// It implements /user_management/authenticate (the WorkOS SDK endpoint shape)
// so that the sessions.Manager's single code path works in tests without
// hitting real WorkOS.
package testidp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
)

// Constants exported for tests that need to assert against the
// canonical fixture identity.
const (
	MockUserID    = "test-user-1"
	MockUserEmail = "dev@example.com"
	MockOrgID     = "550e8400-e29b-41d4-a716-446655440000"
	MockOrgName   = "Local Dev Org"
	MockOrgSlug   = "local-dev-org"
)

// Config holds the IDP configuration.
type Config struct {
	User         UserConfig
	Organization OrgConfig
}

// UserConfig holds the mock user configuration.
type UserConfig struct {
	ID          string
	Email       string
	DisplayName string
	PhotoURL    *string
	Admin       bool
}

// OrgConfig holds the mock organization configuration.
type OrgConfig struct {
	ID       string
	Name     string
	Slug     string
	WorkOSID *string
}

// NewConfig returns a Config with the canonical fixture defaults.
func NewConfig() Config {
	wosOrgID := MockOrgID
	return Config{
		User: UserConfig{
			ID:          MockUserID,
			Email:       MockUserEmail,
			DisplayName: "Dev User",
			PhotoURL:    nil,
			Admin:       true,
		},
		Organization: OrgConfig{
			ID:       MockOrgID,
			Name:     MockOrgName,
			Slug:     MockOrgSlug,
			WorkOSID: &wosOrgID,
		},
	}
}

// Handler returns an http.Handler implementing the WorkOS SDK authenticate
// endpoint (POST /user_management/authenticate). The WorkOS Go SDK sends
// requests to {endpoint}/user_management/authenticate, so the test httptest
// server URL should be passed as the SDK client's Endpoint.
func Handler(cfg Config) http.Handler {
	s := &server{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /user_management/authenticate", s.handleAuthenticate)
	mux.HandleFunc("POST /user_management/sessions/revoke", s.handleRevokeSession)
	return mux
}

type server struct {
	cfg Config
}

// authenticateResponse mirrors the WorkOS AuthenticateResponse shape that the
// Go SDK deserialises. Only the fields the sessions package reads are included.
type authenticateResponse struct {
	User         authenticateUser `json:"user"`
	AccessToken  string           `json:"access_token"`
	RefreshToken string           `json:"refresh_token"`
}

type authenticateUser struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	FirstName         string `json:"first_name"`
	LastName          string `json:"last_name"`
	ProfilePictureURL string `json:"profile_picture_url"`
}

// handleAuthenticate implements the WorkOS /user_management/authenticate
// endpoint. Accepts any non-empty code and returns the configured mock user.
func (s *server) handleAuthenticate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code     string `json:"code"`
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if body.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}

	// Split display name into first/last for the WorkOS response shape.
	firstName := s.cfg.User.DisplayName
	lastName := ""

	pictureURL := ""
	if s.cfg.User.PhotoURL != nil {
		pictureURL = *s.cfg.User.PhotoURL
	}

	sessionID := "mock_session_" + randomID()

	resp := authenticateResponse{
		User: authenticateUser{
			ID:                s.cfg.User.ID,
			Email:             s.cfg.User.Email,
			FirstName:         firstName,
			LastName:          lastName,
			ProfilePictureURL: pictureURL,
		},
		AccessToken:  mockJWT(sessionID),
		RefreshToken: randomID(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleRevokeSession is a no-op mock of POST /user_management/sessions/revoke.
func (s *server) handleRevokeSession(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failure: %v", err))
	}
	return hex.EncodeToString(b)
}

// mockJWT builds a minimal unsigned JWT with a "sid" claim so that
// extractSessionIDFromJWT in the sessions package can parse it.
func mockJWT(sessionID string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]string{"sid": sessionID})
	payloadEnc := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + payloadEnc + "."
}
