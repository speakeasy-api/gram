package oauth21

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/xaa"
)

// idJAGLifetime is how long a minted ID-JAG stays valid. The grant is
// redeemed immediately by the client that asked for it, so this is short by
// design -- 5 minutes matches the profile's own examples.
const idJAGLifetime = 5 * time.Minute

// idJAGResponse is the RFC 8693 token-exchange response carrying an ID-JAG.
// `TokenType` is always xaa.TokenTypeNotApplicable: an ID-JAG is an
// authorization grant to be redeemed elsewhere, not a credential to present
// at a resource.
type idJAGResponse struct {
	IssuedTokenType string `json:"issued_token_type"`
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	Scope           string `json:"scope,omitempty"`
	ExpiresIn       int    `json:"expires_in"`
}

// handleTokenExchangeGrant mints an ID-JAG: the middle leg of the cross-app
// access flow, where a client trades the identity assertion it holds from
// this IdP for a grant naming one specific resource authorization server.
//
// Everything this endpoint decides is policy the dev-idp's XAA tables
// describe -- which app is asking, whether that app is assigned to the
// resolved user for the target resource, and which of the requested scopes
// that assignment actually grants.
func (h *Handler) handleTokenExchangeGrant(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if got := r.Form.Get("requested_token_type"); got != xaa.TokenTypeIDJAG {
		oauthError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("only requested_token_type=%s is supported, got %q", xaa.TokenTypeIDJAG, got))
		return
	}

	audience := r.Form.Get("audience")
	if audience == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "audience is required and must be the resource authorization server's issuer identifier")
		return
	}

	queries := repo.New(h.db)

	// `audience` names a resource authorization server by its issuer URL;
	// RFC 8693 gives unknown targets their own error code rather than
	// folding them into invalid_request.
	slug, ok := xaa.ResourceSlugFromIssuer(h.cfg.ExternalURL, audience)
	if !ok {
		oauthError(w, http.StatusBadRequest, "invalid_target", "audience does not name a resource authorization server on this dev-idp")
		return
	}
	resource, err := queries.GetXaaResourceBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			oauthError(w, http.StatusBadRequest, "invalid_target", "no resource is registered for that audience")
			return
		}
		h.logger.ErrorContext(ctx, "load xaa resource for mint", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to load resource")
		return
	}

	// `resource` is optional, but when sent it must name the protected
	// resource behind the audience -- it is not a second way to choose one.
	if want := r.Form.Get("resource"); want != "" && want != resource.ResourceIdentifier {
		oauthError(w, http.StatusBadRequest, "invalid_target", "resource does not match the resource identifier registered for that audience")
		return
	}

	app := h.authenticateXaaApp(ctx, w, queries, r)
	if app == nil {
		return
	}

	userID, ok := h.resolveExchangeSubject(ctx, w, queries, r)
	if !ok {
		return
	}

	assignment, err := queries.GetXaaAppAssignmentForMint(ctx, repo.GetXaaAppAssignmentForMintParams{
		AppID:      app.ID,
		UserID:     userID,
		ResourceID: resource.ID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Real enterprise IdPs answer a policy denial here with
			// access_denied rather than one of the RFC 6749 §5.2 codes, and a
			// client's error handling should be exercised against what it
			// will actually meet in production.
			oauthError(w, http.StatusForbidden, "access_denied", "the user is not assigned this app for that resource")
			return
		}
		h.logger.ErrorContext(ctx, "load xaa assignment", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to evaluate policy")
		return
	}

	// An omitted `scope` means "whatever this assignment grants"; a supplied
	// one is narrowed to it. Asking for scopes and being granted none is a
	// denial, not an empty success.
	granted := assignment.GrantedScopes
	if requested := r.Form.Get("scope"); requested != "" {
		granted = xaa.NarrowScope(requested, assignment.GrantedScopes)
		if granted == "" {
			oauthError(w, http.StatusBadRequest, "invalid_scope", "none of the requested scopes are granted by this assignment")
			return
		}
	}

	now := time.Now()
	jti := uuid.New().String()
	user, err := queries.GetUser(ctx, userID)
	if err != nil {
		h.logger.ErrorContext(ctx, "look up user for id-jag", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to load user")
		return
	}

	claims := xaa.Claims{
		Email:    user.Email,
		Resource: resource.ResourceIdentifier,
		ClientID: app.ClientID,
		Scope:    granted,
		AuthTime: now.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Issuer:    h.issuer(),
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{xaa.ResourceASIssuer(h.cfg.ExternalURL, resource.Slug)},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(idJAGLifetime)),
			NotBefore: nil,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = h.keystore.KID()
	token.Header["typ"] = xaa.JWTType
	signed, err := token.SignedString(h.keystore.PrivateKey())
	if err != nil {
		h.logger.ErrorContext(ctx, "sign id-jag", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to sign the grant")
		return
	}

	if _, err := queries.CreateXaaIssuedJag(ctx, repo.CreateXaaIssuedJagParams{
		Jti:        jti,
		AppID:      app.ID,
		UserID:     userID,
		ResourceID: resource.ID,
		Scope:      granted,
		ExpiresAt:  now.Add(idJAGLifetime),
	}); err != nil {
		h.logger.ErrorContext(ctx, "record issued id-jag", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to record the grant")
		return
	}

	// RFC 8693 §2.2.1 requires no-store on a token-exchange response.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, idJAGResponse{
		IssuedTokenType: xaa.TokenTypeIDJAG,
		AccessToken:     signed,
		TokenType:       xaa.TokenTypeNotApplicable,
		Scope:           granted,
		ExpiresIn:       int(idJAGLifetime.Seconds()),
	})
}

// authenticateXaaApp resolves and authenticates the requesting app from the
// form's client credentials. It writes the error response itself and returns
// nil when the caller should stop.
func (h *Handler) authenticateXaaApp(ctx context.Context, w http.ResponseWriter, queries *repo.Queries, r *http.Request) *repo.XaaApp {
	clientID := r.Form.Get("client_id")
	if clientID == "" {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "client_id is required to mint an ID-JAG")
		return nil
	}

	app, err := queries.GetXaaAppByClientID(ctx, clientID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			oauthError(w, http.StatusUnauthorized, "invalid_client", "client_id is not a registered cross-app access app")
			return nil
		}
		h.logger.ErrorContext(ctx, "load xaa app", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to load client")
		return nil
	}

	if !app.Enabled {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "this app is disabled")
		return nil
	}

	// An app registered without a secret is a public client and authenticates
	// by client_id alone; one registered with a secret must present it.
	if app.ClientSecret != "" {
		presented := r.Form.Get("client_secret")
		if subtle.ConstantTimeCompare([]byte(presented), []byte(app.ClientSecret)) != 1 {
			oauthError(w, http.StatusUnauthorized, "invalid_client", "client_secret does not match")
			return nil
		}
	}

	return &app
}

