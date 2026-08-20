// OAuth 2.1 token endpoint (RFC 6749 §4.1.3 / §6) for the issuer-gated
// authn-challenge surface. HandleToken dispatches on grant_type to one of
// the two grant handlers below; both funnel through mintSessionAndRespond
// to write the RFC 6749 §5.1 response.

package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
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

// HandleToken implements the OAuth 2.1 token endpoint (RFC 6749 §4.1.3 /
// §6). Mounted at `POST /mcp/{mcpSlug}/token`. Performs the common upfront
// work — parse form, load toolset, authenticate the client — then
// dispatches on grant_type to handleTokenAuthorizationCodeGrant or
// handleTokenRefreshTokenGrant. Both grant handlers funnel through
// mintSessionAndRespond which writes the RFC 6749 §5.1 response.
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
// handleTokenRefreshTokenGrant. Both grant handlers funnel through
// mintSessionAndRespond which writes the RFC 6749 §5.1 response.
func (s *Service) ServeToken(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		return writeTokenError(ctx, w, s.logger, http.StatusBadRequest, "invalid_request", "failed to parse form")
	}

	logger := endpoint.LogWith(s.logger)

	grantType := r.PostForm.Get("grant_type")
	clientID, clientSecret, presentedAuthMethod, _ := extractClientCredentials(r)
	if clientID == "" {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authentication rejected", clientID, presentedAuthMethod, grantType, "missing_client_id")
		return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "client_id is required")
	}
	// lookupClientOnly: any CIMD row was persisted at authorize time, and
	// mid-flow token legs must keep working even if the issuer's admission
	// policy changes between legs.
	clientRow, err := s.resolveUserSessionClient(ctx, logger, endpoint, clientID, lookupClientOnly)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authentication rejected", clientID, presentedAuthMethod, grantType, "unknown_client_id")
			return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
	}
	// CIMD-resolved clients are public by construction (the AS only accepts
	// documents declaring token_endpoint_auth_method "none", and the schema
	// forbids a secret on CIMD rows). Reject any attempt to authenticate
	// one with credentials per RFC 6749 §5.2 — a URL-shaped client_id
	// cannot travel via HTTP Basic (r.BasicAuth does no percent-decoding),
	// so a legitimate CIMD client always presents form client_id + none.
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
	if clientRow.ClientIDMetadataUri.Valid && presentedAuthMethod != "none" {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authentication rejected", clientID, presentedAuthMethod, grantType, "cimd_client_presented_credentials")
		return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", `client_id metadata document clients must use token_endpoint_auth_method "none"`)
	}
	// Public clients (token_endpoint_auth_method=none) have a NULL hash:
	// PKCE / refresh-token possession is the integrity proof, no secret check.
	// Confidential clients MUST present a matching secret.
	if clientRow.ClientSecretHash.Valid {
		if err := bcrypt.CompareHashAndPassword([]byte(clientRow.ClientSecretHash.String), []byte(clientSecret)); err != nil {
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authentication rejected", clientID, presentedAuthMethod, grantType, "client_secret_mismatch")
			return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "client secret mismatch")
		}
	}
	logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authenticated", clientID, presentedAuthMethod, grantType, "")

	// Base URL the AS metadata advertises — equals the JWT `iss` claim so
	// the two sides of the contract stay aligned across custom domains.
	baseURL := s.BaseURLForRequest(r)

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
// S256 PKCE verifier, then mints a new session via mintSessionAndRespond.
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

	// Atomic GETDEL: single-use authorization code. If two clients race to
	// redeem the same code, exactly one wins the GETDEL; the other gets
	// ErrCacheMiss and is rejected as invalid_grant (RFC 6749 §4.1.2 / §10.5).
	grantKey := "userSessionGrant:" + endpoint.UserSessionIssuerID.String() + ":" + req.Code
	grant, err := s.userSessionGrantCache.GetAndDelete(ctx, grantKey)
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
	if err := s.mintSessionAndRespond(ctx, w, endpoint, clientRow, grant.Subject, desiredSessionDuration, nil, toolSelection, baseURL, logger); err != nil {
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

	s.metrics.RecordOAuthFlowCompleted(ctx, issuerID, mcpSlug)
	logger.InfoContext(ctx, "oauth flow completed")
	return nil
}

