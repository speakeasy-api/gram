// OAuth 2.1 token endpoint (RFC 6749 §4.1.3 / §6) for the issuer-gated
// authn-challenge surface. HandleToken dispatches on grant_type to one of
// the two grant handlers below; both mint and persist an RFC 6749 §5.1
// response through mintSession.

package mcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// tokenResponse is the RFC 6749 §5.1 successful token response shape, plus
// `refresh_token` since we issue them on every grant.
//
// `scope` is intentionally absent: RFC 6749 §5.1 says the returned `scope`
// is the scope of the issued access token, and our access-token JWT
// carries no scope claim — no enforcement, no persistence. Emitting a
// `scope` field here would assert token state we don't hold. Restore it
// when /token mints scope-bearing access tokens.
type tokenResponse struct {
	AccessToken            string `json:"access_token"`
	TokenType              string `json:"token_type"`
	ExpiresIn              int64  `json:"expires_in"`
	RefreshToken           string `json:"refresh_token,omitempty"`
	AuthorizationExpiresIn int64  `json:"authorization_expires_in"`
}

const (
	// refreshTokenReplayGracePeriod is intentionally short: it only covers
	// clients that issue parallel refresh requests from several open sessions.
	refreshTokenReplayGracePeriod     = 30 * time.Second
	refreshTokenReplayWait            = 5 * time.Second
	refreshTokenReplayInitialPollWait = 20 * time.Millisecond
	refreshTokenReplayMaxPollWait     = 1 * time.Second
)

// userSessionRefreshReplay is the encrypted result of a recent refresh-token
// rotation. Redis never holds tokens or session identity in plaintext.
type userSessionRefreshReplay struct {
	// Key identifies the issuer and hashed refresh token being replayed.
	Key string `json:"key"`

	// Ciphertext holds the AES-GCM-encrypted replay payload.
	Ciphertext string `json:"ciphertext"`
}

type userSessionRefreshReplayPayload struct {
	AccessExpiresAt        time.Time           `json:"access_expires_at"`
	AudienceURN            string              `json:"audience_urn"`
	AuthorizationExpiresAt time.Time           `json:"authorization_expires_at"`
	ClientID               uuid.UUID           `json:"client_id"`
	EndpointIssuer         string              `json:"endpoint_issuer"`
	ErrorDescription       string              `json:"error_description"`
	FailureReason          string              `json:"failure_reason"`
	JTI                    string              `json:"jti"`
	ReplayKey              string              `json:"replay_key"`
	Response               tokenResponse       `json:"response"`
	Subject                *urn.SessionSubject `json:"subject,omitempty"`
}

type mintSessionParams struct {
	AuthorizationExpiresAt *time.Time
	BaseURL                string
	DesiredSessionDuration *time.Duration
	Replayable             bool
	Subject                urn.SessionSubject
	ToolSelection          []byte
}

type mintedSession struct {
	AccessExpiresAt        time.Time
	AuthorizationExpiresAt time.Time
	Body                   []byte
	EndpointIssuer         string
	JTI                    string
	Response               tokenResponse
	Subject                urn.SessionSubject
}

type mintUserSessionAccessTokenParams struct {
	AccessExpiresAt time.Time
	AudienceURN     string
	ClientID        string
	Issuer          string
	JTI             string
	Subject         urn.SessionSubject
}

var _ cache.CacheableObject[userSessionRefreshReplay] = (*userSessionRefreshReplay)(nil)

func (r userSessionRefreshReplay) CacheKey() string { return r.Key }

func (r userSessionRefreshReplay) TTL() time.Duration { return refreshTokenReplayGracePeriod }

// HandleToken implements the OAuth 2.1 token endpoint (RFC 6749 §4.1.3 /
// §6). Mounted at `POST /mcp/{mcpSlug}/token`. Performs the common upfront
// work — parse form, load toolset, authenticate the client — then
// dispatches on grant_type to handleTokenAuthorizationCodeGrant or
// handleTokenRefreshTokenGrant. Both grant handlers mint and persist the
// RFC 6749 §5.1 response through mintSession.
func (s *Service) HandleToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogToolsetMCPSlug(mcpSlug))
	endpoint, err := s.LoadResolvedMcpEndpointBySlug(ctx, logger, mcpSlug, "mcp")
	if err != nil {
		return err
	}
	return s.ServeToken(w, r, endpoint)
}

