package adminmcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

const staffChallengePrefix = "adminMCPChallenge:"
const staffBrowserProofCookie = "__Host-admin-mcp-proof"
const staffCodeLifetime = 10 * time.Minute
const staffAuthorizationLifetime = 24 * time.Hour

type staffChallenge struct {
	ID            string    `json:"id"`
	ClientID      string    `json:"client_id"`
	RedirectURI   string    `json:"redirect_uri"`
	State         string    `json:"state"`
	CodeChallenge string    `json:"code_challenge"`
	CSRFToken     string    `json:"csrf_token"`
	BrowserProof  string    `json:"browser_proof"`
	SessionHash   string    `json:"session_hash"`
	ResourceURI   string    `json:"resource_uri"`
	CreatedAt     time.Time `json:"created_at"`
}

func (c staffChallenge) CacheKey() string              { return staffChallengePrefix + c.ID }
func (c staffChallenge) AdditionalCacheKeys() []string { return []string{} }
func (c staffChallenge) TTL() time.Duration            { return staffCodeLifetime }

var _ cache.CacheableObject[staffChallenge] = (*staffChallenge)(nil)

type staffAuthorizationStore interface {
	Authorize(context.Context, staffAuthorization) error
}

type staffAuthorization struct {
	Subject       string
	ClientID      string
	SessionEnc    string
	ResourceURI   string
	Scopes        []string
	CodeHash      string
	CodeChallenge string
	RedirectURI   string
	ExpiresAt     time.Time
	GrantExpires  time.Time
}

// StaffOAuthAuthorization runs the browser-only, staff-verified consent flow.
// Its handlers must be mounted only behind the tailnet ingress.
type StaffOAuthAuthorization struct {
	clients  staffClientStore
	store    staffAuthorizationStore
	cache    cache.TypedCacheObject[staffChallenge]
	verifier adminSessionVerifier
	cipher   *encryption.Client
	resource string
}

func NewStaffOAuthAuthorization(clients staffClientStore, store staffAuthorizationStore, challengeCache cache.Cache, verifier adminSessionVerifier, cipher *encryption.Client, resource string) *StaffOAuthAuthorization {
	return &StaffOAuthAuthorization{
		clients: clients, store: store, cache: cache.NewTypedObjectCache[staffChallenge](nil, challengeCache, cache.SuffixNone),
		verifier: verifier, cipher: cipher, resource: resource,
	}
}

func (s *StaffOAuthAuthorization) AuthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !s.ready() {
			staffOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "authorization unavailable")
			return
		}
		request := usersessions.AuthorizationRequestFromQuery(r.URL.Query())
		if err := request.ValidateRedirectableFields(); err != nil {
			staffRequestError(w, err)
			return
		}
		if err := oauthwire.ValidateRedirectURI(request.RedirectURI); err != nil {
			staffRequestError(w, err)
			return
		}
		client, err := s.clients.GetClient(r.Context(), request.ClientID)
		if err != nil || !slices.Contains(client.RedirectURIs, request.RedirectURI) {
			staffOAuthError(w, http.StatusUnauthorized, "invalid_client", "unknown client_id or redirect_uri")
			return
		}
		if err := request.ValidatePostRedirect(); err != nil {
			staffRedirectError(w, r, request.RedirectURI, request.State, err)
			return
		}
		if err := oauthwire.ValidateResourceIndicators(request.Resources, s.resource); err != nil {
			staffRedirectError(w, r, request.RedirectURI, request.State, err)
			return
		}
		if !validStaffPKCEChallenge(request.CodeChallenge) {
			staffRedirectError(w, r, request.RedirectURI, request.State, &oauthwire.Error{Code: "invalid_request", Description: "invalid PKCE S256 challenge"})
			return
		}
		csrf, err := staffOpaqueToken()
		if err != nil {
			staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not start authorization")
			return
		}
		proof, err := staffOpaqueToken()
		if err != nil {
			staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not start authorization")
			return
		}
		challenge := staffChallenge{ID: uuid.NewString(), ClientID: client.ID, RedirectURI: request.RedirectURI, State: request.State, CodeChallenge: request.CodeChallenge, CSRFToken: csrf, BrowserProof: staffTokenHash(proof), SessionHash: "", ResourceURI: s.resource, CreatedAt: time.Now()}
		if err := s.cache.Store(r.Context(), challenge); err != nil {
			staffOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "could not start authorization")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: staffBrowserProofCookie + "-" + challenge.ID, Value: proof, Path: "/", MaxAge: int(staffCodeLifetime.Seconds()), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:exhaustruct // Host-only session proof; Domain and Expires intentionally omitted.
		http.Redirect(w, r, Path+"/connect?state="+url.QueryEscape(challenge.ID), http.StatusFound)
	})
}

