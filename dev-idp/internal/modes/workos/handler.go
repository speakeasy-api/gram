// Package workos implements the dev-idp's workos mode — emulates the
// Speakeasy IDP shape (the same /v1/speakeasy_provider/* surface served
// by /local-speakeasy/) but resolves user and organization data from the
// live WorkOS REST API instead of the dev-idp's local Postgres tables.
//
// Architecture:
//
//   - The Speakeasy provider exchange itself is local: auth_codes and
//     tokens are persisted to the dev-idp DB with mode='workos' so /login
//     → /exchange → /validate work without round-tripping WorkOS for the
//     code/token lifecycle.
//   - User identity comes from real WorkOS. The currentUser for this mode
//     is a WorkOS sub (e.g. `user_01H...`); /validate fetches the live
//     WorkOS user + memberships at request time.
//   - To keep the auth_codes/tokens FK to users(id) workable, every
//     workos-resolved identity is shadowed into the local users table
//     (find-or-create by email). The local row is just a stable UUID;
//     the source of truth for the user's profile is always WorkOS.
//
// Three additional GET endpoints (`/users/{id_or_email}`,
// `/organizations/{id}`, `/currentUser`) live on this mode for direct
// dashboard inspection of WorkOS state.
//
// The mode is unmounted entirely when WORKOS_API_KEY is unset
// (idp-design.md §8). When mounted, every request hits the live WorkOS
// API (or whatever WORKOS_HOST points at — useful for sandbox /
// fixtures) using the configured API key.
package workos

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/defaultuser"
	gramworkos "github.com/speakeasy-api/gram/dev-idp/internal/workos"
)

const (
	// Mode is the discriminator persisted on every auth_codes / tokens /
	// current_users row owned by this handler.
	Mode = "workos"

	// Prefix is the URL prefix the dev-idp listener mounts the workos
	// handler under. Compose with http.StripPrefix when wiring.
	Prefix = "/workos"

	// clientIDSentinel is recorded on auth_codes / tokens so the dashboard
	// can show which mode minted the row. The Speakeasy exchange has no
	// real client concept.
	clientIDSentinel = "workos"

	authCodeLifetime = 5 * time.Minute
	tokenLifetime    = 24 * time.Hour
)

// Config carries the static configuration for the workos mode. Mirrors
// localspeakeasy.Config so dev-idp.go can wire both handlers from the
// same SPEAKEASY_SECRET_KEY env var.
type Config struct {
	SecretKey string
}

// Handler serves the workos mode's HTTP routes.
type Handler struct {
	cfg    Config
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
	client *gramworkos.Client
}

func NewHandler(cfg Config, client *gramworkos.Client, logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *Handler {
	return &Handler{
		cfg:    cfg,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/modes/workos"),
		logger: logger.With(slog.String("component", "devidp."+Mode)),
		db:     db,
		client: client,
	}
}

// Handler returns the http.Handler that should be mounted under `Prefix`
// (use http.StripPrefix). All registered paths are relative to that
// prefix.
func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	// Speakeasy provider exchange — same shape as local-speakeasy, but
	// identity backed by WorkOS instead of local Postgres.
	mux.HandleFunc("GET /v1/speakeasy_provider/login", h.handleLogin)
	mux.HandleFunc("POST /v1/speakeasy_provider/exchange", h.withSecretKey(h.handleExchange))
	mux.HandleFunc("GET /v1/speakeasy_provider/validate", h.withSecretKey(h.handleValidate))
	mux.HandleFunc("POST /v1/speakeasy_provider/revoke", h.withSecretKey(h.handleRevoke))
	mux.HandleFunc("POST /v1/speakeasy_provider/register", h.withSecretKey(h.handleRegister))
	// Direct WorkOS inspection — used by the dashboard.
	mux.HandleFunc("GET /users/{id_or_email}", h.handleGetUser)
	mux.HandleFunc("GET /organizations/{id}", h.handleGetOrganization)
	mux.HandleFunc("GET /currentUser", h.handleGetCurrentUser)
	return mux
}

