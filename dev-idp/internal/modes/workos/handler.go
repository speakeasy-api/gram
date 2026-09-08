// Package workos serves the dev-idp's WorkOS REST surface at /workos.
//
// A single Backend flag decides where the data comes from. BackendLocal
// emulates the API against the dev-idp's own SQLite store; BackendWorkOS
// passes calls through to a real WorkOS environment. The mount point does
// not change with the backend, so WORKOS_API_URL is a constant.
//
// Two things are always served locally, whatever the backend:
//
//   - The authorization_code grant at POST /user_management/authenticate, the
//     second leg of non-interactive login. Real WorkOS only accepts codes from
//     its own AuthKit ceremony, so dev-idp consumes its local code. Other
//     grants, including Magic Auth, pass through to WorkOS.
//   - /_inspect/*, the dev-idp dashboard's read-only window. Namespaced so
//     it can never shadow a genuine WorkOS API path.
package workos

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/defaultuser"
	gramworkos "github.com/speakeasy-api/gram/dev-idp/internal/workos"
)

const (
	maxAuthenticateBodyBytes = 64 << 10

	// Mode is the identity slot key persisted on current_users rows holding a
	// real WorkOS subject. It does not select the runtime backend.
	Mode = "workos"

	// Prefix is the URL prefix the dev-idp listener mounts this handler
	// under. Compose with http.StripPrefix when wiring.
	Prefix = "/workos"
)

// Config carries the static configuration for the WorkOS surface.
type Config struct {
	// Backend selects emulation or passthrough.
	Backend Backend

	// ClientSecret authenticates Gram callers to the dev-idp WorkOS surface.
	// It is never forwarded to WorkOS.
	ClientSecret string

	// UpstreamURL is the real WorkOS API base URL that BackendWorkOS
	// proxies to. Ignored under BackendLocal.
	UpstreamURL string

	// APIKey authenticates dev-idp to upstream WorkOS. Ignored under
	// BackendLocal and never shared with downstream Gram callers.
	APIKey string
}

// Handler serves the dev-idp's WorkOS surface.
type Handler struct {
	cfg      Config
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *sql.DB
	client   *gramworkos.Client
	emulator http.Handler
	proxy    *httputil.ReverseProxy
}

// NewHandler builds the WorkOS surface. emulator serves the local
// implementation and is required. client is required under BackendWorkOS
// and unused under BackendLocal.
func NewHandler(cfg Config, emulator http.Handler, client *gramworkos.Client, logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) (*Handler, error) {
	if cfg.ClientSecret == "" || cfg.ClientSecret == "unset" {
		return nil, errors.New("dev-idp WorkOS client secret is required")
	}
	if cfg.Backend == BackendWorkOS && (cfg.APIKey == "" || cfg.APIKey == "unset") {
		return nil, errors.New("WorkOS API key is required for the workos backend")
	}

	h := &Handler{
		cfg:      cfg,
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/modes/workos"),
		logger:   logger.With(slog.String("component", "devidp.workos"), slog.String("backend", cfg.Backend.String())),
		db:       db,
		client:   client,
		emulator: emulator,
		proxy:    nil,
	}

	if cfg.Backend == BackendWorkOS {
		upstream, err := url.Parse(cfg.UpstreamURL)
		if err != nil {
			return nil, fmt.Errorf("parse WorkOS upstream URL %q: %w", cfg.UpstreamURL, err)
		}
		apiKey := cfg.APIKey
		h.proxy = &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstream)
			r.Out.Host = upstream.Host
			// The downstream credential authenticates to dev-idp only. Never
			// forward it across the upstream trust boundary.
			r.Out.Header.Set("Authorization", "Bearer "+apiKey)
		}}
	}

	return h, nil
}