func (s *StaffOAuthAuthorization) ConnectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		if !s.ready() {
			staffOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "authorization unavailable")
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.connectGet(w, r)
		case http.MethodPost:
			s.connectPost(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func (s *StaffOAuthAuthorization) connectGet(w http.ResponseWriter, r *http.Request) {
	challenge, err := s.cache.Get(r.Context(), staffChallengePrefix+r.URL.Query().Get("state"))
	if err != nil || challenge.ID == "" || time.Since(challenge.CreatedAt) >= staffCodeLifetime || challenge.ResourceURI != s.resource {
		staffOAuthError(w, http.StatusUnauthorized, "invalid_request", "authorization state is invalid or expired")
		return
	}
	if !staffBrowserProofMatches(r, challenge) {
		staffOAuthError(w, http.StatusUnauthorized, "invalid_request", "authorization browser is invalid")
		return
	}
	client, err := s.clients.GetClient(r.Context(), challenge.ClientID)
	if err != nil || !slices.Contains(client.RedirectURIs, challenge.RedirectURI) {
		staffOAuthError(w, http.StatusUnauthorized, "invalid_client", "authorization client is unavailable")
		return
	}
	_, sessionID, err := s.staffSession(r)
	if err != nil {
		var authErr *oops.ShareableError
		if !errors.Is(err, errMissingStaffCookie) && (!errors.As(err, &authErr) || authErr.Code != oops.CodeUnauthorized) {
			staffOAuthError(w, http.StatusUnauthorized, "access_denied", "staff login is required")
			return
		}
		// A logged-out session can leave a path-scoped browser cookie behind.
		// Clear it before returning to normal staff login, without touching /admin.
		if !errors.Is(err, errMissingStaffCookie) {
			http.SetCookie(w, &http.Cookie{Name: constants.AdminSessionCookie, Path: Path, MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:exhaustruct // Only the scoped cookie is expired.
		}
		// The relative return target is server-owned and contains only the opaque
		// challenge ID, never an untrusted redirect URI or client-supplied URL.
		returnTo := Path + "/connect?state=" + url.QueryEscape(challenge.ID)
		http.Redirect(w, r, "/admin/auth.login?return_to="+url.QueryEscape(returnTo), http.StatusFound)
		return
	}
	challenge.SessionHash = staffTokenHash(sessionID)
	if err := s.cache.Store(r.Context(), challenge); err != nil {
		staffOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "could not prepare authorization")
		return
	}
	var page strings.Builder
	if err := staffConsentPage.Execute(&page, struct{ ClientName, RedirectURI, State, CSRF string }{ClientName: client.Name, RedirectURI: challenge.RedirectURI, State: challenge.ID, CSRF: challenge.CSRFToken}); err != nil {
		staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not render authorization page")
		return
	}
	// Chrome also checks form-action on the 303 after consent. Only this
	// registered callback may receive the resulting browser navigation.
	callback, err := url.Parse(challenge.RedirectURI)
	if err != nil {
		staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not render authorization page")
		return
	}
	callbackSource := callback.Scheme + ":"
	if callback.Host != "" {
		callbackSource = callback.Scheme + "://" + callback.Host
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' "+callbackSource+"; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page.String()))
}

var staffConsentPage = template.Must(template.New("staff-consent").Parse(`<!doctype html><html><head><title>Connect Staff Admin MCP</title></head><body><h1>Connect Staff Admin MCP</h1><p>{{.ClientName}} requests access to staff administration. The callback is {{.RedirectURI}}.</p><form method="post" action="/admin-mcp/connect"><input type="hidden" name="state" value="{{.State}}"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><button type="submit" name="action" value="approve">Approve</button><button type="submit" name="action" value="deny">Deny</button></form></body></html>`))

func (s *StaffOAuthAuthorization) connectPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		staffOAuthError(w, http.StatusBadRequest, "invalid_request", "could not parse authorization")
		return
	}
	challenge, err := s.cache.GetAndDelete(r.Context(), staffChallengePrefix+r.PostForm.Get("state"))
	if err != nil || challenge.ID == "" || time.Since(challenge.CreatedAt) >= staffCodeLifetime || challenge.ResourceURI != s.resource {
		staffOAuthError(w, http.StatusUnauthorized, "invalid_request", "authorization state is invalid or expired")
		return
	}
	if !staffBrowserProofMatches(r, challenge) {
		staffOAuthError(w, http.StatusUnauthorized, "invalid_request", "authorization browser is invalid")
		return
	}
	if challenge.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(challenge.CSRFToken)) != 1 {
		staffOAuthError(w, http.StatusUnauthorized, "invalid_request", "authorization confirmation is invalid")
		return
	}
	client, err := s.clients.GetClient(r.Context(), challenge.ClientID)
	if err != nil || !slices.Contains(client.RedirectURIs, challenge.RedirectURI) {
		staffOAuthError(w, http.StatusUnauthorized, "invalid_client", "authorization client is unavailable")
		return
	}
	staff, sessionID, err := s.staffSession(r)
	if err != nil {
		staffOAuthError(w, http.StatusUnauthorized, "access_denied", "staff login is required")
		return
	}
	if challenge.SessionHash == "" || subtle.ConstantTimeCompare([]byte(challenge.SessionHash), []byte(staffTokenHash(sessionID))) != 1 {
		staffOAuthError(w, http.StatusUnauthorized, "access_denied", "staff session changed during authorization")
		return
	}
	if r.PostForm.Get("action") != "approve" {
		staffRedirectError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "access_denied", Description: "authorization denied"})
		return
	}
	code, err := staffOpaqueToken()
	if err != nil {
		staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not complete authorization")
		return
	}
	encryptedSession, err := s.cipher.Encrypt([]byte(sessionID))
	if err != nil {
		staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not complete authorization")
		return
	}
	now := time.Now()
	input := staffAuthorization{
		Subject: urn.NewUserSubject(staff.OIDCSubject).String(), ClientID: challenge.ClientID,
		SessionEnc: encryptedSession, ResourceURI: s.resource, Scopes: []string{"admin:read"},
		CodeHash: staffTokenHash(code), CodeChallenge: challenge.CodeChallenge, RedirectURI: challenge.RedirectURI,
		ExpiresAt: now.Add(staffAuthorizationLifetime), GrantExpires: now.Add(staffCodeLifetime),
	}
	if err := s.store.Authorize(r.Context(), input); err != nil {
		if errors.Is(err, errStaffGrant) {
			staffOAuthError(w, http.StatusUnauthorized, "invalid_client", "authorization client is unavailable")
		} else {
			staffOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "could not complete authorization")
		}
		return
	}
	callback, err := url.Parse(challenge.RedirectURI)
	if err != nil {
		staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not complete authorization")
		return
	}
	query := callback.Query()
	query.Set("code", code)
	if challenge.State != "" {
		query.Set("state", challenge.State)
	}
	callback.RawQuery = query.Encode()
	http.Redirect(w, r, callback.String(), http.StatusSeeOther)
}

