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
	"github.com/jackc/pgx/v5/pgtype"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	mcpAuthFlowMaxBodyBytes = 16 * 1024
	mcpAuthFlowTTL          = 15 * time.Minute
	mcpAuthEventKind        = "assistant_mcp_auth"
	mcpOAuthIssuerMaxLength = 500
	mcpOAuthRegistrationTTL = 45 * time.Second
	mcpOAuthRegistrationMax = 30 * time.Second
	mcpOAuthPersistenceMax  = 5 * time.Second
	mcpOAuthCoordinationMax = mcpOAuthRegistrationTTL + mcpOAuthRegistrationMax + mcpOAuthPersistenceMax
	mcpOAuthPollMin         = 100 * time.Millisecond
	mcpOAuthPollMax         = time.Second

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
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at"`
}

type mcpAuthClientCredentials struct {
	ClientID              string
	ClientSecretEncrypted string
}

type mcpOAuthTokenError struct {
	Code       string
	StatusCode int
}

func (e *mcpOAuthTokenError) Error() string {
	return fmt.Sprintf("token request failed: status=%d error=%s", e.StatusCode, e.Code)
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

	attemptID := uuid.NewString()
	if s.core.serverURL == nil {
		return oops.E(oops.CodeUnexpected, nil, "assistant mcp auth callback base url not configured").LogError(ctx, s.logger)
	}
	redirectURI := s.core.serverURL.JoinPath("rpc", "assistantMcpAuth", principal.AssistantID.String(), "oauth", "callback").String()
	codeVerifier, codeChallenge, err := newPKCEPair()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate PKCE verifier").LogError(ctx, s.logger)
	}
	encryptedVerifier, err := s.core.encryptionClient.Encrypt([]byte(codeVerifier))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encrypt pkce verifier").LogError(ctx, s.logger)
	}

	metadata, err := externalmcp.DiscoverOAuthMetadata(ctx, s.logger, s.core.guardianPolicy, "", mcpURL.String())
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "discover mcp authorization server metadata").LogError(ctx, s.logger)
	}
	if metadata.Issuer == "" || metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" || metadata.RegistrationEndpoint == "" {
		return oops.E(oops.CodeUnexpected, nil, "mcp authorization server does not advertise RFC 8414 endpoints").LogError(ctx, s.logger)
	}
	if len(metadata.Issuer) > mcpOAuthIssuerMaxLength {
		return oops.E(oops.CodeUnexpected, nil, "mcp authorization server issuer is too long").LogError(ctx, s.logger)
	}

	credentials, err := s.getOrRegisterMCPAuthClient(
		ctx,
		projectID,
		principal.AssistantID,
		metadata.Issuer,
		metadata.RegistrationEndpoint,
		redirectURI,
	)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve assistant mcp oauth client").LogError(ctx, s.logger)
	}

	state, err := s.core.assistantTokens.GenerateMCPAuthFlow(assistanttokens.MCPAuthFlowInput{
		OrgID:             claims.OrgID,
		ProjectID:         projectID,
		UserID:            claims.UserID,
		AssistantID:       principal.AssistantID,
		ThreadID:          threadID,
		AttemptID:         attemptID,
		FlowID:            principal.AssistantID.String(),
		ServerID:          req.ServerID,
		McpURL:            mcpURL.String(),
		ClientID:          credentials.ClientID,
		ClientSecret:      credentials.ClientSecretEncrypted,
		RedirectURI:       redirectURI,
		CodeVerifier:      encryptedVerifier,
		TokenEndpoint:     metadata.TokenEndpoint,
		OAuthServerIssuer: metadata.Issuer,
		TTL:               mcpAuthFlowTTL,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "sign mcp auth flow state").LogError(ctx, s.logger)
	}

	authURL, err := buildMCPAuthURL(metadata.AuthorizationEndpoint, credentials.ClientID, redirectURI, state, codeChallenge)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build mcp auth url").LogError(ctx, s.logger)
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
	callbackID := chi.URLParam(r, "id")
	state := r.URL.Query().Get("state")
	claims, err := s.core.assistantTokens.ValidateMCPAuthFlow(state)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp auth callback state").LogError(ctx, s.logger)
	}
	if claims.FlowID != callbackID {
		return oops.E(oops.CodeBadRequest, nil, "mcp auth callback flow mismatch").LogError(ctx, s.logger)
	}

	projectID, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid callback project id").LogError(ctx, s.logger)
	}
	assistantID, err := uuid.Parse(claims.AssistantID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid callback assistant id").LogError(ctx, s.logger)
	}
	threadID, err := uuid.Parse(claims.ThreadID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid callback thread id").LogError(ctx, s.logger)
	}
	mcpURL, err := url.Parse(claims.McpURL)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid callback mcp url").LogError(ctx, s.logger)
	}
	mcpSlug, err := mcpSlugFromURL(mcpURL)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "callback mcp url missing slug").LogError(ctx, s.logger)
	}

	payload := mcpAuthEventPayload{
		GramEventKind:    mcpAuthEventKind,
		Status:           mcpAuthStatusSuccess,
		ServerID:         claims.ServerID,
		McpSlug:          mcpSlug,
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
			var tokenErr *mcpOAuthTokenError
			if errors.As(err, &tokenErr) && tokenErr.Code == "invalid_client" {
				s.invalidateMCPAuthClient(ctx, projectID, assistantID, claims.OAuthServerIssuer, claims.ClientID)
			}
			payload.Status = mcpAuthStatusFailed
			payload.Error = "invalid_grant"
			payload.ErrorDescription = "failed to consume authorization grant"
			s.logger.ErrorContext(ctx, "assistant mcp auth grant consumption failed",
				attr.SlogAssistantID(assistantID.String()),
				attr.SlogAssistantThreadID(threadID.String()),
				attr.SlogProjectID(projectID.String()),
				attr.SlogError(err),
			)
		}
	}

	eventCreated, err := s.enqueueMCPAuthEvent(ctx, projectID, assistantID, threadID, mcpAuthAttemptID(claims), payload)
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

