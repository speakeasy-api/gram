package assistants

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	mcpAuthFlowMaxBodyBytes = 16 * 1024
	mcpAuthFlowTTL          = 15 * time.Minute
	mcpAuthEventKind        = "assistant_mcp_auth"

	mcpAuthStatusSuccess = "success"
	mcpAuthStatusFailed  = "failed"
)

type createMCPAuthFlowRequest struct {
	ThreadID string `json:"thread_id"`
	ServerID string `json:"server_id"`
	URL      string `json:"url"`
}

type createMCPAuthFlowResponse struct {
	ServerID string `json:"server_id"`
	McpSlug  string `json:"mcp_slug"`
	AuthURL  string `json:"auth_url"`
}

type mcpAuthClientRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type mcpAuthClientRegistrationResponse struct {
	ClientID string `json:"client_id"`
}

type mcpAuthEventPayload struct {
	GramEventKind    string `json:"gram_event_kind"`
	Status           string `json:"status"`
	ServerID         string `json:"mcp_server_id"`
	McpSlug          string `json:"mcp_slug"`
	Error            string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func (s *Service) handleCreateMCPAuthFlow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := r.Header.Get("Authorization")
	if token == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	authedCtx, claims, err := s.core.assistantTokens.Authorize(ctx, token)
	if err != nil {
		return fmt.Errorf("authorize assistant runtime token: %w", err)
	}
	ctx = authedCtx

	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.C(oops.CodeUnauthorized)
	}
	projectID, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "invalid token project")
	}

	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || mediaType != "application/json" {
			return oops.E(oops.CodeBadRequest, err, "Content-Type must be application/json")
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, mcpAuthFlowMaxBodyBytes)
	var req createMCPAuthFlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return oops.E(oops.CodeBadRequest, err, "request body too large")
		}
		return oops.E(oops.CodeBadRequest, err, "decode mcp auth flow request")
	}
	threadID, err := uuid.Parse(req.ThreadID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid thread_id")
	}
	if principal.ThreadID != uuid.Nil && principal.ThreadID != threadID {
		return oops.E(oops.CodeForbidden, nil, "token thread does not match requested thread")
	}

	mcpURL, err := url.Parse(req.URL)
	if err != nil || mcpURL.Scheme == "" || mcpURL.Host == "" {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp url")
	}
	mcpSlug, err := mcpSlugFromURL(mcpURL)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "mcp auth flow only supports hosted MCP URLs")
	}

	flowID := uuid.NewString()
	if s.core.serverURL == nil {
		return oops.E(oops.CodeUnexpected, nil, "assistant mcp auth callback base url not configured").Log(ctx, s.logger)
	}
	redirectURI := s.core.serverURL.JoinPath("rpc", "assistant-mcp-auth", flowID, "oauth", "callback").String()
	codeVerifier, codeChallenge, err := newPKCEPair()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate PKCE verifier").Log(ctx, s.logger)
	}
	encryptedVerifier, err := s.core.encryptionClient.Encrypt([]byte(codeVerifier))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encrypt pkce verifier").Log(ctx, s.logger)
	}

	metadata, err := externalmcp.DiscoverOAuthMetadata(ctx, s.logger, s.core.guardianPolicy, "", mcpURL.String())
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "discover mcp authorization server metadata").Log(ctx, s.logger)
	}
	if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" || metadata.RegistrationEndpoint == "" {
		return oops.E(oops.CodeUnexpected, nil, "mcp authorization server does not advertise RFC 8414 endpoints").Log(ctx, s.logger)
	}

	clientID, err := s.registerMCPAuthClient(ctx, metadata.RegistrationEndpoint, redirectURI)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "register assistant mcp oauth client").Log(ctx, s.logger)
	}

	state, err := s.core.assistantTokens.GenerateMCPAuthFlow(assistanttokens.MCPAuthFlowInput{
		OrgID:         claims.OrgID,
		ProjectID:     projectID,
		UserID:        claims.UserID,
		AssistantID:   principal.AssistantID,
		ThreadID:      threadID,
		FlowID:        flowID,
		ServerID:      req.ServerID,
		McpSlug:       mcpSlug,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeVerifier:  encryptedVerifier,
		TokenEndpoint: metadata.TokenEndpoint,
		TTL:           mcpAuthFlowTTL,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "sign mcp auth flow state").Log(ctx, s.logger)
	}

	authURL, err := buildMCPAuthURL(metadata.AuthorizationEndpoint, clientID, redirectURI, state, codeChallenge)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build mcp auth url").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "assistant mcp auth flow created",
		attr.SlogAssistantID(principal.AssistantID.String()),
		attr.SlogAssistantThreadID(threadID.String()),
		attr.SlogProjectID(projectID.String()),
		attr.SlogToolsetMCPSlug(mcpSlug),
	)

	return writeJSON(w, http.StatusOK, createMCPAuthFlowResponse{
		ServerID: req.ServerID,
		McpSlug:  mcpSlug,
		AuthURL:  authURL,
	})
}