func staffBrowserProofMatches(r *http.Request, challenge staffChallenge) bool {
	cookie, err := r.Cookie(staffBrowserProofCookie + "-" + challenge.ID)
	return err == nil && cookie.Value != "" && challenge.BrowserProof != "" &&
		subtle.ConstantTimeCompare([]byte(staffTokenHash(cookie.Value)), []byte(challenge.BrowserProof)) == 1
}

var errMissingStaffCookie = errors.New("missing admin session cookie")

func (s *StaffOAuthAuthorization) staffSession(r *http.Request) (*contextvalues.AdminAuthContext, string, error) {
	cookie, err := r.Cookie(constants.AdminSessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, "", errMissingStaffCookie
	}
	ctx, err := s.verifier.Authorize(r.Context(), cookie.Value, &security.APIKeyScheme{Name: constants.AdminAuthSecurityScheme}) //nolint:exhaustruct // Only name is used.
	if err != nil {
		return nil, "", fmt.Errorf("verify staff browser session: %w", err)
	}
	staff, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || staff == nil || staff.SessionID != cookie.Value || staff.OIDCSubject == "" || staff.Email == "" {
		return nil, "", errors.New("staff session identity mismatch")
	}
	return staff, cookie.Value, nil
}

func (s *StaffOAuthAuthorization) ready() bool {
	return s != nil && s.clients != nil && s.store != nil && s.verifier != nil && s.cipher != nil && s.resource != ""
}

func staffRequestError(w http.ResponseWriter, err error) {
	if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
		staffOAuthError(w, http.StatusBadRequest, oauthErr.Code, oauthErr.Description)
	} else {
		staffOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid request")
	}
}

func staffRedirectError(w http.ResponseWriter, r *http.Request, redirectURI, state string, err error) {
	var oauthErr *oauthwire.Error
	if !errors.As(err, &oauthErr) {
		oauthErr = &oauthwire.Error{Code: "invalid_request", Description: "invalid request"}
	}
	callback, parseErr := url.Parse(redirectURI)
	if parseErr != nil {
		staffOAuthError(w, http.StatusBadRequest, oauthErr.Code, oauthErr.Description)
		return
	}
	query := callback.Query()
	query.Set("error", oauthErr.Code)
	query.Set("error_description", oauthErr.Description)
	if state != "" {
		query.Set("state", state)
	}
	callback.RawQuery = query.Encode()
	http.Redirect(w, r, callback.String(), http.StatusSeeOther)
}

func validStaffPKCEChallenge(challenge string) bool {
	if len(challenge) != 43 {
		return false
	}
	for _, c := range challenge {
		if c < 'A' || c > 'Z' {
			if c < 'a' || c > 'z' {
				if c < '0' || c > '9' {
					if c != '-' && c != '_' {
						return false
					}
				}
			}
		}
	}
	return true
}