// ServeToken is the post-resolution entry point for the OAuth 2.1
// token endpoint, shared by /mcp's HandleToken (toolset-keyed) and
// /x/mcp's mcp_endpoint-keyed route registration. Performs the common
// upfront work — parse form, authenticate the client — then dispatches
// on grant_type to handleTokenAuthorizationCodeGrant or
// handleTokenRefreshTokenGrant. Both grant handlers mint and persist the
// RFC 6749 §5.1 response through mintSession.
func (s *Service) ServeToken(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		return writeTokenError(ctx, w, s.logger, http.StatusBadRequest, "invalid_request", "failed to parse form")
	}

	logger := endpoint.LogWith(s.logger)

	grantType := r.PostForm.Get("grant_type")
	creds := extractClientCredentials(r)
	presentedAuthMethod := creds.method
	clientID, reason := resolvePresentedClientID(creds)
	if reason != "" {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authentication rejected", clientID, presentedAuthMethod, grantType, reason)
		return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "client_id is required")
	}

	// Base URL the AS metadata advertises — equals the JWT `iss` claim so
	// the two sides of the contract stay aligned across custom domains.
	// Computed before client authentication because an assertion's aud is
	// checked against URLs derived from it.
	baseURL := s.BaseURLForRequest(r)
	// lookupClientOnly: any CIMD row was persisted at authorize time, and
	// mid-flow token legs must keep working even if the issuer's admission
	// policy changes between legs.
	clientRow, err := s.resolveUserSessionClient(ctx, logger, endpoint, clientID, lookupClientOnly)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authentication rejected", clientID, presentedAuthMethod, grantType, "unknown_client_id")
			return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", clientAuthFailureDescription)
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
	}
	// The `disabled` admission mode is an off switch, so it applies to the
	// token leg too: an operator who turns CIMD off for an issuer expects
	// outstanding refresh tokens to stop working, not just new authorize
	// flows. It is a whole-class deny needing no catalog or custom-URL
	// consultation, so it costs one in-memory comparison.
	//
	// `presets` deliberately does NOT enforce here. Preset membership is
	// implicit and Gram-mutable — removing a catalog entry de-admits it on
	// every presets-mode issuer at deploy — so enforcing at /token would let
	// a one-line catalog edit terminate live sessions fleet-wide, surfacing
	// as a mid-session failure no client recovers from. Admission for
	// `presets` is a gate on STARTING a flow, never on continuing one.
	//
	// Note this stops the issuance of new tokens; access tokens already
	// minted stay valid until they expire (see AIS-406).
	if clientRow.ClientIDMetadataUri.Valid {
		mode, recognized := admission.ResolveMode(endpoint.CIMDAdmissionModeRaw.String, endpoint.CIMDAdmissionModeRaw.Valid)
		if !recognized {
			// Still fails closed below, but without this the rejection is
			// indistinguishable from an operator deliberately choosing
			// `disabled`, so a corrupt row would never be diagnosed from
			// the token leg.
			logger.ErrorContext(ctx, "unrecognized cimd admission mode stored on issuer, failing closed",
				attr.SlogCIMDAdmissionMode(endpoint.CIMDAdmissionModeRaw.String),
			)
		}
		if mode == admission.ModeDisabled {
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authentication rejected", clientID, presentedAuthMethod, grantType, "cimd_admission_disabled")
			return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "this server does not accept client ID metadata documents")
		}
	}
	// Authentication is decided by the method the row persisted, not by
	// whether the row is CIMD-resolved or carries a secret, so one rule
	// serves every registration source. Shared with the revocation endpoint.
	if reason := s.authenticateOAuthClient(ctx, logger, endpoint, clientAssertionAtToken, clientRow, creds, baseURL); reason != "" {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authentication rejected", clientID, presentedAuthMethod, grantType, reason)
		return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", clientAuthFailureDescription)
	}
	logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authenticated", clientID, presentedAuthMethod, grantType, "")

	switch grantType {
	case "authorization_code":
		return s.handleTokenAuthorizationCodeGrant(ctx, w, r, endpoint, clientRow, baseURL, presentedAuthMethod, logger)
	case "refresh_token":
		return s.handleTokenRefreshTokenGrant(ctx, w, r, endpoint, clientRow, baseURL, presentedAuthMethod, logger)
	default:
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth token request rejected", clientID, presentedAuthMethod, grantType, "unsupported_grant_type")
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
	}
}

