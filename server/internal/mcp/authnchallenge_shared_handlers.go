// HTTP handlers of the shared authorization servers. Each resolves the issuer
// from the route, then either serves an issuer-level operation directly or
// resolves the MCP server the request is for and hands it to the same Serve*
// entry point a per-endpoint authorization server uses.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// errSharedRefreshUnavailable means the refresh resource cannot yet be
// resolved safely: a rotation may have committed without publishing its replay.
var errSharedRefreshUnavailable = errors.New("refresh token rotation outcome is unavailable")

// sharedAuthorizationServerRoute is one route a shared authorization server
// serves.
type sharedAuthorizationServerRoute struct {
	// method is the HTTP method.
	method string

	// path is the chi route pattern, carrying the issuer id parameter.
	path string

	// handler serves the route.
	handler func(http.ResponseWriter, *http.Request) error
}

// sharedAuthorizationServerRoutes is every route a shared authorization server
// serves. RFC 8414 metadata is served at both the path-insertion location
// §3.1 defines and the path-appended location some clients request instead.
// The IdP and remote login callbacks are deployment-global and stay where they
// are.
func (s *Service) sharedAuthorizationServerRoutes() []sharedAuthorizationServerRoute {
	return []sharedAuthorizationServerRoute{
		{method: http.MethodGet, path: wellknown.OAuthAuthorizationServerPath + sharedAuthorizationServerPattern, handler: s.HandleSharedAuthorizationServerMetadata},
		{method: http.MethodGet, path: sharedAuthorizationServerPattern + wellknown.OAuthAuthorizationServerPath, handler: s.HandleSharedAuthorizationServerMetadata},
		{method: http.MethodPost, path: sharedAuthorizationServerPattern + "/register", handler: s.HandleSharedRegister},
		{method: http.MethodGet, path: sharedAuthorizationServerPattern + "/authorize", handler: s.HandleSharedAuthorize},
		{method: http.MethodGet, path: sharedAuthorizationServerPattern + "/connect", handler: s.HandleSharedConsent},
		{method: http.MethodPost, path: sharedAuthorizationServerPattern + "/connect", handler: s.HandleSharedConsent},
		{method: http.MethodPost, path: sharedAuthorizationServerPattern + "/connect/remote-session", handler: s.HandleSharedConsentAction},
		{method: http.MethodPost, path: sharedAuthorizationServerPattern + "/connect/mcp", handler: s.HandleSharedConsentMCP},
		{method: http.MethodDelete, path: sharedAuthorizationServerPattern + "/connect/mcp", handler: s.HandleSharedConsentMCP},
		{method: http.MethodPost, path: sharedAuthorizationServerPattern + "/token", handler: s.HandleSharedToken},
		{method: http.MethodPost, path: sharedAuthorizationServerPattern + "/revoke", handler: s.HandleSharedRevoke},
	}
}

// attachSharedAuthorizationServers mounts the shared authorization server
// routes on mux.
func attachSharedAuthorizationServers(mux goahttp.Muxer, service *Service) {
	for _, route := range service.sharedAuthorizationServerRoutes() {
		o11y.AttachHandler(mux, route.method, route.path, oops.ErrHandle(service.logger, route.handler).ServeHTTP)
	}
}

// HandleSharedAuthorizationServerMetadata serves a shared authorization
// server's RFC 8414 metadata.
//
// It advertises the authorization-code and refresh grants only.
// TODO(AIM-402): advertise the ID-JAG and workload grants once the shared
// token endpoint accepts them.
func (s *Service) HandleSharedAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	issuer, err := s.sharedIssuerForRequest(r)
	if err != nil {
		return err
	}
	urls, err := issuer.authorizationServer.urls()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build OAuth server URLs").LogError(ctx, issuer.logger)
	}
	grantTypes := []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken}
	return writeJSONMetadata(ctx, w, r, issuer.logger, s.authorizationServerMetadata(ctx, urls, issuer.cimdAdmissionModeRaw, grantTypes, nil))
}

// HandleSharedRegister serves RFC 7591 dynamic client registration on a
// shared authorization server. Clients belong to the issuer, so a client
// registered here, or at any of the issuer's per-endpoint authorization
// servers, works for all of its MCP servers.
func (s *Service) HandleSharedRegister(w http.ResponseWriter, r *http.Request) error {
	issuer, err := s.sharedIssuerForRequest(r)
	if err != nil {
		return err
	}
	return s.serveRegister(w, r, issuer.logger, issuer.authorizationServer.issuerID)
}