// =============================================================================
// JSON wire types — mirror local-speakeasy exactly so Gram-side decoders
// don't care which mode answered.
// =============================================================================

type speakeasyUserJSON struct {
	ID           string  `json:"id"`
	Email        string  `json:"email"`
	DisplayName  string  `json:"display_name"`
	PhotoURL     *string `json:"photo_url"`
	GithubHandle *string `json:"github_handle"`
	Admin        bool    `json:"admin"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	Whitelisted  bool    `json:"whitelisted"`
}

type speakeasyOrgJSON struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Slug               string   `json:"slug"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
	AccountType        string   `json:"account_type"`
	SSOConnectionID    *string  `json:"sso_connection_id"`
	WorkOSID           *string  `json:"workos_id,omitempty"`
	UserWorkspaceSlugs []string `json:"user_workspaces_slugs"` // typo preserved — wire compat with Gram-side
}

type speakeasyValidateResponse struct {
	User          speakeasyUserJSON  `json:"user"`
	Organizations []speakeasyOrgJSON `json:"organizations"`
}

// =============================================================================
// Middleware
// =============================================================================

func (h *Handler) withSecretKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("speakeasy-auth-provider-key")
		if key != h.cfg.SecretKey {
			h.logger.WarnContext(r.Context(), "rejected: invalid provider key", slog.String("http.route", r.URL.Path))
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "Unauthorized: invalid or missing provider key",
			})
			return
		}
		next(w, r)
	}
}

