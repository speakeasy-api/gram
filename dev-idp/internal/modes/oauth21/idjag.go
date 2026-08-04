package oauth21

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/ema"
	"github.com/speakeasy-api/gram/dev-idp/internal/jwks"
)

// idJAGLifetime is how long a minted ID-JAG stays valid. The grant is
// redeemed immediately by the client that asked for it, so this is short by
// design -- 5 minutes matches the profile's own examples.
const idJAGLifetime = 5 * time.Minute

// clientAssertionSkew is how much clock drift a private_key_jwt assertion may
// carry. Assertions are minted seconds before use, so this only has to cover
// ordinary drift between the app and this IdP.
const clientAssertionSkew = 60 * time.Second

// idJAGResponse is the RFC 8693 token-exchange response carrying an ID-JAG.
// `TokenType` is always ema.TokenTypeNotApplicable: an ID-JAG is an
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
// Everything this endpoint decides is policy the dev-idp's EMA tables
// describe -- which app is asking, whether that app is assigned to the
// resolved user for the target resource, and which of the requested scopes
// that assignment actually grants.
func (h *Handler) handleTokenExchangeGrant(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if got := r.Form.Get("requested_token_type"); got != ema.TokenTypeIDJAG {
		oauthError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("only requested_token_type=%s is supported, got %q", ema.TokenTypeIDJAG, got))
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
	slug, ok := ema.ResourceSlugFromIssuer(h.cfg.ExternalURL, audience)
	if !ok {
		oauthError(w, http.StatusBadRequest, "invalid_target", "audience does not name a resource authorization server on this dev-idp")
		return
	}
	resource, err := queries.GetEmaResourceBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			oauthError(w, http.StatusBadRequest, "invalid_target", "no resource is registered for that audience")
			return
		}
		h.logger.ErrorContext(ctx, "load ema resource for mint", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to load resource")
		return
	}

	// `resource` is optional, but when sent it must name the protected
	// resource behind the audience -- it is not a second way to choose one.
	if want := r.Form.Get("resource"); want != "" && want != resource.ResourceIdentifier {
		oauthError(w, http.StatusBadRequest, "invalid_target", "resource does not match the resource identifier registered for that audience")
		return
	}

	app := h.authenticateEmaApp(ctx, w, queries, r)
	if app == nil {
		return
	}

	userID, ok := h.resolveExchangeSubject(ctx, w, queries, r)
	if !ok {
		return
	}

	assignment, err := queries.GetEmaAppAssignmentForMint(ctx, repo.GetEmaAppAssignmentForMintParams{
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
		h.logger.ErrorContext(ctx, "load ema assignment", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to evaluate policy")
		return
	}

	// An omitted `scope` means "whatever this assignment grants"; a supplied
	// one is narrowed to it. Asking for scopes and being granted none is a
	// denial, not an empty success.
	granted := assignment.GrantedScopes
	if requested := r.Form.Get("scope"); requested != "" {
		granted = ema.NarrowScope(requested, assignment.GrantedScopes)
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

	claims := ema.Claims{
		Email:    user.Email,
		Resource: resource.ResourceIdentifier,
		ClientID: app.ClientID,
		Scope:    granted,
		AuthTime: now.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Issuer:    h.issuer(),
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{ema.ResourceASIssuer(h.cfg.ExternalURL, resource.Slug)},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(idJAGLifetime)),
			NotBefore: nil,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = h.keystore.KID()
	token.Header["typ"] = ema.JWTType
	signed, err := token.SignedString(h.keystore.PrivateKey())
	if err != nil {
		h.logger.ErrorContext(ctx, "sign id-jag", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to sign the grant")
		return
	}

	if _, err := queries.CreateEmaIssuedJag(ctx, repo.CreateEmaIssuedJagParams{
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
		IssuedTokenType: ema.TokenTypeIDJAG,
		AccessToken:     signed,
		TokenType:       ema.TokenTypeNotApplicable,
		Scope:           granted,
		ExpiresIn:       int(idJAGLifetime.Seconds()),
	})
}

// authenticateEmaApp resolves and authenticates the requesting app. It writes
// the error response itself and returns nil when the caller should stop.
//
// Which method applies follows from how the app was registered rather than
// from anything the caller asserts, so a client cannot pick the weakest one
// on offer: an app with a JWKS must present a private_key_jwt assertion even
// if it also has a secret.
func (h *Handler) authenticateEmaApp(ctx context.Context, w http.ResponseWriter, queries *repo.Queries, r *http.Request) *repo.EmaApp {
	// With private_key_jwt the client_id form parameter is optional (RFC 7523
	// §2.2 puts the identity in the assertion's iss/sub), so fall back to the
	// assertion before deciding the request is unidentified.
	clientID := r.Form.Get("client_id")
	assertion := r.Form.Get("client_assertion")
	if clientID == "" && assertion != "" {
		clientID = unverifiedAssertionSubject(assertion)
	}
	if clientID == "" {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "client_id is required to mint an ID-JAG")
		return nil
	}

	app, err := queries.GetEmaAppByClientID(ctx, clientID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			oauthError(w, http.StatusUnauthorized, "invalid_client", "client_id is not a registered enterprise-managed authorization app")
			return nil
		}
		h.logger.ErrorContext(ctx, "load ema app", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to load client")
		return nil
	}

	if !app.Enabled {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "this app is disabled")
		return nil
	}

	switch {
	case app.Jwks != "":
		if got := r.Form.Get("client_assertion_type"); got != ema.ClientAssertionTypeJWTBearer {
			oauthError(w, http.StatusUnauthorized, "invalid_client",
				fmt.Sprintf("this app authenticates with private_key_jwt; send client_assertion_type=%s", ema.ClientAssertionTypeJWTBearer))
			return nil
		}
		if err := h.verifyClientAssertion(app, assertion); err != nil {
			h.logger.WarnContext(ctx, "reject client assertion",
				slog.String("client_id", app.ClientID),
				slog.Any("error", err),
			)
			oauthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
			return nil
		}

	case app.ClientSecret != "":
		presented := r.Form.Get("client_secret")
		if subtle.ConstantTimeCompare([]byte(presented), []byte(app.ClientSecret)) != 1 {
			oauthError(w, http.StatusUnauthorized, "invalid_client", "client_secret does not match")
			return nil
		}

	default:
		// A public client: registered with neither credential, so client_id
		// alone identifies it. Safe here only because the ID-JAG it can obtain
		// is still gated on an assignment for the resolved user.
	}

	return &app
}

// unverifiedAssertionSubject reads `sub` out of a client assertion without
// checking its signature, purely to know which app's key to check it with.
// Nothing is trusted on the strength of this -- verifyClientAssertion
// re-reads every claim from the verified token.
func unverifiedAssertionSubject(assertion string) string {
	var claims jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(assertion, &claims); err != nil {
		return ""
	}
	return claims.Subject
}

// verifyClientAssertion checks an RFC 7523 §2.2 private_key_jwt assertion
// against the app's registered JWKS.
//
// The audience check is what stops an assertion from being replayed: a token
// the app minted for some other authorization server must not authenticate it
// here. Both the issuer identifier and the token endpoint URL are accepted,
// because deployments differ on which one they put in `aud` and rejecting the
// other reads as a signing bug rather than a configuration one.
func (h *Handler) verifyClientAssertion(app repo.EmaApp, assertion string) error {
	if assertion == "" {
		return errors.New("client_assertion is required")
	}

	doc, err := jwks.Parse([]byte(app.Jwks))
	if err != nil {
		return fmt.Errorf("this app's registered jwks is unusable: %w", err)
	}

	var probe jwt.RegisteredClaims
	unverified, _, err := jwt.NewParser().ParseUnverified(assertion, &probe)
	if err != nil {
		return fmt.Errorf("client_assertion is not a JWT: %w", err)
	}
	kid, _ := unverified.Header["kid"].(string)

	key, err := doc.FindRSA(kid)
	if err != nil {
		return fmt.Errorf("no key in this app's jwks matches the assertion: %w", err)
	}

	var claims jwt.RegisteredClaims
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{h.keystore.SigningAlg()}),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(clientAssertionSkew),
	)
	if _, err := parser.ParseWithClaims(assertion, &claims, func(*jwt.Token) (any, error) { return key, nil }); err != nil {
		return fmt.Errorf("client_assertion did not verify: %w", err)
	}

	if claims.Issuer != app.ClientID || claims.Subject != app.ClientID {
		return fmt.Errorf("client_assertion iss and sub must both be %q", app.ClientID)
	}

	iss := h.issuer()
	if !slices.Contains(claims.Audience, iss) && !slices.Contains(claims.Audience, iss+"/token") {
		return fmt.Errorf("client_assertion aud %v names neither this issuer (%s) nor its token endpoint", claims.Audience, iss)
	}

	return nil
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
	case ema.TokenTypeIDToken:
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

	case ema.TokenTypeRefreshToken:
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
			fmt.Sprintf("subject_token_type must be %s or %s", ema.TokenTypeIDToken, ema.TokenTypeRefreshToken))
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
