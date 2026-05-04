// Package localspeakeasy implements the dev-idp's local-speakeasy mode — a
// drop-in port of the standalone local-speakeasy-idp binary's
// /v1/speakeasy_provider/* surface (idp-design.md §7.1) onto the dev-idp's
// shared Postgres store and per-mode currentUser.
//
// Wire-shape compatibility is preserved byte-for-byte (including the
// `user_workspaces_slugs` JSON-tag typo) so existing Gram-side callers
// don't need to change when they switch SPEAKEASY_SERVER_ADDRESS over to
// the dev-idp's /local-speakeasy/ prefix.
package localspeakeasy

import (
	"context"
	"crypto/rand"
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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/devidp/database/repo"
	"github.com/speakeasy-api/gram/server/internal/devidp/defaultuser"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Mode is the discriminator persisted on every auth_codes / tokens /
// current_users row owned by this handler.
const Mode = "local-speakeasy"

// Prefix is the URL prefix the dev-idp listener mounts the local-speakeasy
// handler under. Compose it with http.StripPrefix when wiring.
const Prefix = "/local-speakeasy"

// clientIDSentinel is the string written to auth_codes.client_id and
// tokens.client_id for local-speakeasy traffic. The Speakeasy provider
// exchange has no real client concept; the column is NOT NULL on the
// schema, so we stamp this constant for inspection in the dashboard.
const clientIDSentinel = "local-speakeasy"

const (
	authCodeLifetime = 5 * time.Minute
	tokenLifetime    = 24 * time.Hour
)

// Config carries the static configuration for the local-speakeasy mode.
type Config struct {
	// SecretKey is the value the `speakeasy-auth-provider-key` request
	// header must match for /exchange, /validate, /revoke, /register.
	// Sourced from SPEAKEASY_SECRET_KEY.
	SecretKey string
}

// Handler serves the local-speakeasy mode's HTTP routes.
type Handler struct {
	cfg    Config
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
}

func NewHandler(cfg Config, logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *Handler {
	return &Handler{
		cfg:    cfg,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/devidp/modes/localspeakeasy"),
		logger: logger.With(attr.SlogComponent("devidp." + Mode)),
		db:     db,
	}
}

// Handler returns the http.Handler that should be mounted under
// `Prefix` (use http.StripPrefix). All registered paths are relative to
// that prefix.
func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/speakeasy_provider/login", h.handleLogin)
	mux.HandleFunc("POST /v1/speakeasy_provider/exchange", h.withSecretKey(h.handleExchange))
	mux.HandleFunc("GET /v1/speakeasy_provider/validate", h.withSecretKey(h.handleValidate))
	mux.HandleFunc("POST /v1/speakeasy_provider/revoke", h.withSecretKey(h.handleRevoke))
	mux.HandleFunc("POST /v1/speakeasy_provider/register", h.withSecretKey(h.handleRegister))
	return mux
}

// =============================================================================
// JSON wire types — mirror local-speakeasy-idp/mockidp.go exactly.
// =============================================================================

type userJSON struct {
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

type orgJSON struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Slug               string   `json:"slug"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
	AccountType        string   `json:"account_type"`
	SSOConnectionID    *string  `json:"sso_connection_id"`
	WorkOSID           *string  `json:"workos_id,omitempty"`
	UserWorkspaceSlugs []string `json:"user_workspaces_slugs"` // JSON key intentionally pluralised "workspaces" — preserved from the original to keep wire-compat with Gram-side consumers.
}

type validateResponse struct {
	User          userJSON  `json:"user"`
	Organizations []orgJSON `json:"organizations"`
}

// =============================================================================
// Middleware
// =============================================================================

func (h *Handler) withSecretKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("speakeasy-auth-provider-key")
		if key != h.cfg.SecretKey {
			h.logger.WarnContext(r.Context(), "rejected: invalid provider key", attr.SlogHTTPRoute(r.URL.Path))
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "Unauthorized: invalid or missing provider key",
			})
			return
		}
		next(w, r)
	}
}

// =============================================================================
// Handlers
// =============================================================================

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	returnURL := r.URL.Query().Get("return_url")
	state := r.URL.Query().Get("state")

	if returnURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Missing return_url parameter",
		})
		return
	}

	target, err := url.Parse(returnURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid return_url"})
		return
	}

	userID, err := h.resolveCurrentUserID(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	code, err := h.issueAuthCode(ctx, userID, returnURL)
	if err != nil {
		h.logger.ErrorContext(ctx, "issue auth code", attr.SlogError(err))
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
		// Original behaviour: unknown code → fall back to the configured
		// user, since the original tests sometimes called /exchange
		// without /login. Preserve that here by resolving currentUser.
		uid, ferr := h.resolveCurrentUserID(ctx)
		if ferr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or expired code"})
			return
		}
		token, terr := h.issueIDToken(ctx, uid)
		if terr != nil {
			h.logger.ErrorContext(ctx, "issue id_token (fallback)", attr.SlogError(terr))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id_token": token})
		return
	}

	token, err := h.issueIDToken(ctx, codeRow.UserID)
	if err != nil {
		h.logger.ErrorContext(ctx, "issue id_token", attr.SlogError(err))
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
		h.logger.ErrorContext(ctx, "build validate response", attr.SlogError(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load user"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idToken := r.Header.Get("speakeasy-auth-provider-id-token")
	if idToken != "" {
		if err := repo.New(h.db).RevokeToken(ctx, repo.RevokeTokenParams{Token: idToken, Mode: Mode}); err != nil {
			h.logger.WarnContext(ctx, "revoke token", attr.SlogError(err))
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

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
	if accountType == "" {
		accountType = "free"
	}

	dbtx, err := h.db.Begin(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "begin register tx", attr.SlogError(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to register"})
		return
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	org, err := q.CreateOrganization(ctx, repo.CreateOrganizationParams{
		Name:        body.OrganizationName,
		Slug:        slugify(body.OrganizationName),
		AccountType: pgtype.Text{String: accountType, Valid: true},
		WorkosID:    pgtype.Text{String: "", Valid: false},
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "create organization", attr.SlogError(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create organization"})
		return
	}
	if _, err := q.CreateMembership(ctx, repo.CreateMembershipParams{
		UserID:         tokenRow.UserID,
		OrganizationID: org.ID,
		Role:           pgtype.Text{String: "member", Valid: true},
	}); err != nil {
		h.logger.ErrorContext(ctx, "create membership", attr.SlogError(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to attach membership"})
		return
	}
	if err := dbtx.Commit(ctx); err != nil {
		h.logger.ErrorContext(ctx, "commit register tx", attr.SlogError(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to register"})
		return
	}

	resp, err := h.validateResponseFor(ctx, repo.New(h.db), tokenRow.UserID)
	if err != nil {
		h.logger.ErrorContext(ctx, "build register response", attr.SlogError(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load user"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// =============================================================================
// Helpers
// =============================================================================

// errCurrentUserMissing is returned when the currentUser names a
// users.id that no longer exists in the local users table. Surfaced as a
// 500 on the wire — this is an integrity error, not a request error.
var errCurrentUserMissing = errors.New("currentUser references a missing user row")

func (h *Handler) resolveCurrentUserID(ctx context.Context) (uuid.UUID, error) {
	queries := repo.New(h.db)
	row, err := queries.GetCurrentUser(ctx, Mode)
	if errors.Is(err, pgx.ErrNoRows) {
		// First touch on this mode: bootstrap the default user from the
		// local git committer (idp-design.md §3).
		uid, berr := defaultuser.BootstrapLocalUser(ctx, h.db, Mode)
		if berr != nil {
			return uuid.Nil, fmt.Errorf("bootstrap default currentUser: %w", berr)
		}
		return uid, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("read currentUser: %w", err)
	}
	id, err := uuid.Parse(row.SubjectRef)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse currentUser subject_ref: %w", err)
	}
	if _, err := queries.GetUser(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, errCurrentUserMissing
		}
		return uuid.Nil, fmt.Errorf("look up currentUser: %w", err)
	}
	return id, nil
}

func (h *Handler) issueAuthCode(ctx context.Context, userID uuid.UUID, returnURL string) (string, error) {
	code := randomToken()
	_, err := repo.New(h.db).CreateAuthCode(ctx, repo.CreateAuthCodeParams{
		Code:                code,
		Mode:                Mode,
		UserID:              userID,
		ClientID:            clientIDSentinel,
		RedirectUri:         returnURL,
		CodeChallenge:       pgtype.Text{String: "", Valid: false},
		CodeChallengeMethod: pgtype.Text{String: "", Valid: false},
		Scope:               pgtype.Text{String: "", Valid: false},
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(authCodeLifetime), Valid: true, InfinityModifier: pgtype.Finite},
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
		Scope:     pgtype.Text{String: "", Valid: false},
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(tokenLifetime), Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		return "", fmt.Errorf("insert id_token: %w", err)
	}
	return token, nil
}

func (h *Handler) validateResponseFor(ctx context.Context, queries *repo.Queries, userID uuid.UUID) (validateResponse, error) {
	user, err := queries.GetUser(ctx, userID)
	if err != nil {
		return validateResponse{}, fmt.Errorf("look up user: %w", err)
	}
	orgs, err := queries.ListOrganizationsForUser(ctx, userID)
	if err != nil {
		return validateResponse{}, fmt.Errorf("list organizations for user: %w", err)
	}

	out := validateResponse{
		User: userJSON{
			ID:           user.ID.String(),
			Email:        user.Email,
			DisplayName:  user.DisplayName,
			PhotoURL:     pgTextPtr(user.PhotoUrl),
			GithubHandle: pgTextPtr(user.GithubHandle),
			Admin:        user.Admin,
			CreatedAt:    user.CreatedAt.Time.UTC().Format(time.RFC3339),
			UpdatedAt:    user.UpdatedAt.Time.UTC().Format(time.RFC3339),
			Whitelisted:  user.Whitelisted,
		},
		Organizations: make([]orgJSON, 0, len(orgs)),
	}
	for _, o := range orgs {
		out.Organizations = append(out.Organizations, orgJSON{
			ID:                 o.ID.String(),
			Name:               o.Name,
			Slug:               o.Slug,
			CreatedAt:          o.CreatedAt.Time.UTC().Format(time.RFC3339),
			UpdatedAt:          o.UpdatedAt.Time.UTC().Format(time.RFC3339),
			AccountType:        o.AccountType,
			SSOConnectionID:    nil,
			WorkOSID:           pgTextPtr(o.WorkosID),
			UserWorkspaceSlugs: []string{o.Slug},
		})
	}
	return out, nil
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

func pgTextPtr(t pgtype.Text) *string {
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