// HandleSharedRevoke serves RFC 7009 token revocation on a shared
// authorization server.
func (s *Service) HandleSharedRevoke(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	issuer, err := s.sharedIssuerForRequest(r)
	if err != nil {
		return err
	}
	urls, err := issuer.authorizationServer.urls()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build OAuth server URLs").LogError(ctx, issuer.logger)
	}
	return s.serveRevoke(w, r, issuer.logger, issuer.authorizationServer.issuerID, urls)
}

// HandleSharedAuthorize serves the authorization endpoint of a shared
// authorization server. The client must name the MCP server it wants with
// exactly one RFC 8707 resource indicator; the rest of the request is the
// per-endpoint authorization request, served for that server.
func (s *Service) HandleSharedAuthorize(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	issuer, err := s.sharedIssuerForRequest(r)
	if err != nil {
		return err
	}
	resources := r.URL.Query()[oauthwire.ParamResource]
	endpoint, rejection, err := s.resolveSharedResourceIndicators(ctx, issuer.logger, issuer.authorizationServer, resources)
	if err != nil {
		return err
	}
	if rejection != sharedResourceAccepted {
		s.recordSharedResourceRejection(ctx, issuer.logger, issuer.authorizationServer.issuerID, mcpmetrics.OAuthFlowStageAuthorize, rejection, resources)
		return s.rejectSharedAuthorizeResource(w, r, issuer, rejection)
	}
	return s.ServeAuthorize(w, r, endpoint)
}

// rejectSharedAuthorizeResource answers an authorization request whose
// resource indicator was refused with invalid_target (RFC 8707 §2). It is
// redirected to the client only when the client and its redirect_uri check out
// against the issuer's registrations, the same condition the per-endpoint
// authorization endpoint redirects under; otherwise it is rendered inline.
func (s *Service) rejectSharedAuthorizeResource(w http.ResponseWriter, r *http.Request, issuer *sharedIssuer, rejection sharedResourceRejection) error {
	ctx := r.Context()
	logger := issuer.logger
	description := sharedResourceDescription(rejection)

	req := usersessions.AuthorizationRequestFromQuery(r.URL.Query())
	req.SetDefaults()
	if err := req.ValidateRedirectableFields(); err != nil {
		return writeAuthorizeOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}
	// A client_id this issuer has not registered, including a CIMD client on
	// first contact, has no redirect_uri that can be trusted yet.
	client, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: issuer.authorizationServer.issuerID,
		ClientID:            req.ClientID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidTarget, description)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
	}
	if !redirectURIMatches(&client, req.RedirectURI) {
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidTarget, description)
	}
	return redirectAuthorizeOAuthError(ctx, w, r, logger, issuer.authorizationServer.issuer, req.RedirectURI, req.State, string(rejection),
		&oauthwire.Error{Code: oautherr.CodeInvalidTarget, Description: description})
}

// sharedResourceDescription is the error_description for a refused resource
// indicator. Every reason that concerns which MCP server the resource names
// shares one description, so a client cannot learn which servers exist or
// which issuer they belong to.
func sharedResourceDescription(rejection sharedResourceRejection) string {
	switch rejection {
	case sharedResourceMissing:
		return "resource is required: name the MCP server this authorization is for"
	case sharedResourceMultiple:
		return "exactly one resource is allowed"
	case sharedResourceMismatch:
		return "resource does not match the MCP server this grant is for"
	case sharedResourceAccepted, sharedResourceMalformed, sharedResourceUnknownHost, sharedResourceNotFound, sharedResourceForeignIssuer:
		return "resource does not identify an MCP server of this authorization server"
	}
	return "resource does not identify an MCP server of this authorization server"
}

// HandleSharedConsent serves the consent page of a shared authorization
// server, for the MCP server its challenge was minted for.
func (s *Service) HandleSharedConsent(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	issuer, err := s.sharedIssuerForRequest(r)
	if err != nil {
		return err
	}
	stateID := r.URL.Query().Get("state")
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, consentFormMaxBytes)
		if err := r.ParseForm(); err != nil {
			return oops.E(oops.CodeBadRequest, err, "failed to parse form").LogError(ctx, issuer.logger)
		}
		stateID = r.PostForm.Get("state")
	}
	endpoint, err := s.sharedChallengeEndpoint(ctx, issuer, stateID)
	if err != nil {
		return err
	}
	return s.ServeConsent(w, r, endpoint)
}

// HandleSharedConsentAction serves the consent page's remote session card
// actions on a shared authorization server.
func (s *Service) HandleSharedConsentAction(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	issuer, err := s.sharedIssuerForRequest(r)
	if err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, consentActionFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse form").LogError(ctx, issuer.logger)
	}
	endpoint, err := s.sharedChallengeEndpoint(ctx, issuer, r.PostForm.Get("state"))
	if err != nil {
		return err
	}
	return s.ServeConsentAction(w, r, endpoint)
}