// handleTokenAuthorizationCodeGrant implements RFC 6749 §4.1.3. Reads the
// authorization code from the form, atomically consumes the
// UserSessionGrant from Redis (single-use), validates redirect_uri + the
// S256 PKCE verifier, then mints a new session via mintSession.
//
// No re-check of user_session_consents: possession of a valid grant IS
// proof of consent. The grant was minted by HandleConsent's POST after
// writing the consent row, and we atomically consumed it here.
func (s *Service) handleTokenAuthorizationCodeGrant(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	endpoint *ResolvedMcpEndpoint,
	clientRow *usersessions_repo.UserSessionClient,
	baseURL string,
	presentedAuthMethod string,
	logger *slog.Logger,
) error {
	// Flow-outcome dimensions. This is the authorization_code grant
	// — the terminal leg of a user-facing OAuth flow — so every rejection
	// below is a flow failure at the token stage, and the success path is the
	// single point that counts a completion. The refresh_token grant handler
	// deliberately records neither: refresh is not part of an initial flow.
	issuerID := endpoint.UserSessionIssuerID.String()
	mcpSlug := endpoint.Slug

	req := usersessions.AuthCodeTokenRequestFromForm(r.PostForm)
	req.SetDefaults()
	if err := req.Validate(); err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth authorization_code token request rejected", clientRow.ClientID, presentedAuthMethod, "authorization_code", "invalid_request")
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
		return writeTokenOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	// RFC 8707 §2, token leg. Built from the address this request arrived on,
	// so it matches the identifier the protected-resource metadata advertised
	// to the client that is now redeeming its code.
	canonicalResource, err := endpoint.RootURL(baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build token resource identifier").LogError(ctx, logger)
	}
	if err := oauthwire.ValidateResourceIndicators(req.Resources, canonicalResource); err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth authorization_code token request rejected", clientRow.ClientID, presentedAuthMethod, "authorization_code", "resource_mismatch")
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
		return writeTokenOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	// Inspect the endpoint binding before consuming the code so presenting it on
	// another endpoint cannot burn the legitimate client's grant. Once that
	// authority matches, GETDEL atomically elects one redemption winner; client,
	// redirect, and PKCE misuse intentionally burn the single-use code.
	grantKey := "userSessionGrant:" + endpoint.UserSessionIssuerID.String() + ":" + req.Code
	grant, err := s.userSessionGrantCache.Get(ctx, grantKey)
	if err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth authorization_code token request rejected", clientRow.ClientID, presentedAuthMethod, "authorization_code", "code_not_found_or_expired")
		// Deliberately NOT counted as a flow failure: a missing/expired code is
		// ambiguous. An expired code is closer to abandonment than an errant
		// config, and a replayed or retried code would double-count an outcome
		// already recorded on the first redemption. It falls into the
		// started-without-terminal gap instead, keeping `failed` to unambiguous
		// config/code/client errors.
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code not found or expired")
	}

	// Grant in hand: stamp the flow id so the token leg shares the correlation
	// key minted at /authorize and carried through the grant.
	logger = logger.With(attr.SlogOAuthFlowID(grant.FlowID))

	// New grants are bound to the exact endpoint authority consented by the
	// subject. A nil snapshot denotes only a grant minted before this field
	// landed; the authorization-code TTL bounds that compatibility window.
	if grant.Endpoint != nil {
		if err := endpoint.ValidateGrant(ctx, s.db, *grant.Endpoint, grant.UserSessionIssuerID, baseURL); err != nil {
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth authorization_code token request rejected", clientRow.ClientID, presentedAuthMethod, "authorization_code", "code_endpoint_mismatch")
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
			return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code not found or expired")
		}
	}

	grant, err = s.userSessionGrantCache.GetAndDelete(ctx, grantKey)
	if err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth authorization_code token request rejected", clientRow.ClientID, presentedAuthMethod, "authorization_code", "code_already_redeemed")
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code not found or expired")
	}
	// Recheck the consumed value so this remains safe if a future writer ever
	// replaces grants under an existing key between the peek and GETDEL.
	if grant.Endpoint != nil {
		if err := endpoint.ValidateGrant(ctx, s.db, *grant.Endpoint, grant.UserSessionIssuerID, baseURL); err != nil {
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth authorization_code token request rejected", clientRow.ClientID, presentedAuthMethod, "authorization_code", "code_endpoint_mismatch")
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
			return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code not found or expired")
		}
	}

	if grant.ClientID != clientRow.ClientID {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth authorization_code token request rejected", clientRow.ClientID, presentedAuthMethod, "authorization_code", "code_client_mismatch")
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code was issued to a different client")
	}
	if grant.RedirectURI != req.RedirectURI {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth authorization_code token request rejected", clientRow.ClientID, presentedAuthMethod, "authorization_code", "redirect_uri_mismatch")
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the original request")
	}
	if !verifyPKCES256(req.CodeVerifier, grant.CodeChallenge) {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth authorization_code token request rejected", clientRow.ClientID, presentedAuthMethod, "authorization_code", "pkce_mismatch")
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
	}

	var desiredSessionDuration *time.Duration
	if grant.DesiredSessionDurationHours > 0 {
		d := time.Duration(grant.DesiredSessionDurationHours) * time.Hour
		desiredSessionDuration = &d
	}
	var toolSelection []byte
	if grant.ToolSelection != nil {
		// Codes are cached issuer-wide, so a sibling endpoint sharing the
		// issuer could otherwise redeem this code and mint a session whose
		// selection is bound to another endpoint's resource — rejected at use
		// time anyway, but failing the redemption is cheaper than a
		// 200-then-401 loop.
		if grant.ToolSelection.Resource != endpointToolSelectionResource(endpoint) {
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth authorization_code token request rejected", clientRow.ClientID, presentedAuthMethod, "authorization_code", "tool_selection_resource_mismatch")
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
			return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "authorization code is bound to a different MCP endpoint")
		}
		encoded, merr := json.Marshal(grant.ToolSelection)
		if merr != nil {
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
			return oops.E(oops.CodeUnexpected, merr, "encode tool selection").LogError(ctx, logger)
		}
		toolSelection = encoded
	}

	minted, err := s.mintSession(ctx, endpoint, clientRow, usersessions_repo.New(s.db), mintSessionParams{
		AuthorizationExpiresAt: nil,
		BaseURL:                baseURL,
		DesiredSessionDuration: desiredSessionDuration,
		Replayable:             false,
		Subject:                grant.Subject,
		ToolSelection:          toolSelection,
	}, logger)
	if err != nil {
		// Almost all errors here occur before the 200 is written — issuer
		// lookup, session_duration validation, signing, or persisting the
		// user_sessions row — so no token reached the client and failed is
		// correct. The lone post-commit case is a failure writing the response
		// body after a 200 + persisted session (e.g. the client dropped the
		// connection): we still count failed, because the client received no
		// usable token, so the flow did not complete from its perspective.
		// Conservatively bucketing this rare case as failed (not completed)
		// keeps completed meaning "a token the client could actually use."
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
		return err
	}
	if err := writeTokenSuccess(ctx, w, logger, minted.Body); err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageToken)
		return err
	}

	s.metrics.RecordOAuthFlowCompleted(ctx, issuerID, mcpSlug)
	logger.InfoContext(ctx, "oauth flow completed")
	return nil
}

