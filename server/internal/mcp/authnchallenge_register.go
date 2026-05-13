// RFC 7591 Dynamic Client Registration handler for the issuer-gated OAuth
// surface. Validation + defaults of the request live in the usersessions
// package as RegistrationRequest; this file owns the HTTP plumbing.

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// dcrMaxBodyBytes caps the RFC 7591 §3.1 client metadata document size on
// HandleRegister. The spec doesn't mandate a limit; 64 KiB is well past any
// real document and defends against memory-exhaustion (gosec G120).
const dcrMaxBodyBytes int64 = 64 * 1024

// dcrRegistrationResponse is the RFC 7591 §3.2.1 successful registration
// response. `client_secret` is included exactly once, on this response. Both
// `client_secret` and `client_secret_expires_at` are omitted entirely for
// public (`token_endpoint_auth_method=none`) clients per RFC 7591 §3.2.1
// — emitting an empty string for `client_secret` confuses some MCP SDKs into
// preferring `client_secret_basic` for the token call.
//
// `scope` is intentionally absent (see RegistrationRequest comment).
type dcrRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   *int64   `json:"client_secret_expires_at,omitempty"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// HandleRegister implements RFC 7591 Dynamic Client Registration for issuer-
// gated MCP servers. Mounted at `POST /mcp/{mcpSlug}/register`. Public endpoint
// (no caller auth); the issuer's metadata document advertises this URL via
// `registration_endpoint`.
//
// Generated client_secret is returned plaintext exactly once; only its bcrypt
// hash is persisted in user_session_clients.client_secret_hash.
func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, _, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}

	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}
	if err := s.requireUserSessionIssuer(ctx, toolset); err != nil {
		return err
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || mediaType != "application/json" {
			return writeDCRError(ctx, w, logger, "invalid_client_metadata", "Content-Type must be application/json")
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, dcrMaxBodyBytes)

	var req usersessions.RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return writeDCRError(ctx, w, logger, "invalid_client_metadata", fmt.Sprintf("request body exceeds %d bytes", dcrMaxBodyBytes))
		}
		return writeDCRError(ctx, w, logger, "invalid_client_metadata", "request body is not valid JSON")
	}

	req.SetDefaults()
	if err := req.Validate(); err != nil {
		var oauthErr *usersessions.OAuthError
		if errors.As(err, &oauthErr) {
			return writeDCRError(ctx, w, logger, oauthErr.Code, oauthErr.Description)
		}
		return oops.E(oops.CodeUnexpected, err, "validate DCR request").Log(ctx, logger)
	}

	clientID := "client_" + uuid.NewString()

	// Public clients (token_endpoint_auth_method=none) skip secret generation
	// and store NULL in client_secret_hash. The /token handler treats a NULL
	// hash as "no secret expected; PKCE is the integrity proof".
	var clientSecret string
	var clientSecretHash pgtype.Text
	if req.TokenEndpointAuthMethod != "none" {
		var err error
		clientSecret, err = generateClientSecret()
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to generate client secret").Log(ctx, logger)
		}
		hashed, hashErr := bcrypt.GenerateFromPassword([]byte(clientSecret), bcrypt.DefaultCost)
		if hashErr != nil {
			return oops.E(oops.CodeUnexpected, hashErr, "failed to hash client secret").Log(ctx, logger)
		}
		clientSecretHash = pgtype.Text{String: string(hashed), Valid: true}
	}

	row, err := usersessions_repo.New(s.db).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            clientID,
		ClientSecretHash:    clientSecretHash,
		ClientName:          req.ClientName,
		RedirectUris:        req.RedirectURIs,
		// RFC 7591 §3.2.1 expires_at=0 = non-expiring; we leave the Postgres column NULL.
		ClientSecretExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to create user session client").Log(ctx, logger)
	}

	logger.InfoContext(ctx, "user session client registered",
		attr.SlogOAuthClientID(clientID),
		attr.SlogOAuthClientName(req.ClientName),
	)

	// Confidential clients get client_secret + client_secret_expires_at=0
	// (non-expiring per RFC 7591 §3.2.1). Public clients (none) get neither
	// field — emitting them would suggest a secret exists.
	var clientSecretExpiresAt *int64
	if req.TokenEndpointAuthMethod != "none" {
		zero := int64(0)
		clientSecretExpiresAt = &zero
	}

	resp := dcrRegistrationResponse{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientIDIssuedAt:        row.ClientIDIssuedAt.Time.Unix(),
		ClientSecretExpiresAt:   clientSecretExpiresAt,
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              req.GrantTypes,
		ResponseTypes:           req.ResponseTypes,
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
	}

	body, err := json.Marshal(resp)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal registration response").Log(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write response body").Log(ctx, logger)
	}
	return nil
}

// writeDCRError emits an RFC 7591 §3.2.2 client registration error response.
// Status is 400 with a JSON body { "error": "<code>", "error_description": "..." }.
func writeDCRError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, code, description string) error {
	body, err := json.Marshal(map[string]string{
		"error":             code,
		"error_description": description,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal DCR error").Log(ctx, logger)
	}

	logger.InfoContext(ctx, "DCR registration rejected",
		attr.SlogOAuthError(code),
		attr.SlogOAuthErrorDescription(description),
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusBadRequest)
	if _, werr := w.Write(body); werr != nil {
		return oops.E(oops.CodeUnexpected, werr, "failed to write DCR error body").Log(ctx, logger)
	}
	return nil
}

// generateClientSecret produces 32 bytes of cryptographically random data
// and base64url-encodes them.
func generateClientSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
