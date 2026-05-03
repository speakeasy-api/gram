// Package workos implements the dev-idp's workos mode — a thin proxy over
// the live WorkOS REST API used to resolve user/org metadata for tests and
// for the dashboard (idp-design.md §7.2).
//
// Unlike the local modes, this mode does NOT read from the dev-idp's
// users / organizations / memberships tables. Its only dev-idp DB
// interaction is reading the current_users[mode='workos'] row to discover
// the WorkOS sub the operator/test pinned via /rpc/devIdp.setCurrentUser.
//
// The mode is unmounted entirely when WORKOS_API_KEY is unset (see
// idp-design.md §8). When mounted, every request hits the live WorkOS
// API (or whatever WORKOS_HOST points at — useful for sandbox / fixtures)
// using the configured API key.
package workos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/devidp/database/repo"
	"github.com/speakeasy-api/gram/server/internal/devidp/defaultuser"
	gramworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// Mode is the discriminator persisted on the current_users row owned by
// this handler.
const Mode = "workos"

// Prefix is the URL prefix the dev-idp listener mounts the workos handler
// under. Compose it with http.StripPrefix when wiring.
const Prefix = "/workos"

// Handler serves the workos mode's HTTP routes.
type Handler struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	client *gramworkos.Client
}

func NewHandler(client *gramworkos.Client, logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *Handler {
	return &Handler{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/devidp/modes/workos"),
		logger: logger.With(attr.SlogComponent("devidp." + Mode)),
		db:     db,
		client: client,
	}
}

// Handler returns the http.Handler that should be mounted under `Prefix`.
// All registered paths are relative to that prefix.
func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id_or_email}", h.handleGetUser)
	mux.HandleFunc("GET /organizations/{id}", h.handleGetOrganization)
	mux.HandleFunc("GET /currentUser", h.handleGetCurrentUser)
	return mux
}

// =============================================================================
// JSON wire types
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
}

// =============================================================================
// Handlers
// =============================================================================

func (h *Handler) handleGetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idOrEmail := r.PathValue("id_or_email")

	user, err := h.lookupUser(ctx, idOrEmail)
	if err != nil {
		h.respondError(ctx, w, "lookup user", err)
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
		h.respondError(ctx, w, "get organization", err)
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
	if errors.Is(err, pgx.ErrNoRows) {
		// First touch on this mode: bootstrap from git committer email by
		// looking it up in WorkOS.
		bootstrapped, berr := h.bootstrapDefaultUser(ctx)
		if berr != nil {
			h.logger.WarnContext(ctx, "bootstrap default workos currentUser", attr.SlogError(berr))
			writeJSON(w, http.StatusNotFound, map[string]string{"error": berr.Error()})
			return
		}
		row = bootstrapped
	} else if err != nil {
		h.logger.ErrorContext(ctx, "read currentUser", attr.SlogError(err))
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
		// currentUser set to a non-existent WorkOS sub. Return what we have
		// so the operator can see the broken state in the dashboard rather
		// than getting a 5xx.
		h.logger.WarnContext(ctx, "currentUser not found in WorkOS", attr.SlogError(err))
	case err != nil:
		h.logger.ErrorContext(ctx, "fetch live workos user", attr.SlogError(err))
	case user != nil:
		resp.Email = strPtr(user.Email)
		resp.FirstName = strPtr(user.FirstName)
		resp.LastName = strPtr(user.LastName)
		resp.ProfilePictureURL = strPtr(user.ProfilePictureURL)
	}

	writeJSON(w, http.StatusOK, resp)
}

// bootstrapDefaultUser is the workos-mode bootstrap path (idp-design.md §3):
// resolve the local git committer's email, look it up in WorkOS, and
// persist the resulting WorkOS sub as current_users[mode='workos']. Errors
// out of this path surface to /workos/currentUser as a 404 — the operator
// can't log in without either fixing the git config, registering the
// committer in WorkOS, or calling /rpc/devIdp.setCurrentUser explicitly.
func (h *Handler) bootstrapDefaultUser(ctx context.Context) (repo.CurrentUser, error) {
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
// Helpers
// =============================================================================

// lookupUser dispatches to GetUser or GetUserByEmail based on whether the
// segment looks like an email (contains '@'). WorkOS user ids start with
// `user_` but we use the simpler email-vs-not heuristic so the path also
// accepts external-id-shaped strings.
func (h *Handler) lookupUser(ctx context.Context, idOrEmail string) (*gramworkos.User, error) {
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

// respondError translates a WorkOS API error into the matching HTTP status.
// Unknown errors become 502 (gateway) so the operator knows the failure
// originated upstream of the dev-idp.
func (h *Handler) respondError(ctx context.Context, w http.ResponseWriter, op string, err error) {
	if gramworkos.IsNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found in WorkOS"})
		return
	}
	h.logger.ErrorContext(ctx, op, attr.SlogError(err))
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