// handleTokenRefreshTokenGrant implements RFC 6749 §6 (and OAuth 2.1's
// refresh-token rotation guidance). Hashes the supplied refresh token,
// elects one rotation winner, atomically replaces the matching user_sessions
// row with a successor, then pushes the old access token's JTI into the
// revocation cache. Concurrent
// replays receive the winner's response during a short grace period. A replay
// through another endpoint surface re-signs only the access token for that
// surface; the refresh grant remains single-use in persistent storage.
//
// Client binding: the soft-deleted row's user_session_client_id MUST match
// the authenticated client. This blocks Client B from refreshing tokens
// issued to Client A even if B somehow obtains the opaque refresh token.
func (s *Service) handleTokenRefreshTokenGrant(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	endpoint *ResolvedMcpEndpoint,
	clientRow *usersessions_repo.UserSessionClient,
	baseURL string,
	presentedAuthMethod string,
	logger *slog.Logger,
) error {
	req := usersessions.RefreshTokenRequestFromForm(r.PostForm)
	req.SetDefaults()
	if err := req.Validate(); err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "invalid_request")
		return writeTokenOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	// RFC 8707 §2 applies to the refresh leg too: MCP 2026-07-28 has clients
	// send `resource` on every token request, so a rotation naming another
	// server is the same misconfiguration as it is on the authorization_code
	// grant. No flow-failure metric here — a refresh is not part of an initial
	// flow, and the completion ratio counts only those.
	canonicalResource, err := endpoint.RootURL(baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build token resource identifier").LogError(ctx, logger)
	}
	if err := oauthwire.ValidateResourceIndicators(req.Resources, canonicalResource); err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "resource_mismatch")
		return writeTokenOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	refreshTokenHash := sha256Hex(req.RefreshToken)
	replayKey := "userSessionRefreshReplay:" + endpoint.UserSessionIssuerID.String() + ":" + refreshTokenHash
	lockKey := "lock:" + replayKey
	lockOwner, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate refresh replay lock owner").LogError(ctx, logger)
	}
	timeout := time.NewTimer(refreshTokenReplayWait)
	defer timeout.Stop()
	pollWait := refreshTokenReplayInitialPollWait
	poll := time.NewTimer(pollWait)
	defer poll.Stop()

	for {
		// A completed outcome is authoritative and avoids renewing a coordination
		// lock after the original winner has already published.
		if replay, replayErr := s.userSessionRefreshReplayCache.Get(ctx, replayKey); replayErr == nil {
			return s.writeRefreshTokenReplay(ctx, w, r, endpoint, clientRow, baseURL, presentedAuthMethod, replayKey, replay, logger)
		}

		rotationWinner := true
		ownsLock := false
		if s.userSessionRefreshReplayCoordination != nil {
			var coordinationErr error
			if leases, ok := s.userSessionRefreshReplayCoordination.(cache.LeaseCache); ok {
				rotationWinner, coordinationErr = leases.AcquireLease(ctx, lockKey, lockOwner, refreshTokenReplayGracePeriod)
			} else {
				coordinationErr = errors.New("refresh replay cache does not support ownership-aware leases")
			}
			if coordinationErr != nil {
				// The database claim remains authoritative when Redis is
				// unavailable; only the compatibility grace period is lost.
				rotationWinner = true
				logger.WarnContext(ctx, "failed to coordinate refresh token replay grace period", attr.SlogError(coordinationErr))
			} else {
				ownsLock = rotationWinner
			}
		}
		if rotationWinner {
			releaseLease, rotationErr := s.rotateRefreshToken(
				ctx,
				w,
				r,
				endpoint,
				clientRow,
				baseURL,
				presentedAuthMethod,
				refreshTokenHash,
				replayKey,
				ownsLock,
				logger,
			)
			if ownsLock && releaseLease {
				// Safe rollback paths and published outcomes release immediately.
				// Ambiguous commits and post-commit publication failures retain the
				// lease until its TTL so retries cannot misclassify a consumed token.
				s.releaseRefreshTokenReplayLock(ctx, lockKey, lockOwner, logger)
			}
			return rotationErr
		}

		replay, replayErr := s.userSessionRefreshReplayCache.Get(ctx, replayKey)
		if replayErr == nil {
			return s.writeRefreshTokenReplay(ctx, w, r, endpoint, clientRow, baseURL, presentedAuthMethod, replayKey, replay, logger)
		}
		if !errors.Is(replayErr, redisCache.ErrCacheMiss) {
			logger.WarnContext(ctx, "failed to read refresh token replay response", attr.SlogError(replayErr))
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay unavailable", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_cache_error")
			return writeTokenError(ctx, w, logger, http.StatusServiceUnavailable, "temporarily_unavailable", "refresh token rotation is still in progress")
		}

		select {
		case <-ctx.Done():
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay interrupted", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_context_cancelled")
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return writeTokenError(ctx, w, logger, http.StatusServiceUnavailable, "temporarily_unavailable", "refresh token rotation is still in progress")
			}
			return oops.E(oops.CodeCanceled, ctx.Err(), "wait for refresh token rotation")
		case <-timeout.C:
			if replay, finalErr := s.userSessionRefreshReplayCache.Get(ctx, replayKey); finalErr == nil {
				return s.writeRefreshTokenReplay(ctx, w, r, endpoint, clientRow, baseURL, presentedAuthMethod, replayKey, replay, logger)
			} else if !errors.Is(finalErr, redisCache.ErrCacheMiss) {
				logger.WarnContext(ctx, "failed final refresh token replay lookup", attr.SlogError(finalErr))
			}
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay unavailable", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_wait_timeout")
			return writeTokenError(ctx, w, logger, http.StatusServiceUnavailable, "temporarily_unavailable", "refresh token rotation is still in progress")
		case <-poll.C:
			pollWait = min(pollWait*2, refreshTokenReplayMaxPollWait)
			poll.Reset(pollWait)
		}
	}
}

