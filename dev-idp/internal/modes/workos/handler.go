// Package workos implements the dev-idp's workos mode — proxies WorkOS
// REST API calls to the live WorkOS dev environment. Provides direct
// inspection endpoints for users, organizations, and the current user.
//
// The mode is unmounted entirely when WORKOS_API_KEY is unset.
// When mounted, every request hits the live WorkOS API (or whatever
// WORKOS_HOST points at) using the configured API key.
package workos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/defaultuser"
	gramworkos "github.com/speakeasy-api/gram/dev-idp/internal/workos"
)

const (
	// Mode is the discriminator persisted on current_users rows owned by
	// this handler.
	Mode = "workos"

	// Prefix is the URL prefix the dev-idp listener mounts the workos
	// handler under. Compose with http.StripPrefix when wiring.
	Prefix = "/workos"
)

// Handler serves the workos mode's HTTP routes.
type Handler struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
	client *gramworkos.Client
}

func NewHandler(client *gramworkos.Client, logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *Handler {
	return &Handler{
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
	// Direct WorkOS inspection — used by the dashboard.
	mux.HandleFunc("GET /users/{id_or_email}", h.handleGetUser)
	mux.HandleFunc("GET /organizations/{id}", h.handleGetOrganization)
	mux.HandleFunc("GET /currentUser", h.handleGetCurrentUser)
	return mux
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