func mcpAuthAttemptID(claims *assistanttokens.MCPAuthFlowClaims) string {
	if claims.ID != "" {
		return claims.ID
	}
	return claims.FlowID
}

func (s *Service) enqueueMCPAuthEvent(ctx context.Context, projectID, assistantID, threadID uuid.UUID, flowID string, payload mcpAuthEventPayload) (bool, error) {
	repo := assistantrepo.New(s.core.db)
	thread, err := repo.ResolveThreadCorrelation(ctx, assistantrepo.ResolveThreadCorrelationParams{
		ThreadID:  threadID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, oops.E(oops.CodeNotFound, err, "assistant thread not found").LogError(ctx, s.logger)
		}
		return false, oops.E(oops.CodeUnexpected, err, "load assistant thread for mcp auth event").LogError(ctx, s.logger)
	}
	if thread.AssistantID != assistantID {
		return false, oops.E(oops.CodeForbidden, nil, "assistant thread assistant mismatch").LogError(ctx, s.logger)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "marshal mcp auth event").LogError(ctx, s.logger)
	}
	_, err = repo.InsertAssistantThreadEvent(ctx, assistantrepo.InsertAssistantThreadEventParams{
		AssistantThreadID:     threadID,
		AssistantID:           assistantID,
		ProjectID:             projectID,
		TriggerInstanceID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		EventID:               mcpAuthEventKind + ":" + flowID,
		CorrelationID:         thread.CorrelationID,
		Status:                eventStatusPending,
		NormalizedPayloadJson: body,
		SourcePayloadJson:     body,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, oops.E(oops.CodeUnexpected, err, "insert mcp auth assistant event").LogError(ctx, s.logger)
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

func (s *Service) getOrRegisterMCPAuthClient(
	ctx context.Context,
	projectID uuid.UUID,
	assistantID uuid.UUID,
	oauthServerIssuer string,
	registrationEndpoint string,
	redirectURI string,
) (mcpAuthClientCredentials, error) {
	queries := assistantrepo.New(s.core.db)
	coordinationCtx, cancel := context.WithTimeout(ctx, mcpOAuthCoordinationMax)
	defer cancel()
	pollDelay := mcpOAuthPollMin
	claimLease := pgtype.Interval{
		Microseconds: mcpOAuthRegistrationTTL.Microseconds(),
		Days:         0,
		Months:       0,
		Valid:        true,
	}

	for {
		now := time.Now()
		usableAfter := pgtype.Timestamptz{
			Time:             now.Add(mcpAuthFlowTTL),
			InfinityModifier: pgtype.Finite,
			Valid:            true,
		}
		existing, err := queries.GetAssistantMCPOAuthClient(coordinationCtx, assistantrepo.GetAssistantMCPOAuthClientParams{
			ProjectID:         projectID,
			AssistantID:       assistantID,
			OauthServerIssuer: oauthServerIssuer,
			RedirectUri:       redirectURI,
			UsableAfter:       usableAfter,
			ClaimLease:        claimLease,
		})
		if err == nil && existing.Usable.Bool {
			return mcpAuthClientCredentials{
				ClientID:              existing.ClientID.String,
				ClientSecretEncrypted: existing.ClientSecretEncrypted.String,
			}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			if err != nil {
				return mcpAuthClientCredentials{}, fmt.Errorf("get assistant mcp oauth client: %w", err)
			}
		}

		if errors.Is(err, pgx.ErrNoRows) || existing.Claimable.Bool {
			registrationOwner := uuid.New()
			claimed, err := queries.ClaimAssistantMCPOAuthClientRegistration(coordinationCtx, assistantrepo.ClaimAssistantMCPOAuthClientRegistrationParams{
				ProjectID:         projectID,
				AssistantID:       assistantID,
				OauthServerIssuer: oauthServerIssuer,
				RedirectUri:       redirectURI,
				RegistrationOwner: uuid.NullUUID{UUID: registrationOwner, Valid: true},
				ClaimLease:        claimLease,
				UsableAfter:       usableAfter,
			})
			if err != nil {
				return mcpAuthClientCredentials{}, fmt.Errorf("claim assistant mcp oauth client registration: %w", err)
			}
			if claimed == 1 {
				return s.completeMCPAuthClientRegistration(
					coordinationCtx,
					queries,
					projectID,
					assistantID,
					oauthServerIssuer,
					registrationOwner,
					registrationEndpoint,
					redirectURI,
				)
			}
		}

		poll := time.NewTimer(mcpOAuthPollDelay(pollDelay))
		select {
		case <-coordinationCtx.Done():
			if !poll.Stop() {
				select {
				case <-poll.C:
				default:
				}
			}
			return mcpAuthClientCredentials{}, fmt.Errorf("wait for assistant mcp oauth client registration: %w", coordinationCtx.Err())
		case <-poll.C:
		}
		pollDelay = min(pollDelay*2, mcpOAuthPollMax)
	}
}

func mcpOAuthPollDelay(base time.Duration) time.Duration {
	var randomByte [1]byte
	if _, err := rand.Read(randomByte[:]); err != nil {
		return base
	}
	return base/2 + time.Duration(randomByte[0])*base/(2*255)
}

func (s *Service) completeMCPAuthClientRegistration(
	ctx context.Context,
	queries *assistantrepo.Queries,
	projectID uuid.UUID,
	assistantID uuid.UUID,
	oauthServerIssuer string,
	registrationOwner uuid.UUID,
	registrationEndpoint string,
	redirectURI string,
) (mcpAuthClientCredentials, error) {
	registrationCtx, cancel := context.WithTimeout(ctx, mcpOAuthRegistrationMax)
	registration, err := s.registerMCPAuthClient(
		registrationCtx,
		registrationEndpoint,
		redirectURI,
		"Gram Assistant "+assistantID.String(),
	)
	cancel()
	if err != nil {
		s.abandonMCPAuthClientRegistration(ctx, queries, projectID, assistantID, oauthServerIssuer, registrationOwner)
		return mcpAuthClientCredentials{}, err
	}
	if registration.ClientSecretExpiresAt < 0 {
		s.abandonMCPAuthClientRegistration(ctx, queries, projectID, assistantID, oauthServerIssuer, registrationOwner)
		return mcpAuthClientCredentials{}, fmt.Errorf("registration response has invalid client_secret_expires_at")
	}
	if registration.ClientSecretExpiresAt > 0 && !time.Unix(registration.ClientSecretExpiresAt, 0).After(time.Now().Add(mcpAuthFlowTTL)) {
		s.abandonMCPAuthClientRegistration(ctx, queries, projectID, assistantID, oauthServerIssuer, registrationOwner)
		return mcpAuthClientCredentials{}, fmt.Errorf("registration response client secret expires before the authorization flow")
	}

	clientSecretEncrypted, err := s.core.encryptionClient.Encrypt([]byte(registration.ClientSecret))
	if err != nil {
		s.abandonMCPAuthClientRegistration(ctx, queries, projectID, assistantID, oauthServerIssuer, registrationOwner)
		return mcpAuthClientCredentials{}, fmt.Errorf("encrypt mcp client secret: %w", err)
	}
	clientSecretExpiresAt := pgtype.Timestamptz{
		Time:             time.Time{},
		InfinityModifier: pgtype.Finite,
		Valid:            false,
	}
	if registration.ClientSecretExpiresAt > 0 {
		clientSecretExpiresAt = pgtype.Timestamptz{
			Time:             time.Unix(registration.ClientSecretExpiresAt, 0),
			InfinityModifier: pgtype.Finite,
			Valid:            true,
		}
	}
	persistenceCtx, persistenceCancel := context.WithTimeout(context.WithoutCancel(ctx), mcpOAuthPersistenceMax)
	defer persistenceCancel()
	completed, err := queries.CompleteAssistantMCPOAuthClientRegistration(persistenceCtx, assistantrepo.CompleteAssistantMCPOAuthClientRegistrationParams{
		ClientID:              pgtype.Text{String: registration.ClientID, Valid: true},
		ClientSecretEncrypted: pgtype.Text{String: clientSecretEncrypted, Valid: true},
		ClientSecretExpiresAt: clientSecretExpiresAt,
		ProjectID:             projectID,
		AssistantID:           assistantID,
		OauthServerIssuer:     oauthServerIssuer,
		RegistrationOwner:     uuid.NullUUID{UUID: registrationOwner, Valid: true},
	})
	if err != nil {
		s.abandonMCPAuthClientRegistration(ctx, queries, projectID, assistantID, oauthServerIssuer, registrationOwner)
		return mcpAuthClientCredentials{}, fmt.Errorf("complete assistant mcp oauth client registration: %w", err)
	}
	if completed != 1 {
		return mcpAuthClientCredentials{}, fmt.Errorf("assistant mcp oauth client registration lease was lost")
	}

	return mcpAuthClientCredentials{
		ClientID:              registration.ClientID,
		ClientSecretEncrypted: clientSecretEncrypted,
	}, nil
}

func (s *Service) abandonMCPAuthClientRegistration(
	ctx context.Context,
	queries *assistantrepo.Queries,
	projectID uuid.UUID,
	assistantID uuid.UUID,
	oauthServerIssuer string,
	registrationOwner uuid.UUID,
) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()

	err := queries.AbandonAssistantMCPOAuthClientRegistration(cleanupCtx, assistantrepo.AbandonAssistantMCPOAuthClientRegistrationParams{
		ProjectID:         projectID,
		AssistantID:       assistantID,
		OauthServerIssuer: oauthServerIssuer,
		RegistrationOwner: uuid.NullUUID{UUID: registrationOwner, Valid: true},
	})
	if err != nil {
		s.logger.WarnContext(cleanupCtx, "failed to release assistant mcp oauth client registration claim", attr.SlogError(err))
	}
}

func (s *Service) invalidateMCPAuthClient(ctx context.Context, projectID, assistantID uuid.UUID, oauthServerIssuer, clientID string) {
	if oauthServerIssuer == "" || clientID == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mcpOAuthPersistenceMax)
	defer cancel()

	err := assistantrepo.New(s.core.db).InvalidateAssistantMCPOAuthClient(cleanupCtx, assistantrepo.InvalidateAssistantMCPOAuthClientParams{
		ProjectID:         projectID,
		AssistantID:       assistantID,
		OauthServerIssuer: oauthServerIssuer,
		ClientID:          pgtype.Text{String: clientID, Valid: true},
	})
	if err != nil {
		s.logger.WarnContext(cleanupCtx, "failed to invalidate assistant mcp oauth client", attr.SlogError(err))
	}
}