// handleTokenRefreshTokenGrant implements RFC 6749 §6 (and OAuth 2.1's
// refresh-token rotation guidance). Hashes the supplied refresh token,
// atomically soft-deletes the matching user_sessions row (single-use:
// concurrent refreshes race for the slot), pushes the old access token's
// JTI into the revocation cache, then mints a new session via
// mintSessionAndRespond.
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

	// Soft-delete by hash claims the single-use slot atomically. If the row
	// is already gone (unknown / replayed / revoked), pgx.ErrNoRows surfaces
	// here as invalid_grant.
	oldSession, err := usersessions_repo.New(s.db).RevokeUserSessionByRefreshTokenHash(ctx, usersessions_repo.RevokeUserSessionByRefreshTokenHashParams{
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		RefreshTokenHash:    sha256Hex(req.RefreshToken),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_unknown_or_already_used")
			return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token is unknown or already used")
		}
		return oops.E(oops.CodeUnexpected, err, "revoke old refresh token").LogError(ctx, logger)
	}

	// Client binding: refuse if the original session was minted for a
	// different client. We've already soft-deleted the row -- that's
	// intentional, the alternative would let a leaking client poke at others'
	// refresh tokens without invalidating them.
	if !oldSession.UserSessionClientID.Valid || oldSession.UserSessionClientID.UUID != clientRow.ID {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_client_mismatch")
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token was issued to a different client")
	}

	if !oldSession.RefreshExpiresAt.Valid || !oldSession.RefreshExpiresAt.Time.After(time.Now()) {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "refresh_token_expired")
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "refresh_token has expired")
	}

	// Best-effort: invalidate any access token still floating around from
	// the prior session row. If Redis is down, the access token will expire
	// naturally on its own clock; we'd rather mint than fail the refresh.
	if err := s.chatSessionsManager.RevokeToken(ctx, oldSession.Jti); err != nil {
		logger.WarnContext(ctx, "failed to revoke old access token jti on refresh", attr.SlogError(err))
	}

	// Session length is an absolute authorization lifetime. Rotation carries
	// that deadline forward verbatim; it never opens a fresh authorization
	// window merely because the client exchanged its refresh token.
	authorizationExpiresAt := oldSession.RefreshExpiresAt.Time
	// The tool selection rides refresh slides verbatim; only a fresh
	// authorization-code grant (which re-runs consent) replaces it. Validate
	// before minting: the serve path rejects a malformed or cross-endpoint
	// policy anyway, so refusing here returns invalid_grant (forcing reauth)
	// instead of consuming the refresh token and minting tokens that
	// immediately 401. The resource check matters on issuers shared across
	// endpoints: sessions are portable there, but a restrictive selection is
	// bound to the inventory the user consented on and must not slide onto a
	// sibling.
	oldSelection, perr := toolfilter.ParseSessionSelection(oldSession.ToolSelection)
	if perr != nil {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "tool_selection_malformed")
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "session tool selection is malformed; reauthorize")
	}
	if oldSelection != nil && oldSelection.Resource != endpointToolSelectionResource(endpoint) {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth refresh_token request rejected", clientRow.ClientID, presentedAuthMethod, "refresh_token", "tool_selection_resource_mismatch")
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "session tool selection is bound to a different MCP endpoint; reauthorize")
	}
	return s.mintSessionAndRespond(ctx, w, endpoint, clientRow, oldSession.SubjectUrn, nil, &authorizationExpiresAt, oldSession.ToolSelection, baseURL, logger)
}

// accessTokenLifetime is the wall-clock validity of a minted access-token
// JWT. Hardcoded because OAuth 2.1 best practice is short access tokens
// regardless of session policy. mintSessionAndRespond caps it to the remaining
// authorization lifetime.
const accessTokenLifetime = 1 * time.Hour