// HandleSharedConsentMCP serves the consent page's tool inventory requests on
// a shared authorization server.
func (s *Service) HandleSharedConsentMCP(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	issuer, err := s.sharedIssuerForRequest(r)
	if err != nil {
		return err
	}
	endpoint, err := s.sharedChallengeEndpoint(ctx, issuer, r.Header.Get(consentStateHeader))
	if err != nil {
		return err
	}
	return s.ServeConsentMCP(w, r, endpoint)
}

// sharedChallengeEndpoint resolves the MCP server an in-flight challenge on a
// shared authorization server was minted for. The consent handlers re-read and
// fully validate the challenge themselves; this only finds the endpoint they
// validate it against, and refuses a challenge another authorization server
// minted.
func (s *Service) sharedChallengeEndpoint(ctx context.Context, issuer *sharedIssuer, stateID string) (*ResolvedMcpEndpoint, error) {
	if stateID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, issuer.logger)
	}
	challenge, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogError(ctx, issuer.logger)
	}
	if challenge.UserSessionIssuerID != issuer.authorizationServer.issuerID || !challenge.Endpoint.SharedAuthorizationServer {
		return nil, oops.E(oops.CodeUnauthorized, nil, "authn challenge state does not match this authorization server").LogError(ctx, issuer.logger)
	}
	endpoint, err := s.loadResolvedMcpEndpointByRef(ctx, challenge.Endpoint)
	if errors.Is(err, errToolsetEndpointMismatch) {
		// The MCP endpoint was re-pointed at another backend mid-flow.
		return nil, oauthAuthorityError(err).LogWarn(ctx, issuer.logger)
	}
	return endpoint, err
}

// HandleSharedToken serves the token endpoint of a shared authorization
// server. It finds the MCP server the grant is for from the grant itself, the
// authorization code's challenge or the refresh token's session, so a token
// request may omit `resource`; when it names one, it must be that server.
// The grant is then served exactly as the server's per-endpoint token endpoint
// would serve it, which checks any `resource` after authenticating the
// client, and mints a session bound to the server.
//
// TODO(AIM-402): accept the ID-JAG and workload grants.
func (s *Service) HandleSharedToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	issuer, err := s.sharedIssuerForRequest(r)
	if err != nil {
		return err
	}
	logger := issuer.logger

	r.Body = http.MaxBytesReader(w, r.Body, tokenFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "failed to parse form")
	}

	var endpoint *ResolvedMcpEndpoint
	grantType := r.PostForm.Get(oauthwire.ParamGrantType)
	switch grantType {
	case oauthwire.GrantTypeAuthorizationCode:
		endpoint, err = s.sharedAuthorizationCodeEndpoint(ctx, w, logger, issuer.authorizationServer.issuerID, r.PostForm.Get(oauthwire.ParamCode))
	case oauthwire.GrantTypeRefreshToken:
		endpoint, err = s.sharedRefreshTokenEndpoint(ctx, w, logger, issuer.authorizationServer, r.PostForm.Get(oauthwire.ParamRefreshToken))
	default:
		creds := extractClientCredentials(r)
		clientID, _ := resolvePresentedClientID(creds)
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth token request rejected", clientID, creds.method, grantType, oautherr.CodeUnsupportedGrantType)
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeUnsupportedGrantType, "unsupported grant_type")
	}
	if endpoint == nil {
		return err
	}

	return s.ServeToken(w, r, endpoint)
}

// sharedAuthorizationCodeEndpoint resolves the MCP server an authorization
// code was minted for, from the challenge reference the code carries. It
// returns nil once it has written a refusal, with the result of writing it.
// The code is only read here; the grant handler consumes it.
func (s *Service) sharedAuthorizationCodeEndpoint(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, issuerID uuid.UUID, code string) (*ResolvedMcpEndpoint, error) {
	if code == "" {
		return nil, writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "code is required")
	}
	grant, err := s.userSessionGrantCache.Get(ctx, userSessionGrantCacheKey(issuerID, code, strings.HasPrefix(code, agentAuthorizationCodePrefix)))
	if err != nil || grant.Endpoint == nil || !grant.Endpoint.SharedAuthorizationServer {
		return nil, writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidGrant, "code not found or expired")
	}
	endpoint, err := s.loadResolvedMcpEndpointByRef(ctx, *grant.Endpoint)
	if err != nil {
		// Gone, or re-pointed at another backend mid-flow: either way the code
		// no longer names a server it can be redeemed for.
		var shareable *oops.ShareableError
		if errors.Is(err, errToolsetEndpointMismatch) || (errors.As(err, &shareable) && shareable.Code == oops.CodeNotFound) {
			return nil, writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidGrant, "code not found or expired")
		}
		return nil, err
	}
	return endpoint, nil
}