func (s *Service) handleMCPAuthCallback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	flowID := chi.URLParam(r, "id")
	state := r.URL.Query().Get("state")
	claims, err := s.core.assistantTokens.ValidateMCPAuthFlow(state)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp auth callback state").Log(ctx, s.logger)
	}
	if claims.FlowID != flowID {
		return oops.E(oops.CodeBadRequest, nil, "mcp auth callback flow mismatch").Log(ctx, s.logger)
	}

	projectID, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid callback project id").Log(ctx, s.logger)
	}
	assistantID, err := uuid.Parse(claims.AssistantID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid callback assistant id").Log(ctx, s.logger)
	}
	threadID, err := uuid.Parse(claims.ThreadID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid callback thread id").Log(ctx, s.logger)
	}

	payload := mcpAuthEventPayload{
		GramEventKind:    mcpAuthEventKind,
		Status:           mcpAuthStatusSuccess,
		ServerID:         claims.ServerID,
		McpSlug:          claims.McpSlug,
		Error:            "",
		ErrorDescription: "",
	}
	oauthErr := r.URL.Query().Get("error")
	code := r.URL.Query().Get("code")
	switch {
	case oauthErr != "":
		payload.Status = mcpAuthStatusFailed
		payload.Error = oauthErr
		payload.ErrorDescription = r.URL.Query().Get("error_description")
	case code == "":
		payload.Status = mcpAuthStatusFailed
		payload.Error = "invalid_request"
		payload.ErrorDescription = "authorization code missing from callback"
	default:
		if err := s.consumeMCPAuthGrant(ctx, claims, code); err != nil {
			payload.Status = mcpAuthStatusFailed
			payload.Error = "invalid_grant"
			payload.ErrorDescription = "failed to consume authorization grant"
			s.logger.WarnContext(ctx, "assistant mcp auth grant consumption failed",
				attr.SlogAssistantID(assistantID.String()),
				attr.SlogAssistantThreadID(threadID.String()),
				attr.SlogProjectID(projectID.String()),
				attr.SlogError(err),
			)
		}
	}

	eventCreated, err := s.enqueueMCPAuthEvent(ctx, projectID, assistantID, threadID, flowID, payload)
	if err != nil {
		return err
	}
	if eventCreated {
		if err := s.signaler.SignalCoordinator(ctx, assistantID); err != nil {
			return fmt.Errorf("signal assistant coordinator: %w", err)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "<!doctype html><title>Authentication complete</title><p>Authentication complete. You can close this window.</p>")
	return nil
}