// Handler returns the http.Handler that should be mounted under `Prefix`
// (use http.StripPrefix). All registered paths are relative to that prefix.
func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()

	// Dashboard inspection, namespaced away from the real API surface. These
	// read the live WorkOS API, so they only exist when there is one; under
	// BackendLocal they answer with an explanation rather than a bare 404,
	// which would read as a wrong URL.
	if h.cfg.Backend == BackendLocal {
		mux.HandleFunc("GET /_inspect/", handleInspectUnavailable)
		// The emulator already implements authenticate against local state.
		mux.Handle("/", h.emulator)
		return h.authenticateClients(mux)
	}

	mux.HandleFunc("GET /_inspect/currentUser", h.handleGetCurrentUser)
	mux.HandleFunc("GET /_inspect/users/{id_or_email}", h.handleGetUser)
	mux.HandleFunc("GET /_inspect/organizations/{id}", h.handleGetOrganization)

	mux.HandleFunc("POST /user_management/authenticate", h.routeAuthenticate)
	mux.HandleFunc("POST /sso/token", h.proxySSOToken)
	mux.Handle("/", h.proxy)
	return h.authenticateClients(mux)
}

func (h *Handler) authenticateClients(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.isPublicRoute(r) {
			next.ServeHTTP(w, r)
			return
		}

		presented, err := clientSecretFromRequest(r)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errRequestBodyTooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		if subtle.ConstantTimeCompare([]byte(presented), []byte(h.cfg.ClientSecret)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid dev-idp client secret"})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) isPublicRoute(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/_inspect/") {
		return true
	}
	if h.cfg.Backend != BackendLocal {
		return false
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/passwordless/sessions/") && strings.HasSuffix(r.URL.Path, "/authorize") {
		return true
	}
	return r.Method == http.MethodGet && r.URL.Path == "/portal"
}

func clientSecretFromRequest(r *http.Request) (string, error) {
	switch r.URL.Path {
	case "/user_management/authenticate":
		body, err := readAndRestoreBody(r)
		if err != nil {
			return "", err
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", errors.New("invalid request body")
		}
		rawClientSecret, ok := payload["client_secret"]
		if !ok {
			return "", nil
		}
		var clientSecret string
		if err := json.Unmarshal(rawClientSecret, &clientSecret); err != nil {
			return "", errors.New("invalid request body")
		}
		return clientSecret, nil
	case "/sso/token":
		body, err := readAndRestoreBody(r)
		if err != nil {
			return "", err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return "", errors.New("invalid form body")
		}
		return values.Get("client_secret"), nil
	default:
		secret, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		return secret, nil
	}
}

var errRequestBodyTooLarge = errors.New("request body too large")

func readAndRestoreBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAuthenticateBodyBytes+1))
	if err != nil {
		return nil, errors.New("read request body")
	}
	if len(body) > maxAuthenticateBodyBytes {
		return nil, errRequestBodyTooLarge
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return body, nil
}