// resolveExchangeSubject reads the subject_token / subject_token_type pair
// down to the local user it stands for. It writes the error response itself
// and reports false when the caller should stop.
func (h *Handler) resolveExchangeSubject(ctx context.Context, w http.ResponseWriter, queries *repo.Queries, r *http.Request) (uuid.UUID, bool) {
	subjectToken := r.Form.Get("subject_token")
	if subjectToken == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "subject_token is required")
		return uuid.Nil, false
	}

	switch r.Form.Get("subject_token_type") {
	case xaa.TokenTypeIDToken:
		userID, err := h.subjectFromIDToken(subjectToken)
		if err != nil {
			h.logger.WarnContext(ctx, "reject id_token subject", slog.Any("error", err))
			oauthError(w, http.StatusBadRequest, "invalid_grant", "subject_token is not a valid id_token from this issuer")
			return uuid.Nil, false
		}
		if _, err := queries.GetUser(ctx, userID); err != nil {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "subject_token names a user that no longer exists")
			return uuid.Nil, false
		}
		return userID, true

	case xaa.TokenTypeRefreshToken:
		stored, err := queries.GetActiveToken(ctx, repo.GetActiveTokenParams{Token: subjectToken, Ts: time.Now()})
		if err != nil || stored.Kind != "refresh_token" {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "subject_token is unknown, revoked, expired, or not a refresh token")
			return uuid.Nil, false
		}
		return stored.UserID, true

	case "":
		oauthError(w, http.StatusBadRequest, "invalid_request", "subject_token_type is required")
		return uuid.Nil, false

	default:
		oauthError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("subject_token_type must be %s or %s", xaa.TokenTypeIDToken, xaa.TokenTypeRefreshToken))
		return uuid.Nil, false
	}
}

// subjectFromIDToken verifies an id_token this dev-idp signed and returns the
// user it names.
//
// The token's `aud` is deliberately not checked against the app doing the
// exchange. In a real IdP that check stops one app from replaying an
// id_token minted for another; here it protects nothing, because anything
// that can reach this endpoint can also set the currentUser and have a fresh
// id_token minted for whichever client it likes. Requiring the match would
// only force every test to run a second login as the app under test.
func (h *Handler) subjectFromIDToken(raw string) (uuid.UUID, error) {
	var claims jwt.RegisteredClaims
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{h.keystore.SigningAlg()}),
		jwt.WithIssuer(h.issuer()),
		jwt.WithExpirationRequired(),
	)
	if _, err := parser.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) {
		return h.keystore.PublicKey(), nil
	}); err != nil {
		return uuid.Nil, fmt.Errorf("parse id_token: %w", err)
	}

	userID, err := uuid.Parse(strings.TrimSpace(claims.Subject))
	if err != nil {
		return uuid.Nil, fmt.Errorf("id_token subject %q is not a local user id: %w", claims.Subject, err)
	}
	return userID, nil
}