func (s *Service) enqueueMCPAuthEvent(ctx context.Context, projectID, assistantID, threadID uuid.UUID, flowID string, payload mcpAuthEventPayload) (bool, error) {
	var correlationID string
	err := s.core.db.QueryRow(ctx, `
SELECT correlation_id
FROM assistant_threads
WHERE id = $1
  AND project_id = $2
  AND assistant_id = $3
  AND deleted IS FALSE
`, threadID, projectID, assistantID).Scan(&correlationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, oops.E(oops.CodeNotFound, err, "assistant thread not found").Log(ctx, s.logger)
		}
		return false, oops.E(oops.CodeUnexpected, err, "load assistant thread for mcp auth event").Log(ctx, s.logger)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "marshal mcp auth event").Log(ctx, s.logger)
	}
	_, err = assistantrepo.New(s.core.db).InsertAssistantThreadEvent(ctx, assistantrepo.InsertAssistantThreadEventParams{
		AssistantThreadID:     threadID,
		AssistantID:           assistantID,
		ProjectID:             projectID,
		TriggerInstanceID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		EventID:               mcpAuthEventKind + ":" + flowID,
		CorrelationID:         correlationID,
		Status:                eventStatusPending,
		NormalizedPayloadJson: body,
		SourcePayloadJson:     body,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, oops.E(oops.CodeUnexpected, err, "insert mcp auth assistant event").Log(ctx, s.logger)
	default:
		return true, nil
	}
}

func decodeMCPAuthTurn(ctx context.Context, logger *slog.Logger, event assistantThreadEventRecord) (string, bool) {
	var payload mcpAuthEventPayload
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
		logger.WarnContext(ctx, "skip mcp auth event with undecodable payload",
			attr.SlogAssistantEventID(event.EventID),
			attr.SlogError(err),
		)
		return "", false
	}
	if payload.GramEventKind != mcpAuthEventKind {
		return "", false
	}
	var b strings.Builder
	b.WriteString("<message-context>\n")
	fmt.Fprintf(&b, "EventID: %s\n", event.EventID)
	fmt.Fprintf(&b, "EventType: %s\n", mcpAuthEventKind)
	if payload.ServerID != "" {
		fmt.Fprintf(&b, "MCPServerID: %s\n", payload.ServerID)
	}
	if payload.McpSlug != "" {
		fmt.Fprintf(&b, "MCPSlug: %s\n", payload.McpSlug)
	}
	if payload.Status != "" {
		fmt.Fprintf(&b, "Status: %s\n", payload.Status)
	}
	if payload.Error != "" {
		fmt.Fprintf(&b, "Error: %s\n", payload.Error)
	}
	if payload.ErrorDescription != "" {
		fmt.Fprintf(&b, "ErrorDescription: %s\n", payload.ErrorDescription)
	}
	b.WriteString("</message-context>")
	return b.String(), true
}

func (s *Service) registerMCPAuthClient(ctx context.Context, endpoint, redirectURI string) (string, error) {
	payload := mcpAuthClientRegistrationRequest{
		ClientName:              "Gram Assistant MCP Auth",
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal registration request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.core.guardianPolicy.Client(guardian.WithDefaultRetryConfig()).Do(req)
	if err != nil {
		return "", fmt.Errorf("send registration request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read registration response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("registration failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out mcpAuthClientRegistrationResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("decode registration response: %w", err)
	}
	if out.ClientID == "" {
		return "", fmt.Errorf("registration response missing client_id")
	}
	return out.ClientID, nil
}

func (s *Service) consumeMCPAuthGrant(ctx context.Context, claims *assistanttokens.MCPAuthFlowClaims, code string) error {
	verifier, err := s.core.encryptionClient.Decrypt(claims.CodeVerifier)
	if err != nil {
		return fmt.Errorf("decrypt pkce verifier: %w", err)
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", claims.RedirectURI)
	form.Set("client_id", claims.ClientID)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claims.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.core.guardianPolicy.Client(guardian.WithDefaultRetryConfig()).Do(req)
	if err != nil {
		return fmt.Errorf("send token request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("token request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func mcpSlugFromURL(u *url.URL) (string, error) {
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(parts) != 2 || parts[0] != "mcp" {
		return "", fmt.Errorf("expected /mcp/{slug}")
	}
	slug, err := url.PathUnescape(parts[1])
	if err != nil || slug == "" {
		return "", fmt.Errorf("invalid mcp slug")
	}
	return slug, nil
}

func buildMCPAuthURL(endpoint, clientID, redirectURI, state, codeChallenge string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse authorize endpoint: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func newPKCEPair() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("read random verifier bytes: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}