func (h *Handler) routeAuthenticate(w http.ResponseWriter, r *http.Request) {
	body, err := readAndRestoreBody(r)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errRequestBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	var envelope struct {
		GrantType string `json:"grant_type"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.GrantType == "authorization_code" {
		h.handleAuthenticate(w, r)
		return
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	apiKey, err := json.Marshal(h.cfg.APIKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encode upstream credential"})
		return
	}
	for key := range payload {
		if strings.EqualFold(key, "client_secret") {
			delete(payload, key)
		}
	}
	payload["client_secret"] = apiKey
	body, err = json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "encode request body"})
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	h.proxy.ServeHTTP(w, r)
}

func (h *Handler) proxySSOToken(w http.ResponseWriter, r *http.Request) {
	body, err := readAndRestoreBody(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid form body"})
		return
	}
	values.Set("client_secret", h.cfg.APIKey)
	body = []byte(values.Encode())
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	h.proxy.ServeHTTP(w, r)
}

// handleInspectUnavailable answers the inspection routes under BackendLocal,
// where there is no live WorkOS to read. The dashboard reads local identity
// through the Goa management API instead.
func handleInspectUnavailable(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusConflict, map[string]string{
		"error": "WorkOS inspection requires GRAM_DEVIDP_BACKEND=workos; this dev-idp is running the local backend",
	})
}

// =============================================================================
// Non-interactive login against a real WorkOS backend
// =============================================================================

// handleAuthenticate completes the login started at /oauth2-1/authorize.
// It consumes the locally minted auth code, then reports the real WorkOS
// identity so the Gram server stores a genuine workos_id and can sync
// memberships from the live API.
func (h *Handler) handleAuthenticate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var body struct {
		ClientID     string `json:"client_id"`
		Code         string `json:"code"`
		ClientSecret string `json:"client_secret"`
		GrantType    string `json:"grant_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Code == "" || body.GrantType != "authorization_code" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code and grant_type=authorization_code required"})
		return
	}

	queries := repo.New(h.db)
	if _, err := queries.ConsumeAuthCodeForClient(ctx, repo.ConsumeAuthCodeForClientParams{
		Code:     body.Code,
		ClientID: body.ClientID,
		Ts:       time.Now(),
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "auth code is unknown, consumed, or expired"})
		return
	}

	row, err := queries.GetCurrentUser(ctx, Mode)
	if errors.Is(err, sql.ErrNoRows) {
		row, err = h.bootstrapWorkosCurrentUser(ctx)
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "resolve workos currentUser for login", slog.Any("error", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "resolve currentUser: " + err.Error()})
		return
	}

	user, err := h.client.GetUser(ctx, row.SubjectRef)
	if err != nil {
		h.respondWorkosError(ctx, w, "fetch currentUser from WorkOS", err)
		return
	}
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "currentUser not found in WorkOS"})
		return
	}

	// organization_id drives SyncMembershipsFromWorkOS on the Gram side.
	var orgID string
	if members, merr := h.client.ListUserMemberships(ctx, user.ID); merr != nil {
		h.logger.WarnContext(ctx, "list workos memberships for login", slog.Any("error", merr))
	} else if len(members) > 0 {
		orgID = members[0].OrganizationID
	}

	resp := map[string]any{
		"user": map[string]any{
			"id":                  user.ID,
			"first_name":          user.FirstName,
			"last_name":           user.LastName,
			"email":               user.Email,
			"email_verified":      true,
			"profile_picture_url": user.ProfilePictureURL,
			"external_id":         "",
		},
		"access_token":  unsignedSessionJWT("devidp_session_" + uuid.NewString()),
		"refresh_token": uuid.NewString(),
	}
	if orgID != "" {
		resp["organization_id"] = orgID
	}
	writeJSON(w, http.StatusOK, resp)
}

// unsignedSessionJWT builds a minimal unsigned JWT carrying a "sid" claim.
// The Gram server parses the claim out of the access token to record a
// session id; it never verifies this signature.
func unsignedSessionJWT(sessionID string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]string{"sid": sessionID})
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

// =============================================================================
// Direct WorkOS inspection (used by the dashboard)
// =============================================================================

type userJSON struct {
	WorkosSub         string `json:"workos_sub"`
	Email             string `json:"email"`
	FirstName         string `json:"first_name"`
	LastName          string `json:"last_name"`
	ProfilePictureURL string `json:"profile_picture_url"`
}

type orgJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type currentUserJSON struct {
	WorkosSub         string  `json:"workos_sub"`
	Email             *string `json:"email,omitempty"`
	FirstName         *string `json:"first_name,omitempty"`
	LastName          *string `json:"last_name,omitempty"`
	ProfilePictureURL *string `json:"profile_picture_url,omitempty"`
	ShadowID          *string `json:"shadow_id,omitempty"`
	ShadowAdmin       bool    `json:"shadow_admin"`
}

func (h *Handler) handleGetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idOrEmail := r.PathValue("id_or_email")

	user, err := h.lookupWorkosUser(ctx, idOrEmail)
	if err != nil {
		h.respondWorkosError(ctx, w, "lookup user", err)
		return
	}
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found in WorkOS"})
		return
	}
	writeJSON(w, http.StatusOK, userJSON{
		WorkosSub:         user.ID,
		Email:             user.Email,
		FirstName:         user.FirstName,
		LastName:          user.LastName,
		ProfilePictureURL: user.ProfilePictureURL,
	})
}

func (h *Handler) handleGetOrganization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orgID := r.PathValue("id")

	org, err := h.client.GetOrganization(ctx, orgID)
	if err != nil {
		h.respondWorkosError(ctx, w, "get organization", err)
		return
	}
	writeJSON(w, http.StatusOK, orgJSON{
		ID:        org.ID,
		Name:      org.Name,
		CreatedAt: org.CreatedAt,
		UpdatedAt: org.UpdatedAt,
	})
}

