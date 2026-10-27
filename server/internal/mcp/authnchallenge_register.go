// RFC 7591 Dynamic Client Registration handler for the issuer-gated OAuth
// surface. Validation + defaults of the request live in the usersessions
// package as RegistrationRequest; this file owns the HTTP plumbing.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
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
	ClientID                string          `json:"client_id"`
	ClientSecret            string          `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64           `json:"client_id_issued_at"`
	ClientSecretExpiresAt   *int64          `json:"client_secret_expires_at,omitempty"`
	ClientName              string          `json:"client_name"`
	RedirectURIs            []string        `json:"redirect_uris"`
	GrantTypes              []string        `json:"grant_types"`
	ResponseTypes           []string        `json:"response_types"`
	TokenEndpointAuthMethod string          `json:"token_endpoint_auth_method"`
	JWKS                    json.RawMessage `json:"jwks,omitempty"`
	JWKSURI                 string          `json:"jwks_uri,omitempty"`
}

// HandleRegister is the chi handler at `POST /mcp/{mcpSlug}/register`.
// Resolves the slug to an issuer-gated ResolvedMcpEndpoint and dispatches
// to ServeRegister.
func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogToolsetMCPSlug(mcpSlug))
	endpoint, err := s.LoadResolvedMcpEndpointBySlug(ctx, logger, mcpSlug, mcproute.MCP)
	if err != nil {
		return err
	}
	return s.ServeRegister(w, r, endpoint)
}

// ServeRegister implements RFC 7591 Dynamic Client Registration for
// issuer-gated MCP servers. Post-resolution entry point shared by
// HandleRegister after slug resolution. Public endpoint (no caller auth); the issuer's
// metadata document advertises this URL via `registration_endpoint`.
//
// Generated client_secret is returned plaintext exactly once; only its
// bcrypt hash is persisted in user_session_clients.client_secret_hash.
func (s *Service) ServeRegister(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	return s.serveRegister(w, r, endpoint.LogWith(s.logger).With(attr.SlogToolsetMCPSlug(endpoint.Slug)), endpoint.UserSessionIssuerID)
}

// serveRegister registers a client with the issuer. Clients belong to the
// issuer, not to an MCP server, so per-endpoint and shared authorization
// servers share it.
func (s *Service) serveRegister(w http.ResponseWriter, r *http.Request, logger *slog.Logger, issuerID uuid.UUID) error {
	ctx := r.Context()

	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || mediaType != "application/json" {
			return writeDCRError(ctx, w, logger, "invalid_client_metadata", "Content-Type must be application/json")
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, dcrMaxBodyBytes)

	var req usersessions.RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return writeDCRError(ctx, w, logger, "invalid_client_metadata", fmt.Sprintf("request body exceeds %d bytes", dcrMaxBodyBytes))
		}
		return writeDCRError(ctx, w, logger, "invalid_client_metadata", "request body is not valid JSON")
	}

	req.SetDefaults()
	if err := req.Validate(usersessions.RegistrableGrantTypes, usersessions.SupportedAuthMethods); err != nil {
		if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
			return writeDCRError(ctx, w, logger, oauthErr.Code, oauthErr.Description)
		}
		return oops.E(oops.CodeUnexpected, err, "validate DCR request").LogError(ctx, logger)
	}

	registered, err := usersessions.CreateClientRegistration(ctx, usersessions_repo.New(s.db), issuerID, req)
	if err != nil {
		if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
			return writeDCRError(ctx, w, logger, oauthErr.Code, oauthErr.Description)
		}
		// The insert re-reads the issuer under lock, so a registration that
		// resolved the endpoint before a migration or delete retired its
		// issuer lands here instead of on a tombstone.
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user_session_issuer not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "failed to create user session client").LogError(ctx, logger)
	}
	row := registered.Row
	clientID := row.ClientID
	clientSecret := registered.ClientSecret
	clientSecretHash := row.ClientSecretHash

	logger.InfoContext(ctx, "user session client registered",
		attr.SlogOAuthClientID(clientID),
		attr.SlogOAuthClientName(req.ClientName),
		attr.SlogOAuthRegisteredAuthMethod(req.TokenEndpointAuthMethod),
		attr.SlogOAuthClientSecretGenerated(clientSecretHash.Valid),
		attr.SlogOAuthRedirectURICount(len(req.RedirectURIs)),
		attr.SlogURLOriginal(r.URL.Path),
		attr.SlogHTTPRequestHeaderUserAgent(r.UserAgent()),
	)

	// Clients that were minted a secret get client_secret plus
	// client_secret_expires_at=0 (non-expiring per RFC 7591 §3.2.1). Every
	// other client gets neither field: emitting them would suggest a secret
	// exists. The key source is echoed as registered, per §3.2.1's rule that
	// the response reflects the metadata actually stored.
	var clientSecretExpiresAt *int64
	if clientSecretHash.Valid {
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
		JWKS:                    req.JWKS,
		JWKSURI:                 req.JWKSURI,
	}

	body, err := json.Marshal(resp)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal registration response").LogError(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write response body").LogError(ctx, logger)
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
		return oops.E(oops.CodeUnexpected, err, "failed to marshal DCR error").LogError(ctx, logger)
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
		return oops.E(oops.CodeUnexpected, werr, "failed to write DCR error body").LogError(ctx, logger)
	}
	return nil
}