func (s *Service) rotateRefreshToken(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	endpoint *ResolvedMcpEndpoint,
	clientRow *usersessions_repo.UserSessionClient,
	baseURL string,
	presentedAuthMethod string,
	refreshTokenHash string,
	replayKey string,
	canPublishFailure bool,
	logger *slog.Logger,
) (releaseLease bool, err error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return true, oops.E(oops.CodeUnexpected, err, "begin refresh token rotation").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := usersessions_repo.New(dbtx)
	oldSession, err := txRepo.RevokeUserSessionByRefreshTokenHash(ctx, usersessions_repo.RevokeUserSessionByRefreshTokenHashParams{
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		RefreshTokenHash:    refreshTokenHash,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Coordination can degrade independently of the response cache
			// during a Redis reconnect. Adopt a completed winner when possible.
			replay, replayErr := s.userSessionRefreshReplayCache.Get(ctx, replayKey)
			if replayErr == nil {
				return true, s.writeRefreshTokenReplay(ctx, w, r, endpoint, clientRow, baseURL, presentedAuthMethod, replayKey, replay, logger)
			}
			if !errors.Is(replayErr, redisCache.ErrCacheMiss) {
				logger.WarnContext(ctx, "failed to read refresh token replay after database claim loss", attr.SlogError(replayErr))
				return true, writeTokenError(ctx, w, logger, http.StatusServiceUnavailable, "temporarily_unavailable", "refresh token rotation outcome is unavailable")
			}

			if !canPublishFailure {
				// Without an owned coordination lease, a missing active row is
				// indistinguishable from another winner's unpublished commit.
				return true, writeTokenError(ctx, w, logger, http.StatusServiceUnavailable, "temporarily_unavailable", "refresh token rotation outcome is unavailable")
			}

			published := s.storeRefreshTokenReplayFailure(
				ctx, replayKey, clientRow.ID,
				"refresh_token_unknown_or_already_used",
				"refresh_token is unknown or already used", logger,
			)
			if !published {
				if existing, existingErr := s.userSessionRefreshReplayCache.Get(ctx, replayKey); existingErr == nil {
					return true, s.writeRefreshTokenReplay(ctx, w, r, endpoint, clientRow, baseURL, presentedAuthMethod, replayKey, existing, logger)
				}
			}
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_unknown_or_already_used")
			return true, writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token is unknown or already used")
		}
		return true, oops.E(oops.CodeUnexpected, err, "revoke old refresh token").LogError(ctx, logger)
	}

	// Client mismatches and expired grants are terminal. Commit their
	// revocation so a leaked or dead token cannot be retried by another leader.
	if !oldSession.UserSessionClientID.Valid || oldSession.UserSessionClientID.UUID != clientRow.ID {
		if err := dbtx.Commit(ctx); err != nil {
			return false, oops.E(oops.CodeUnexpected, err, "commit mismatched refresh token revocation").LogError(ctx, logger)
		}
		postCommitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if revokeErr := s.chatSessionsManager.RevokeToken(postCommitCtx, oldSession.Jti); revokeErr != nil {
			logger.WarnContext(postCommitCtx, "failed to revoke mismatched refresh token jti", attr.SlogError(revokeErr))
		}
		published := s.storeRefreshTokenReplayFailure(
			postCommitCtx,
			replayKey,
			clientRow.ID,
			"refresh_token_client_mismatch",
			"refresh_token was issued to a different client",
			logger,
		)
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_client_mismatch")
		return published, writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token was issued to a different client")
	}

	if !oldSession.RefreshExpiresAt.Valid || !oldSession.RefreshExpiresAt.Time.After(time.Now()) {
		if err := dbtx.Commit(ctx); err != nil {
			return false, oops.E(oops.CodeUnexpected, err, "commit expired refresh token revocation").LogError(ctx, logger)
		}
		postCommitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if revokeErr := s.chatSessionsManager.RevokeToken(postCommitCtx, oldSession.Jti); revokeErr != nil {
			logger.WarnContext(postCommitCtx, "failed to revoke expired refresh token jti", attr.SlogError(revokeErr))
		}
		published := s.storeRefreshTokenReplayFailure(
			postCommitCtx,
			replayKey,
			clientRow.ID,
			"refresh_token_expired",
			"refresh_token has expired",
			logger,
		)
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_expired")
		return published, writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token has expired")
	}

	// Session length is an absolute authorization lifetime. Rotation carries
	// that deadline forward verbatim; it never opens a fresh authorization
	// window merely because the client exchanged its refresh token.
	authorizationExpiresAt := oldSession.RefreshExpiresAt.Time
	// Tool selection rides refresh slides verbatim; reject malformed or
	// cross-endpoint policies before consuming the refresh transaction.
	oldSelection, perr := toolfilter.ParseSessionSelection(oldSession.ToolSelection)
	if perr != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "tool_selection_malformed")
		return true, writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "session tool selection is malformed; reauthorize")
	}
	if oldSelection != nil && oldSelection.Resource != endpointToolSelectionResource(endpoint) {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "tool_selection_resource_mismatch")
		return true, writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "session tool selection is bound to a different MCP endpoint; reauthorize")
	}

	minted, err := s.mintSession(ctx, endpoint, clientRow, txRepo, mintSessionParams{
		AuthorizationExpiresAt: &authorizationExpiresAt,
		BaseURL:                baseURL,
		DesiredSessionDuration: nil,
		Replayable:             true,
		Subject:                oldSession.SubjectUrn,
		ToolSelection:          oldSession.ToolSelection,
	}, logger)
	if err != nil {
		return true, err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "commit refresh token rotation").LogError(ctx, logger)
	}

	// The database rotation is durable before either Redis side effect. Publish
	// with a bounded detached context so a client disconnect cannot strand the
	// committed successor before another request can recover it.
	postCommitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	published := true
	if cacheErr := s.storeRefreshTokenReplay(postCommitCtx, replayKey, userSessionRefreshReplayPayload{
		AccessExpiresAt:        minted.AccessExpiresAt,
		AudienceURN:            endpoint.AudienceURN,
		AuthorizationExpiresAt: minted.AuthorizationExpiresAt,
		ClientID:               clientRow.ID,
		EndpointIssuer:         minted.EndpointIssuer,
		ErrorDescription:       "",
		FailureReason:          "",
		JTI:                    minted.JTI,
		ReplayKey:              "",
		Response:               minted.Response,
		Subject:                &minted.Subject,
	}); cacheErr != nil {
		published = false
		logger.WarnContext(postCommitCtx, "failed to cache refresh token replay response", attr.SlogError(cacheErr))
	}
	if revokeErr := s.chatSessionsManager.RevokeToken(postCommitCtx, oldSession.Jti); revokeErr != nil {
		logger.WarnContext(postCommitCtx, "failed to revoke old access token jti on refresh", attr.SlogError(revokeErr))
	}

	return published, writeTokenSuccess(ctx, w, logger, minted.Body)
}