// mintSessionAndRespond mints a new access-token JWT (HS256) and an opaque
// refresh token, persists a fresh user_sessions row, and writes the RFC
// 6749 §5.1 response. Shared by the authorization_code and refresh_token
// grant handlers since both produce identical token responses.
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
// desiredSessionDuration is used only for an initial authorization: nil means
// "no explicit choice", falling back to the issuer's session_duration.
// authorizationExpiresAt is used only for rotation and is carried from the
// prior row. Exactly one is normally non-nil.
//
// toolSelection is the consent-screen tool policy JSON persisted verbatim on
// the new row (nil = all tools). The auth-code grant encodes it from the
// consent choice; the refresh grant carries the old row's value.
func (s *Service) mintSessionAndRespond(
	ctx context.Context,
	w http.ResponseWriter,
	endpoint *ResolvedMcpEndpoint,
	clientRow *usersessions_repo.UserSessionClient,
	subject urn.SessionSubject,
	desiredSessionDuration *time.Duration,
	authorizationExpiresAt *time.Time,
	toolSelection []byte,
	baseURL string,
	logger *slog.Logger,
) error {
	now := time.Now()
	if authorizationExpiresAt == nil {
		// Resolve the issuer's session_duration — the maximum absolute
		// authorization lifetime. Microseconds-only: the issuer create handler
		// stores via conv.PtrToPGInterval which never sets Months/Days; if we
		// ever see those here, raw SQL bypassed the writer and the conversion
		// is calendar-dependent — fail rather than silently approximate.
		issuer, err := usersessions_repo.New(s.db).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
			ID:        endpoint.UserSessionIssuerID,
			ProjectID: endpoint.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return oops.E(oops.CodeNotFound, err, "user_session_issuer not found")
			}
			return oops.E(oops.CodeUnexpected, err, "lookup user session issuer").LogError(ctx, logger)
		}
		if !issuer.SessionDuration.Valid {
			return oops.E(oops.CodeUnexpected, nil, "issuer session_duration is not set").LogError(ctx, logger)
		}
		if issuer.SessionDuration.Months != 0 || issuer.SessionDuration.Days != 0 {
			return oops.E(oops.CodeUnexpected, nil, "issuer session_duration carries Months/Days; only Microseconds intervals are supported").LogError(ctx, logger)
		}
		maxLifetime := time.Duration(issuer.SessionDuration.Microseconds) * time.Microsecond
		if maxLifetime <= 0 {
			return oops.E(oops.CodeUnexpected, nil, "issuer session_duration is non-positive").LogError(ctx, logger)
		}
		authorizationLifetime := maxLifetime
		if desiredSessionDuration != nil && *desiredSessionDuration > 0 {
			authorizationLifetime = min(*desiredSessionDuration, maxLifetime)
		}
		deadline := now.Add(authorizationLifetime)
		authorizationExpiresAt = &deadline
	}
	authorizationLifetime := authorizationExpiresAt.Sub(now)
	if authorizationLifetime <= 0 {
		return oops.E(oops.CodeUnauthorized, nil, "user authorization has expired").LogError(ctx, logger)
	}
	accessExpiresAt := now.Add(accessTokenLifetime)
	if authorizationExpiresAt.Before(accessExpiresAt) {
		accessExpiresAt = *authorizationExpiresAt
	}
	accessLifetime := accessExpiresAt.Sub(now)

	issuerURL, err := endpoint.RootURL(baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build issuer URL").LogError(ctx, logger)
	}
	access, jti, err := s.userSessionSigner.Mint(sessiontokens.MintParams{
		Subject:   subject,
		Audience:  endpoint.AudienceURN,
		Issuer:    issuerURL,
		Lifetime:  0,
		ExpiresAt: &accessExpiresAt,
		ClientID:  clientRow.ClientID,
		JTI:       "",
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "mint session jwt").LogError(ctx, logger)
	}

	refreshTokenRaw, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate refresh token").LogError(ctx, logger)
	}

	if _, err := usersessions_repo.New(s.db).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		UserSessionClientID: uuid.NullUUID{UUID: clientRow.ID, Valid: true},
		SubjectUrn:          subject,
		Jti:                 jti,
		RefreshTokenHash:    sha256Hex(refreshTokenRaw),
		ExpiresAt:           pgtype.Timestamptz{Time: accessExpiresAt, InfinityModifier: 0, Valid: true},
		RefreshExpiresAt:    pgtype.Timestamptz{Time: *authorizationExpiresAt, InfinityModifier: 0, Valid: true},
		ToolSelection:       toolSelection,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "persist user session").LogError(ctx, logger)
	}

	body, err := json.Marshal(tokenResponse{
		AccessToken:            access,
		TokenType:              "Bearer",
		ExpiresIn:              int64(accessLifetime.Seconds()),
		RefreshToken:           refreshTokenRaw,
		AuthorizationExpiresIn: int64(authorizationLifetime.Seconds()),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "marshal token response").LogError(ctx, logger)
	}

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
	var oauthErr *oauthwire.Error
	if errors.As(err, &oauthErr) {
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