func (h *Handler) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	row, err := repo.New(h.db).GetCurrentUser(ctx, Mode)
	if errors.Is(err, sql.ErrNoRows) {
		bootstrapped, berr := h.bootstrapWorkosCurrentUser(ctx)
		if berr != nil {
			h.logger.WarnContext(ctx, "bootstrap default workos currentUser", slog.Any("error", berr))
			writeJSON(w, http.StatusNotFound, map[string]string{"error": berr.Error()})
			return
		}
		row = bootstrapped
	} else if err != nil {
		h.logger.ErrorContext(ctx, "read currentUser", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read currentUser"})
		return
	}

	resp := currentUserJSON{
		WorkosSub:         row.SubjectRef,
		Email:             nil,
		FirstName:         nil,
		LastName:          nil,
		ProfilePictureURL: nil,
	}
	user, err := h.client.GetUser(ctx, row.SubjectRef)
	switch {
	case err != nil && gramworkos.IsNotFound(err):
		h.logger.WarnContext(ctx, "currentUser not found in WorkOS", slog.Any("error", err))
	case err != nil:
		h.logger.ErrorContext(ctx, "fetch live workos user", slog.Any("error", err))
	case user != nil:
		resp.Email = strPtr(user.Email)
		resp.FirstName = strPtr(user.FirstName)
		resp.LastName = strPtr(user.LastName)
		resp.ProfilePictureURL = strPtr(user.ProfilePictureURL)

		q := repo.New(h.db)
		shadows, serr := q.ListUsersFiltered(ctx, repo.ListUsersFilteredParams{
			After:          uuid.Nil,
			Email:          user.Email,
			OrganizationID: nil,
			MaxRows:        1,
		})
		var shadow *repo.User
		if serr == nil && len(shadows) > 0 && shadows[0].ID != uuid.Nil {
			shadow = &shadows[0]
		} else {
			displayName := strings.TrimSpace(user.FirstName + " " + user.LastName)
			if displayName == "" {
				displayName = user.Email
			}
			created, cerr := q.UpsertUserByEmail(ctx, repo.UpsertUserByEmailParams{
				ID:          uuid.New(),
				Email:       user.Email,
				DisplayName: displayName,
			})
			if cerr != nil {
				h.logger.WarnContext(ctx, "create shadow for currentUser", slog.Any("error", cerr))
			} else if created.ID != uuid.Nil {
				shadow = &created
			}
		}
		if shadow != nil {
			sid := shadow.ID.String()
			resp.ShadowID = &sid
			resp.ShadowAdmin = shadow.Admin
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// =============================================================================
// Identity resolution
// =============================================================================

func (h *Handler) bootstrapWorkosCurrentUser(ctx context.Context) (repo.CurrentUser, error) {
	committer, err := defaultuser.GitCommitter(ctx)
	if err != nil {
		return repo.CurrentUser{}, err
	}
	user, err := h.client.GetUserByEmail(ctx, committer.Email)
	if err != nil {
		return repo.CurrentUser{}, err
	}
	if user == nil {
		return repo.CurrentUser{}, errors.New("no WorkOS user found for " + committer.Email)
	}
	row, err := repo.New(h.db).UpsertCurrentUser(ctx, repo.UpsertCurrentUserParams{
		Mode:       Mode,
		SubjectRef: user.ID,
		Ts:         time.Now(),
	})
	if err != nil {
		return repo.CurrentUser{}, err
	}
	return row, nil
}

// =============================================================================
// Helpers
// =============================================================================

func (h *Handler) lookupWorkosUser(ctx context.Context, idOrEmail string) (*gramworkos.User, error) {
	if strings.Contains(idOrEmail, "@") {
		return h.client.GetUserByEmail(ctx, idOrEmail)
	}
	return h.client.GetUser(ctx, idOrEmail)
}

func (h *Handler) respondWorkosError(ctx context.Context, w http.ResponseWriter, op string, err error) {
	if gramworkos.IsNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found in WorkOS"})
		return
	}
	h.logger.ErrorContext(ctx, op, slog.Any("error", err))
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": op + ": " + err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