func (s *Service) releaseRefreshTokenReplayLock(ctx context.Context, lockKey, lockOwner string, logger *slog.Logger) {
	if s.userSessionRefreshReplayCoordination == nil {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	leases, ok := s.userSessionRefreshReplayCoordination.(cache.LeaseCache)
	if !ok {
		logger.WarnContext(releaseCtx, "refresh replay cache does not support ownership-aware lease release")
		return
	}
	if _, err := leases.ReleaseLeaseIfOwner(releaseCtx, lockKey, lockOwner); err != nil {
		logger.WarnContext(releaseCtx, "failed to release refresh token replay lock", attr.SlogError(err))
	}
}

func (s *Service) writeRefreshTokenReplay(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	endpoint *ResolvedMcpEndpoint,
	clientRow *usersessions_repo.UserSessionClient,
	baseURL string,
	presentedAuthMethod string,
	replayKey string,
	replay userSessionRefreshReplay,
	logger *slog.Logger,
) error {
	plaintext, err := s.enc.Decrypt(replay.Ciphertext)
	if err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay failed", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_decrypt_error")
		return oops.E(oops.CodeUnexpected, err, "decrypt refresh token replay response").LogError(ctx, logger)
	}
	var payload userSessionRefreshReplayPayload
	if err := json.Unmarshal([]byte(plaintext), &payload); err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay failed", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_payload_invalid")
		return oops.E(oops.CodeUnexpected, err, "unmarshal refresh token replay response").LogError(ctx, logger)
	}
	if subtle.ConstantTimeCompare([]byte(payload.ReplayKey), []byte(replayKey)) != 1 {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay failed", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_key_mismatch")
		return oops.E(oops.CodeUnexpected, nil, "refresh token replay response key mismatch").LogError(ctx, logger)
	}
	if payload.ErrorDescription != "" {
		failureReason := payload.FailureReason
		if failureReason == "" {
			failureReason = "refresh_token_replay_rejected"
		}
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", failureReason)
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", payload.ErrorDescription)
	}
	if payload.ClientID != clientRow.ID {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_client_mismatch")
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token was issued to a different client")
	}
	if now := time.Now(); !payload.AccessExpiresAt.After(now) || !payload.AuthorizationExpiresAt.After(now) {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_expired")
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token has expired")
	}
	if payload.Subject == nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay failed", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_payload_invalid")
		return oops.E(oops.CodeUnexpected, nil, "refresh token replay response is missing subject").LogError(ctx, logger)
	}

	endpointIssuer, err := endpoint.RootURL(baseURL)
	if err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay failed", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_resign_error")
		return oops.E(oops.CodeUnexpected, err, "build replay endpoint issuer URL").LogError(ctx, logger)
	}
	if payload.EndpointIssuer != endpointIssuer || payload.AudienceURN != endpoint.AudienceURN {
		accessToken, _, mintErr := s.mintUserSessionAccessToken(mintUserSessionAccessTokenParams{
			AccessExpiresAt: payload.AccessExpiresAt,
			AudienceURN:     endpoint.AudienceURN,
			ClientID:        clientRow.ClientID,
			Issuer:          endpointIssuer,
			JTI:             payload.JTI,
			Subject:         *payload.Subject,
		})
		if mintErr != nil {
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay failed", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_resign_error")
			return oops.E(oops.CodeUnexpected, mintErr, "mint refresh token replay jwt").LogError(ctx, logger)
		}
		payload.Response.AccessToken = accessToken
	}

	now := time.Now()
	payload.Response.ExpiresIn = max(0, int64(payload.AccessExpiresAt.Sub(now).Seconds()))
	payload.Response.AuthorizationExpiresIn = max(0, int64(payload.AuthorizationExpiresAt.Sub(now).Seconds()))
	body, err := json.Marshal(payload.Response)
	if err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay failed", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_payload_invalid")
		return oops.E(oops.CodeUnexpected, err, "marshal refresh token replay response").LogError(ctx, logger)
	}
	if err := writeTokenSuccess(ctx, w, logger, body); err != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay failed", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_replay_write_error")
		return err
	}

	logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token replay served", clientRow.ClientID, presentedAuthMethod, "refresh_token", "")
	s.metrics.RecordOAuthRefreshTokenReplayServed(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug)
	return nil
}