func (s *Service) registerMCPAuthClient(ctx context.Context, endpoint, redirectURI, clientName string) (mcpAuthClientRegistrationResponse, error) {
	payload := mcpAuthClientRegistrationRequest{
		ClientName:              clientName,
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "client_secret_basic",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return mcpAuthClientRegistrationResponse{}, fmt.Errorf("marshal registration request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return mcpAuthClientRegistrationResponse{}, fmt.Errorf("build registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.core.guardianPolicy.Client(guardian.WithAllowedSchemes("http")).Do(req)
	if err != nil {
		return mcpAuthClientRegistrationResponse{}, fmt.Errorf("send registration request: %w", err)
	}
	defer o11y.NoLogDefer(func() error {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.Body.Close()
	})
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return mcpAuthClientRegistrationResponse{}, fmt.Errorf("read registration response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mcpAuthClientRegistrationResponse{}, fmt.Errorf("registration failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out mcpAuthClientRegistrationResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return mcpAuthClientRegistrationResponse{}, fmt.Errorf("decode registration response: %w", err)
	}
	if out.ClientID == "" {
		return mcpAuthClientRegistrationResponse{}, fmt.Errorf("registration response missing client_id")
	}
	if out.ClientSecret == "" {
		return mcpAuthClientRegistrationResponse{}, fmt.Errorf("registration response missing client_secret")
	}
	return out, nil
}

func (s *Service) consumeMCPAuthGrant(ctx context.Context, claims *assistanttokens.MCPAuthFlowClaims, code string) error {
	verifier, err := s.core.encryptionClient.Decrypt(claims.CodeVerifier)
	if err != nil {
		return fmt.Errorf("decrypt pkce verifier: %w", err)
	}
	clientSecret, err := s.core.encryptionClient.Decrypt(claims.ClientSecret)
	if err != nil {
		return fmt.Errorf("decrypt mcp client secret: %w", err)
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", claims.RedirectURI)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claims.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// RFC 6749 §2.3.1: client credentials must be form-urlencoded before
	// going into the Basic authorization header. Upstreams that decode per
	// spec (e.g. Snowflake) reject raw credentials containing '+' or '%'.
	req.SetBasicAuth(url.QueryEscape(claims.ClientID), url.QueryEscape(clientSecret))
	resp, err := s.core.guardianPolicy.Client(
		guardian.WithDefaultRetryConfig(),
		guardian.WithAllowedSchemes("http"),
	).Do(req)
	if err != nil {
		return fmt.Errorf("send token request: %w", err)
	}
	defer o11y.NoLogDefer(func() error {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.Body.Close()
	})
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var tokenError struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &tokenError) == nil && tokenError.Error != "" {
			return &mcpOAuthTokenError{Code: tokenError.Error, StatusCode: resp.StatusCode}
		}
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
	q.Set("requireUserIdentity", "1")
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