// =============================================================================
// Speakeasy provider — backed by real WorkOS
// =============================================================================

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	returnURL := r.URL.Query().Get("return_url")
	state := r.URL.Query().Get("state")

	h.logger.InfoContext(ctx, "auth flow initiated",
		slog.String("event", "devidp.mode.used"),
		slog.String("http.route", r.URL.Path),
	)

	if returnURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing return_url parameter"})
		return
	}
	target, err := url.Parse(returnURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid return_url"})
		return
	}

	shadowID, err := h.resolveShadowUserID(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	code, err := h.issueAuthCode(ctx, shadowID, returnURL)
	if err != nil {
		h.logger.ErrorContext(ctx, "issue auth code", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue auth code"})
		return
	}

	q := target.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (h *Handler) handleExchange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	if body.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing code in request body"})
		return
	}

	queries := repo.New(h.db)
	codeRow, err := queries.ConsumeAuthCode(ctx, repo.ConsumeAuthCodeParams{Code: body.Code, Mode: Mode})
	if err != nil {
		// Tolerance: tests sometimes /exchange without /login. Fall back to
		// the configured currentUser shadow.
		uid, ferr := h.resolveShadowUserID(ctx)
		if ferr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or expired code"})
			return
		}
		token, terr := h.issueIDToken(ctx, uid)
		if terr != nil {
			h.logger.ErrorContext(ctx, "issue id_token (fallback)", slog.Any("error", terr))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id_token": token})
		return
	}

	token, err := h.issueIDToken(ctx, codeRow.UserID)
	if err != nil {
		h.logger.ErrorContext(ctx, "issue id_token", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id_token": token})
}

func (h *Handler) handleValidate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idToken := r.Header.Get("speakeasy-auth-provider-id-token")
	if idToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing token header"})
		return
	}

	queries := repo.New(h.db)
	tokenRow, err := queries.GetActiveToken(ctx, repo.GetActiveTokenParams{Token: idToken, Mode: Mode})
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
		return
	}

	resp, err := h.validateResponseFor(ctx, queries, tokenRow.UserID)
	if err != nil {
		h.logger.ErrorContext(ctx, "build validate response", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load user from WorkOS"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idToken := r.Header.Get("speakeasy-auth-provider-id-token")
	if idToken != "" {
		if err := repo.New(h.db).RevokeToken(ctx, repo.RevokeTokenParams{
			Ts:    sql.NullTime{Time: time.Now(), Valid: true},
			Token: idToken,
			Mode:  Mode,
		}); err != nil {
			h.logger.WarnContext(ctx, "revoke token", slog.Any("error", err))
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRegister mirrors the local-speakeasy register flow — creates a
// new organization for the authenticated user. The org is created
// LOCALLY (in the dev-idp's organizations table) rather than in WorkOS
// because the workos-go wrapper doesn't currently expose an org-create
// surface and round-tripping WorkOS for org creation is rarely what local
// dev wants. The shadow user gets a local membership; subsequent
// /validate calls union the WorkOS-sourced orgs with these locally-
// created ones.
func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idToken := r.Header.Get("speakeasy-auth-provider-id-token")
	if idToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing token header"})
		return
	}

	var body struct {
		OrganizationName string `json:"organization_name"`
		AccountType      string `json:"account_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	if body.OrganizationName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing organization_name"})
		return
	}

	queries := repo.New(h.db)
	tokenRow, err := queries.GetActiveToken(ctx, repo.GetActiveTokenParams{Token: idToken, Mode: Mode})
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
		return
	}

	accountType := body.AccountType

	org, err := queries.CreateOrganization(ctx, repo.CreateOrganizationParams{
		ID:          uuid.New(),
		Name:        body.OrganizationName,
		Slug:        slugify(body.OrganizationName),
		AccountType: sql.NullString{String: accountType, Valid: accountType != ""},
		WorkosID:    sql.NullString{String: "", Valid: false},
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "create organization", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create organization"})
		return
	}
	if _, err := queries.CreateMembership(ctx, repo.CreateMembershipParams{
		ID:             uuid.New(),
		UserID:         tokenRow.UserID,
		OrganizationID: org.ID,
		Role:           sql.NullString{String: "member", Valid: true},
	}); err != nil {
		h.logger.ErrorContext(ctx, "create membership", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to attach membership"})
		return
	}

	resp, err := h.validateResponseFor(ctx, queries, tokenRow.UserID)
	if err != nil {
		h.logger.ErrorContext(ctx, "build register response", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load user"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
	// ShadowID is the local users.id of the shadow row that backs this workos
	// identity. Present whenever the shadow has been created (first login or
	// first /currentUser fetch). The dashboard uses it to call users.update
	// for admin/whitelisted toggles.
	ShadowID    *string `json:"shadow_id,omitempty"`
	ShadowAdmin bool    `json:"shadow_admin"`
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

		// Resolve the local shadow user for admin toggling. Look up by
		// email first to avoid the SQLite ON CONFLICT RETURNING nil-UUID
		// bug. Only call UpsertUserByEmail when no row exists yet (fresh
		// insert path has no conflict, so RETURNING is safe).
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
// Identity resolution (workos sub → live WorkOS user → local shadow)
// =============================================================================

// resolveShadowUserID walks the workos-mode currentUser through to a
// local users.id. The currentUser stores a WorkOS sub; we fetch the live
// WorkOS user and find-or-create a local users row by email so the
// auth_codes/tokens FKs have something to point at. The local row is
// just a stable handle — the source of truth for the user's profile is
// always WorkOS, and /validate re-fetches it on every request.
//
// When no current_users row exists yet, the same git-committer →
// WorkOS-lookup bootstrap that powers /currentUser fires here too so
// the IDP is usable from the first request.
func (h *Handler) resolveShadowUserID(ctx context.Context) (uuid.UUID, error) {
	queries := repo.New(h.db)
	row, err := queries.GetCurrentUser(ctx, Mode)
	if errors.Is(err, sql.ErrNoRows) {
		bootstrapped, berr := h.bootstrapWorkosCurrentUser(ctx)
		if berr != nil {
			return uuid.Nil, fmt.Errorf("bootstrap default currentUser: %w", berr)
		}
		row = bootstrapped
	} else if err != nil {
		return uuid.Nil, fmt.Errorf("read currentUser: %w", err)
	}

	user, err := h.client.GetUser(ctx, row.SubjectRef)
	if err != nil {
		return uuid.Nil, fmt.Errorf("fetch workos user %s: %w", row.SubjectRef, err)
	}
	if user == nil {
		return uuid.Nil, fmt.Errorf("workos user %s not found", row.SubjectRef)
	}

	shadow, err := queries.UpsertUserByEmail(ctx, repo.UpsertUserByEmailParams{
		Email:       user.Email,
		DisplayName: strings.TrimSpace(user.FirstName + " " + user.LastName),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert local shadow for workos user: %w", err)
	}
	return shadow.ID, nil
}

// bootstrapWorkosCurrentUser is the workos-mode bootstrap path
// (idp-design.md §3): resolve the local git committer's email, look it
// up in WorkOS, and persist the resulting WorkOS sub as
// current_users[mode='workos']. Errors carry "the default user relies on
// committer email" so the operator knows what to fix.
func (h *Handler) bootstrapWorkosCurrentUser(ctx context.Context) (repo.CurrentUser, error) {
	committer, err := defaultuser.GitCommitter(ctx)
	if err != nil {
		return repo.CurrentUser{}, fmt.Errorf("resolve git committer: %w", err)
	}
	user, err := h.client.GetUserByEmail(ctx, committer.Email)
	if err != nil {
		return repo.CurrentUser{}, fmt.Errorf("the default user relies on committer email — WorkOS lookup failed for %s: %w", committer.Email, err)
	}
	if user == nil {
		return repo.CurrentUser{}, fmt.Errorf("the default user relies on committer email — no WorkOS user found for %s", committer.Email)
	}
	row, err := repo.New(h.db).UpsertCurrentUser(ctx, repo.UpsertCurrentUserParams{
		Mode:       Mode,
		SubjectRef: user.ID,
	})
	if err != nil {
		return repo.CurrentUser{}, fmt.Errorf("persist default workos currentUser: %w", err)
	}
	return row, nil
}

// =============================================================================
// Speakeasy validate response — built from live WorkOS data
// =============================================================================

func (h *Handler) validateResponseFor(ctx context.Context, queries *repo.Queries, shadowID uuid.UUID) (speakeasyValidateResponse, error) {
	shadow, err := queries.GetUser(ctx, shadowID)
	if err != nil {
		return speakeasyValidateResponse{}, fmt.Errorf("look up shadow user: %w", err)
	}

	currentUser, err := queries.GetCurrentUser(ctx, Mode)
	if err != nil {
		return speakeasyValidateResponse{}, fmt.Errorf("look up current workos sub: %w", err)
	}
	wosUser, err := h.client.GetUser(ctx, currentUser.SubjectRef)
	if err != nil {
		return speakeasyValidateResponse{}, fmt.Errorf("fetch workos user %s: %w", currentUser.SubjectRef, err)
	}
	if wosUser == nil {
		return speakeasyValidateResponse{}, fmt.Errorf("workos user %s not found", currentUser.SubjectRef)
	}

	displayName := strings.TrimSpace(wosUser.FirstName + " " + wosUser.LastName)
	if displayName == "" {
		displayName = wosUser.Email
	}

	out := speakeasyValidateResponse{
		User: speakeasyUserJSON{
			ID:           shadow.ID.String(),
			Email:        wosUser.Email,
			DisplayName:  displayName,
			PhotoURL:     strPtr(wosUser.ProfilePictureURL),
			GithubHandle: nil,
			Admin:        shadow.Admin,
			CreatedAt:    shadow.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:    shadow.UpdatedAt.UTC().Format(time.RFC3339),
			Whitelisted:  shadow.Whitelisted,
		},
		Organizations: []speakeasyOrgJSON{},
	}

	// Live WorkOS orgs the user is a member of.
	memberships, err := h.client.ListUserMemberships(ctx, currentUser.SubjectRef)
	if err != nil {
		return speakeasyValidateResponse{}, fmt.Errorf("list workos memberships for %s: %w", currentUser.SubjectRef, err)
	}
	for _, m := range memberships {
		out.Organizations = append(out.Organizations, speakeasyOrgJSON{
			ID:                 m.OrganizationID,
			Name:               m.Organization,
			Slug:               slugify(m.Organization),
			CreatedAt:          m.CreatedAt,
			UpdatedAt:          m.UpdatedAt,
			AccountType:        "",
			SSOConnectionID:    nil,
			WorkOSID:           strPtr(m.OrganizationID),
			UserWorkspaceSlugs: []string{slugify(m.Organization)},
		})
	}

	// Locally-created orgs (from /register on this mode). Unioned in so
	// the Speakeasy IDP shape behaves consistently across modes.
	localOrgs, err := queries.ListOrganizationsForUser(ctx, shadowID)
	if err != nil {
		return speakeasyValidateResponse{}, fmt.Errorf("list local orgs for shadow %s: %w", shadowID, err)
	}
	for _, o := range localOrgs {
		out.Organizations = append(out.Organizations, speakeasyOrgJSON{
			ID:                 o.ID.String(),
			Name:               o.Name,
			Slug:               o.Slug,
			CreatedAt:          o.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:          o.UpdatedAt.UTC().Format(time.RFC3339),
			AccountType:        o.AccountType,
			SSOConnectionID:    nil,
			WorkOSID:           pgTextPtr(o.WorkosID),
			UserWorkspaceSlugs: []string{o.Slug},
		})
	}

	return out, nil
}

// =============================================================================
// Token issuance (writes to the dev-idp's local auth_codes/tokens tables)
// =============================================================================

func (h *Handler) issueAuthCode(ctx context.Context, userID uuid.UUID, returnURL string) (string, error) {
	code := randomToken()
	_, err := repo.New(h.db).CreateAuthCode(ctx, repo.CreateAuthCodeParams{
		Code:                code,
		Mode:                Mode,
		UserID:              userID,
		ClientID:            clientIDSentinel,
		RedirectUri:         returnURL,
		CodeChallenge:       sql.NullString{String: "", Valid: false},
		CodeChallengeMethod: sql.NullString{String: "", Valid: false},
		Scope:               sql.NullString{String: "", Valid: false},
		ExpiresAt:           time.Now().Add(authCodeLifetime),
	})
	if err != nil {
		return "", fmt.Errorf("insert auth_code: %w", err)
	}
	return code, nil
}

func (h *Handler) issueIDToken(ctx context.Context, userID uuid.UUID) (string, error) {
	token := randomToken()
	_, err := repo.New(h.db).CreateToken(ctx, repo.CreateTokenParams{
		Token:     token,
		Mode:      Mode,
		UserID:    userID,
		ClientID:  clientIDSentinel,
		Kind:      "id_token",
		Scope:     sql.NullString{String: "", Valid: false},
		ExpiresAt: time.Now().Add(tokenLifetime),
	})
	if err != nil {
		return "", fmt.Errorf("insert id_token: %w", err)
	}
	return token, nil
}

// =============================================================================
// Helpers
// =============================================================================

func (h *Handler) lookupWorkosUser(ctx context.Context, idOrEmail string) (*gramworkos.User, error) {
	if strings.Contains(idOrEmail, "@") {
		user, err := h.client.GetUserByEmail(ctx, idOrEmail)
		if err != nil {
			return nil, fmt.Errorf("get user by email: %w", err)
		}
		return user, nil
	}
	user, err := h.client.GetUser(ctx, idOrEmail)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return user, nil
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

func randomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func pgTextPtr(t sql.NullString) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}

var (
	nonAlphanumeric     = regexp.MustCompile(`[^a-z0-9]+`)
	leadingTrailingDash = regexp.MustCompile(`^-+|-+$`)
)

func slugify(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = leadingTrailingDash.ReplaceAllString(s, "")
	return s
}