func (s *Service) storeRefreshTokenReplay(
	ctx context.Context,
	replayKey string,
	payload userSessionRefreshReplayPayload,
) error {
	payload.ReplayKey = replayKey
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal refresh token replay response: %w", err)
	}
	ciphertext, err := s.enc.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt refresh token replay response: %w", err)
	}
	if err := s.userSessionRefreshReplayCache.Store(ctx, userSessionRefreshReplay{
		Key:        replayKey,
		Ciphertext: ciphertext,
	}); err != nil {
		return fmt.Errorf("store refresh token replay response: %w", err)
	}
	return nil
}

func (s *Service) storeRefreshTokenReplayFailure(
	ctx context.Context,
	replayKey string,
	clientID uuid.UUID,
	failureReason string,
	description string,
	logger *slog.Logger,
) bool {
	payload := userSessionRefreshReplayPayload{
		AccessExpiresAt:        time.Time{},
		AudienceURN:            "",
		AuthorizationExpiresAt: time.Time{},
		ClientID:               clientID,
		EndpointIssuer:         "",
		ErrorDescription:       description,
		FailureReason:          failureReason,
		JTI:                    "",
		ReplayKey:              replayKey,
		Response: tokenResponse{
			AccessToken:            "",
			TokenType:              "",
			ExpiresIn:              0,
			RefreshToken:           "",
			AuthorizationExpiresIn: 0,
		},
		Subject: nil,
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		logger.WarnContext(ctx, "failed to marshal refresh token replay rejection", attr.SlogError(err))
		return false
	}
	ciphertext, err := s.enc.Encrypt(plaintext)
	if err != nil {
		logger.WarnContext(ctx, "failed to encrypt refresh token replay rejection", attr.SlogError(err))
		return false
	}
	stored, err := s.userSessionRefreshReplayCache.StoreIfAbsent(ctx, userSessionRefreshReplay{
		Key:        replayKey,
		Ciphertext: ciphertext,
	})
	if err != nil {
		logger.WarnContext(ctx, "failed to cache refresh token replay rejection", attr.SlogError(err))
		return false
	}
	return stored
}

func (s *Service) mintUserSessionAccessToken(params mintUserSessionAccessTokenParams) (accessToken, jti string, err error) {
	accessToken, jti, err = s.userSessionSigner.Mint(sessiontokens.MintParams{
		Subject:   params.Subject,
		Audience:  params.AudienceURN,
		Issuer:    params.Issuer,
		Lifetime:  0,
		ExpiresAt: &params.AccessExpiresAt,
		ClientID:  params.ClientID,
		JTI:       params.JTI,
	})
	if err != nil {
		return "", "", fmt.Errorf("mint session jwt: %w", err)
	}
	return accessToken, jti, nil
}

// accessTokenLifetime is the wall-clock validity of a minted access-token
// JWT. Hardcoded because OAuth 2.1 best practice is short access tokens
// regardless of session policy. mintSession caps it to the remaining
// authorization lifetime.
const accessTokenLifetime = 1 * time.Hour

