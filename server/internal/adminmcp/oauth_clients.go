package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

var staffGrantTypes = []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken}
var staffAuthMethods = []string{oauthwire.AuthMethodClientSecretBasic}

type staffOAuthClient struct {
	ID              string
	Name            string
	SecretHash      string
	RedirectURIs    []string
	SecretExpiresAt *time.Time
}

type staffClientStore interface {
	RegisterClient(context.Context, staffOAuthClient) error
	GetClient(context.Context, string) (staffOAuthClient, error)
}

type postgresStaffClientStore struct{ db *pgxpool.Pool }

func (s postgresStaffClientStore) RegisterClient(ctx context.Context, client staffOAuthClient) error {
	if s.db == nil {
		return errors.New("staff client store is unavailable")
	}
	_, err := s.db.Exec(ctx, `
INSERT INTO admin_mcp_oauth_clients (client_id, client_name, client_secret_hash, redirect_uris)
VALUES ($1, $2, NULLIF($3, ''), $4)
`, client.ID, client.Name, client.SecretHash, client.RedirectURIs)
	if err != nil {
		return fmt.Errorf("register staff MCP client: %w", err)
	}
	return nil
}

func (s postgresStaffClientStore) GetClient(ctx context.Context, clientID string) (staffOAuthClient, error) {
	if s.db == nil {
		return staffOAuthClient{}, errors.New("staff client store is unavailable")
	}
	var client staffOAuthClient
	var secretHash *string
	err := s.db.QueryRow(ctx, `
SELECT client_id, client_name, client_secret_hash, redirect_uris, client_secret_expires_at
FROM admin_mcp_oauth_clients
WHERE client_id = $1 AND revoked_at IS NULL
`, clientID).Scan(&client.ID, &client.Name, &secretHash, &client.RedirectURIs, &client.SecretExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return staffOAuthClient{}, fmt.Errorf("staff MCP client not found: %w", err)
		}
		return staffOAuthClient{}, fmt.Errorf("lookup staff MCP client: %w", err)
	}
	if secretHash != nil {
		client.SecretHash = *secretHash
	}
	return client, nil
}

// StaffOAuthClients owns the registration endpoint; mounting it remains a separate decision.
type StaffOAuthClients struct{ store staffClientStore }

func NewStaffOAuthClients(db *pgxpool.Pool) *StaffOAuthClients {
	return &StaffOAuthClients{store: postgresStaffClientStore{db: db}}
}

func (s *StaffOAuthClients) RegisterHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if s == nil || s.store == nil {
			staffOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "client registration is unavailable")
			return
		}
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/json" {
			staffOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "Content-Type must be application/json")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var request usersessions.RegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			staffOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "request body is not valid JSON")
			return
		}
		if request.TokenEndpointAuthMethod == "" {
			staffOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "token_endpoint_auth_method is required")
			return
		}
		request.SetDefaults()
		if err := request.Validate(staffGrantTypes, staffAuthMethods); err != nil {
			if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
				staffOAuthError(w, http.StatusBadRequest, oauthErr.Code, oauthErr.Description)
			} else {
				staffOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid client metadata")
			}
			return
		}
		// All staff clients use authorization codes and rotating refresh tokens.
		// These are fixed server capabilities, not client-selected permissions.
		if !slices.Contains(request.GrantTypes, oauthwire.GrantTypeAuthorizationCode) {
			staffOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "authorization_code grant is required")
			return
		}
		client := staffOAuthClient{ID: "client_" + uuid.NewString(), Name: request.ClientName, RedirectURIs: request.RedirectURIs, SecretHash: "", SecretExpiresAt: nil}
		secret, err := staffOpaqueToken()
		if err != nil {
			staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not register client")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
		if err != nil {
			staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not register client")
			return
		}
		client.SecretHash = string(hash)
		if err := s.store.RegisterClient(r.Context(), client); err != nil {
			staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not register client")
			return
		}
		response := map[string]any{
			"client_id": client.ID, "client_id_issued_at": time.Now().Unix(),
			"client_name": client.Name, "redirect_uris": client.RedirectURIs,
			"grant_types": staffGrantTypes, "response_types": request.ResponseTypes,
			"token_endpoint_auth_method": request.TokenEndpointAuthMethod,
		}
		if secret != "" {
			response["client_secret"] = secret
			response["client_secret_expires_at"] = 0
		}
		staffJSON(w, http.StatusCreated, response)
	})
}

func staffOAuthError(w http.ResponseWriter, status int, code, description string) {
	staffJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func staffJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