// sharedRefreshTokenEndpoint resolves the MCP server a refresh token's session
// is bound to, from the resource recorded on the session. It returns nil once
// it has written a refusal, with the result of writing it. The refresh token
// is only read here; the grant handler rotates it.
func (s *Service) sharedRefreshTokenEndpoint(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, authorizationServer *sharedAuthorizationServer, refreshToken string) (*ResolvedMcpEndpoint, error) {
	if refreshToken == "" {
		return nil, writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "refresh_token is required")
	}
	resource, err := s.sharedRefreshTokenResource(ctx, authorizationServer.issuerID, sha256Hex(refreshToken))
	if errors.Is(err, errSharedRefreshUnavailable) {
		logger.WarnContext(ctx, "shared refresh resource lookup unavailable", attr.SlogError(err))
		return nil, writeTokenError(ctx, w, logger, http.StatusServiceUnavailable, oautherr.CodeTemporarilyUnavailable, "refresh token rotation outcome is unavailable; retry")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load refresh token resource").LogError(ctx, logger)
	}
	if resource == "" {
		// No session, or one a per-endpoint authorization server minted: it is
		// refreshed there, never here.
		return nil, writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidGrant, "refresh_token is unknown or already used")
	}
	endpoint, rejection, err := s.resolveSharedResource(ctx, logger, authorizationServer, resource)
	if err != nil {
		return nil, err
	}
	if rejection != sharedResourceAccepted {
		s.recordSharedResourceRejection(ctx, logger, authorizationServer.issuerID, mcpmetrics.OAuthFlowStageToken, rejection, []string{resource})
		return nil, writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidGrant, "the MCP server this refresh_token is for is no longer available")
	}
	return endpoint, nil
}

// sharedRefreshTokenResource is the resource the session of a refresh token
// is bound to, or "" when there is none. A live session answers from its row.
// A refresh token rotated moments ago has no live row, and answers from its
// rotation's replay entry instead, so a concurrent refresh still reaches the
// replay the grant handler serves it.
func (s *Service) sharedRefreshTokenResource(ctx context.Context, issuerID uuid.UUID, refreshTokenHash string) (string, error) {
	session, err := usersessions_repo.New(s.db).GetUserSessionByRefreshTokenHash(ctx, usersessions_repo.GetUserSessionByRefreshTokenHashParams{
		UserSessionIssuerID: issuerID,
		RefreshTokenHash:    refreshTokenHash,
	})
	switch {
	case err == nil:
		return session.Resource.String, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return "", fmt.Errorf("load refresh token session: %w", err)
	}

	replayKey := refreshTokenReplayKey(issuerID, refreshTokenHash)
	replay, err := s.userSessionRefreshReplayCache.Get(ctx, replayKey)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		// A rotation deletes the old row before publishing its replay. Only
		// an owned lease plus a second cache miss proves no winner is still
		// publishing; otherwise the caller must retry, not discard its token.
		leases, ok := s.userSessionRefreshReplayCoordination.(cache.LeaseCache)
		if !ok {
			return "", errSharedRefreshUnavailable
		}
		owner, ownerErr := generateOpaqueToken()
		if ownerErr != nil {
			return "", fmt.Errorf("generate shared refresh lookup lease owner: %w", ownerErr)
		}
		lockKey := "lock:" + replayKey
		acquired, leaseErr := leases.AcquireLease(ctx, lockKey, owner, refreshTokenReplayGracePeriod)
		if leaseErr != nil {
			return "", fmt.Errorf("%w: acquire lookup lease: %w", errSharedRefreshUnavailable, leaseErr)
		}
		if acquired {
			defer s.releaseRefreshTokenReplayLock(ctx, lockKey, owner, s.logger)
		}
		// The winner may have published between the first miss and the lease
		// check. Adopt its resource even if it has not released the lease yet.
		replay, err = s.userSessionRefreshReplayCache.Get(ctx, replayKey)
		if errors.Is(err, redisCache.ErrCacheMiss) {
			if acquired {
				return "", nil
			}
			return "", errSharedRefreshUnavailable
		}
	}
	if err != nil {
		return "", fmt.Errorf("%w: read replay: %w", errSharedRefreshUnavailable, err)
	}
	payload, err := s.decodeRefreshTokenReplay(replay, replayKey)
	if err != nil {
		return "", fmt.Errorf("decode refresh token replay: %w", err)
	}
	return payload.Resource, nil
}