// mintSession mints a new access-token JWT (HS256) and an opaque refresh token,
// then persists a fresh user_sessions row through queries. Refresh rotation
// supplies a transaction-backed repository so consuming the old refresh token
// and creating its successor commit atomically.
//
// Lifetimes:
//   - authorization: the subject's consent choice, capped by the issuer's
//     session_duration, and fixed for the lifetime of the grant.
//   - refresh token: the remaining authorization lifetime. Gram does not
//     impose a separate refresh-token idle timeout.
//   - access token: min(accessTokenLifetime, remaining authorization).
//
// `iss` / audience: the JWT issuer claim is built from baseURL (which the
// caller computes from custom-domain context so it matches what the AS
// metadata document advertises). The audience is the toolset URN
// `toolset:<UUID>`, globally unique even when slugs collide across
// projects — prevents cross-project replay.
// Params.DesiredSessionDuration is used only for an initial authorization: nil
// means "no explicit choice", falling back to the issuer's session_duration.
// Params.AuthorizationExpiresAt is used only for rotation and is carried from
// the prior row. Exactly one is normally non-nil. Params.Replayable requests a
// stable high-entropy JTI that can be reused when re-signing for another origin.
// Params.ToolSelection is the consent-screen policy persisted verbatim; refresh
// rotation carries the prior session's value forward.
func (s *Service) mintSession(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	clientRow *usersessions_repo.UserSessionClient,
	queries *usersessions_repo.Queries,
	params mintSessionParams,
	logger *slog.Logger,
) (*mintedSession, error) {
	now := time.Now()
	if params.AuthorizationExpiresAt == nil {
		// Resolve the issuer's session_duration — the maximum absolute
		// authorization lifetime. Microseconds-only: the issuer create handler
		// stores via conv.PtrToPGInterval which never sets Months/Days; if we
		// ever see those here, raw SQL bypassed the writer and the conversion
		// is calendar-dependent — fail rather than silently approximate.
		issuer, err := queries.GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
			ID:        endpoint.UserSessionIssuerID,
			ProjectID: endpoint.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "user_session_issuer not found")
			}
			return nil, oops.E(oops.CodeUnexpected, err, "lookup user session issuer").LogError(ctx, logger)
		}
		if !issuer.SessionDuration.Valid {
			return nil, oops.E(oops.CodeUnexpected, nil, "issuer session_duration is not set").LogError(ctx, logger)
		}
		if issuer.SessionDuration.Months != 0 || issuer.SessionDuration.Days != 0 {
			return nil, oops.E(oops.CodeUnexpected, nil, "issuer session_duration carries Months/Days; only Microseconds intervals are supported").LogError(ctx, logger)
		}
		maxLifetime := time.Duration(issuer.SessionDuration.Microseconds) * time.Microsecond
		if maxLifetime <= 0 {
			return nil, oops.E(oops.CodeUnexpected, nil, "issuer session_duration is non-positive").LogError(ctx, logger)
		}
		authorizationLifetime := maxLifetime
		if params.DesiredSessionDuration != nil && *params.DesiredSessionDuration > 0 {
			authorizationLifetime = min(*params.DesiredSessionDuration, maxLifetime)
		}
		deadline := now.Add(authorizationLifetime)
		params.AuthorizationExpiresAt = &deadline
	}
	authorizationLifetime := params.AuthorizationExpiresAt.Sub(now)
	if authorizationLifetime <= 0 {
		return nil, oops.E(oops.CodeUnauthorized, nil, "user authorization has expired").LogError(ctx, logger)
	}
	accessExpiresAt := now.Add(accessTokenLifetime)
	if params.AuthorizationExpiresAt.Before(accessExpiresAt) {
		accessExpiresAt = *params.AuthorizationExpiresAt
	}
	accessLifetime := accessExpiresAt.Sub(now)

	issuerURL, err := endpoint.RootURL(params.BaseURL)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build issuer URL").LogError(ctx, logger)
	}
	jti := ""
	if params.Replayable {
		jti, err = generateOpaqueToken()
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "generate replayable session jti").LogError(ctx, logger)
		}
	}
	access, jti, err := s.mintUserSessionAccessToken(mintUserSessionAccessTokenParams{
		AccessExpiresAt: accessExpiresAt,
		AudienceURN:     endpoint.AudienceURN,
		ClientID:        clientRow.ClientID,
		Issuer:          issuerURL,
		JTI:             jti,
		Subject:         params.Subject,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "mint session access token").LogError(ctx, logger)
	}

	refreshTokenRaw, err := generateOpaqueToken()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate refresh token").LogError(ctx, logger)
	}

	if _, err := queries.CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		UserSessionClientID: uuid.NullUUID{UUID: clientRow.ID, Valid: true},
		SubjectUrn:          params.Subject,
		Jti:                 jti,
		RefreshTokenHash:    sha256Hex(refreshTokenRaw),
		ExpiresAt:           pgtype.Timestamptz{Time: accessExpiresAt, InfinityModifier: 0, Valid: true},
		RefreshExpiresAt:    pgtype.Timestamptz{Time: *params.AuthorizationExpiresAt, InfinityModifier: 0, Valid: true},
		ToolSelection:       params.ToolSelection,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "persist user session").LogError(ctx, logger)
	}

	response := tokenResponse{
		AccessToken:            access,
		TokenType:              "Bearer",
		ExpiresIn:              int64(accessLifetime.Seconds()),
		RefreshToken:           refreshTokenRaw,
		AuthorizationExpiresIn: int64(authorizationLifetime.Seconds()),
	}
	body, err := json.Marshal(response)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "marshal token response").LogError(ctx, logger)
	}

	return &mintedSession{
		AccessExpiresAt:        accessExpiresAt,
		AuthorizationExpiresAt: *params.AuthorizationExpiresAt,
		Body:                   body,
		EndpointIssuer:         issuerURL,
		JTI:                    jti,
		Response:               response,
		Subject:                params.Subject,
	}, nil
}

func writeTokenSuccess(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, body []byte) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write token response").LogError(ctx, logger)
	}
	return nil
}

// writeTokenOAuthError unwraps a *oauthwire.Error to its code +
// description and forwards to writeTokenError. Falls back to a generic
// invalid_request if err is something else (shouldn't happen — Validate
// returns *oauthwire.Error).
func writeTokenOAuthError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, err error) error {
	if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
		return writeTokenError(ctx, w, logger, status, oauthErr.Code, oauthErr.Description)
	}
	return writeTokenError(ctx, w, logger, status, "invalid_request", err.Error())
}

// writeTokenError emits an RFC 6749 §5.2 token error response: 4xx with a
// JSON body { "error": "<code>", "error_description": "..." } and the
// no-store headers required by RFC 6749 §5.1.
func writeTokenError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, code, description string) error {
	body, err := json.Marshal(map[string]string{
		"error":             code,
		"error_description": description,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "marshal token error").LogError(ctx, logger)
	}

	logger.InfoContext(ctx, "token request rejected",
		attr.SlogOAuthError(code),
		attr.SlogOAuthErrorDescription(description),
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	if _, werr := w.Write(body); werr != nil {
		return oops.E(oops.CodeUnexpected, werr, "write token error body").LogError(ctx, logger)
	}
	return nil
}

// verifyPKCES256 reports whether code_verifier matches the stored
// code_challenge under the S256 method (RFC 7636 §4.6):
// BASE64URL-NO-PAD(SHA256(ASCII(code_verifier))) == code_challenge.
func verifyPKCES256(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}
