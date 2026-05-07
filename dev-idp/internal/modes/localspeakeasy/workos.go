// WorkOS-shaped REST surface served by the local-speakeasy mode. Wire
// shapes match workos-go/v6 SDK types (see workos_types.go) so Gram-side's
// `*workos.Client` decodes our responses identically to api.workos.com.
//
// Endpoint inventory (idp-design.md §7.1, "WorkOS emulation" block):
//
//	GET  /user_management/users/{id}
//	GET  /user_management/users                                              (?email, ?organization_id, ?after, ?limit)
//	GET  /organizations/{id}
//	GET  /user_management/organization_memberships                           (?user_id, ?organization_id, ?after, ?limit)
//	PUT  /user_management/organization_memberships/{id}                      ({role_slug})
//	DELETE /user_management/organization_memberships/{id}
//	POST   /user_management/invitations                                      (send)
//	GET    /user_management/invitations                                      (?organization_id, ?after, ?limit)
//	GET    /user_management/invitations/{id}
//	GET    /user_management/invitations/by_token/{token}
//	POST   /user_management/invitations/{id}/revoke
//	POST   /user_management/invitations/{id}/send                            (resend; no-op apart from updated_at)
//	POST   /user_management/invitations/{id}/accept                          (dev-idp only — dashboard accept flow)
//	GET    /organizations/{id}/roles
//	POST   /authorization/organizations/{id}/roles
//	PATCH  /authorization/organizations/{id}/roles/{slug}
//	DELETE /authorization/organizations/{id}/roles/{slug}
package localspeakeasy

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/defaultuser"
)

// invitationLifetime is how long an emulated invitation stays in the
// "pending" state before being treated as expired. Generous enough that
// long-running test suites won't trip over it.
const invitationLifetime = 30 * 24 * time.Hour

// defaultPageSize / maxPageSize match WorkOS's documented limits closely
// enough for any caller written against the live API to behave the same
// against the emulator.
const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// =============================================================================
// Routes
// =============================================================================

func (h *Handler) registerWorkosRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /user_management/users/{id}", h.handleWorkosGetUser)
	mux.HandleFunc("GET /user_management/users", h.handleWorkosListUsers)

	mux.HandleFunc("GET /organizations/{id}", h.handleWorkosGetOrganization)

	mux.HandleFunc("GET /user_management/organization_memberships", h.handleWorkosListMemberships)
	mux.HandleFunc("PUT /user_management/organization_memberships/{id}", h.handleWorkosUpdateMembership)
	mux.HandleFunc("DELETE /user_management/organization_memberships/{id}", h.handleWorkosDeleteMembership)

	mux.HandleFunc("POST /user_management/invitations", h.handleWorkosSendInvitation)
	mux.HandleFunc("GET /user_management/invitations", h.handleWorkosListInvitations)
	mux.HandleFunc("GET /user_management/invitations/by_token/{token}", h.handleWorkosFindInvitationByToken)
	mux.HandleFunc("GET /user_management/invitations/{id}", h.handleWorkosGetInvitation)
	mux.HandleFunc("POST /user_management/invitations/{id}/revoke", h.handleWorkosRevokeInvitation)
	mux.HandleFunc("POST /user_management/invitations/{id}/send", h.handleWorkosResendInvitation)
	mux.HandleFunc("POST /user_management/invitations/{id}/accept", h.handleWorkosAcceptInvitation)

	mux.HandleFunc("GET /organizations/{id}/roles", h.handleWorkosListRoles)
	mux.HandleFunc("POST /authorization/organizations/{id}/roles", h.handleWorkosCreateRole)
	mux.HandleFunc("PATCH /authorization/organizations/{id}/roles/{slug}", h.handleWorkosUpdateRole)
	mux.HandleFunc("DELETE /authorization/organizations/{id}/roles/{slug}", h.handleWorkosDeleteRole)
}

// =============================================================================
// Users
// =============================================================================

func (h *Handler) handleWorkosGetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	user, err := repo.New(h.db).GetUser(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeWorkosError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "workos get user", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	writeJSON(w, http.StatusOK, workosUserView(user))
}

func (h *Handler) handleWorkosListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit, after, err := parsePageParams(q)
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, err.Error())
		return
	}

	emailNarg := sql.NullString{String: "", Valid: false}
	if email := q.Get("email"); email != "" {
		emailNarg = sql.NullString{String: email, Valid: true}
	}
	orgFilter, err := optionalQueryUUID(q, "organization_id")
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := repo.New(h.db).ListUsersFiltered(ctx, repo.ListUsersFilteredParams{
		After:          after,
		Email:          emailNarg,
		OrganizationID: orgFilter,
		MaxRows:        int64(limit) + 1, //nolint:gosec // limit is clamped above
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "workos list users", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	page, nextCursor := trimPage(rows, limit, func(u repo.User) string { return u.ID.String() })

	out := workosUserList{
		Data:         make([]workosUser, 0, len(page)),
		ListMetadata: listMetadata{Before: "", After: nextCursor},
	}
	for _, u := range page {
		out.Data = append(out.Data, workosUserView(u))
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// Organizations
// =============================================================================

func (h *Handler) handleWorkosGetOrganization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	org, err := repo.New(h.db).GetOrganization(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeWorkosError(w, http.StatusNotFound, "organization not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "workos get organization", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to load organization")
		return
	}
	writeJSON(w, http.StatusOK, workosOrganizationView(org))
}

// =============================================================================
// Memberships
// =============================================================================

func (h *Handler) handleWorkosListMemberships(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit, after, err := parsePageParams(q)
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, err.Error())
		return
	}
	userFilter, err := optionalQueryUUID(q, "user_id")
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, err.Error())
		return
	}
	orgFilter, err := optionalQueryUUID(q, "organization_id")
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := repo.New(h.db).ListMembershipsWithOrgName(ctx, repo.ListMembershipsWithOrgNameParams{
		After:          after,
		UserID:         userFilter,
		OrganizationID: orgFilter,
		MaxRows:        int64(limit) + 1, //nolint:gosec // clamped
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "workos list memberships", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to list memberships")
		return
	}

	page, nextCursor := trimPage(rows, limit, func(m repo.ListMembershipsWithOrgNameRow) string { return m.ID.String() })

	out := workosOrganizationMembershipList{
		Data:         make([]workosOrganizationMembership, 0, len(page)),
		ListMetadata: listMetadata{Before: "", After: nextCursor},
	}
	for _, m := range page {
		out.Data = append(out.Data, workosMembershipView(m))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleWorkosUpdateMembership(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid membership id")
		return
	}
	var body struct {
		RoleSlug string `json:"role_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.RoleSlug == "" {
		writeWorkosError(w, http.StatusBadRequest, "role_slug is required")
		return
	}

	queries := repo.New(h.db)
	if _, err := queries.UpdateMembership(ctx, repo.UpdateMembershipParams{ID: id, Role: body.RoleSlug, Ts: time.Now()}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeWorkosError(w, http.StatusNotFound, "membership not found")
			return
		}
		h.logger.ErrorContext(ctx, "workos update membership", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to update membership")
		return
	}
	row, err := queries.GetMembershipWithOrgName(ctx, id)
	if err != nil {
		h.logger.ErrorContext(ctx, "workos refetch membership", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to load membership")
		return
	}
	writeJSON(w, http.StatusOK, workosMembershipView(repo.ListMembershipsWithOrgNameRow(row)))
}

func (h *Handler) handleWorkosDeleteMembership(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid membership id")
		return
	}
	if err := repo.New(h.db).DeleteMembership(ctx, id); err != nil {
		h.logger.ErrorContext(ctx, "workos delete membership", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to delete membership")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// =============================================================================
// Invitations
// =============================================================================

func (h *Handler) handleWorkosSendInvitation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Email          string `json:"email"`
		OrganizationID string `json:"organization_id"`
		InviterUserID  string `json:"inviter_user_id"`
		RoleSlug       string `json:"role_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Email == "" || body.OrganizationID == "" {
		writeWorkosError(w, http.StatusBadRequest, "email and organization_id are required")
		return
	}
	orgID, err := uuid.Parse(body.OrganizationID)
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid organization_id")
		return
	}

	inviterNarg := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if body.InviterUserID != "" {
		inv, err := uuid.Parse(body.InviterUserID)
		if err != nil {
			writeWorkosError(w, http.StatusBadRequest, "invalid inviter_user_id")
			return
		}
		inviterNarg = uuid.NullUUID{UUID: inv, Valid: true}
	}

	row, err := repo.New(h.db).CreateInvitation(ctx, repo.CreateInvitationParams{
		ID:             uuid.New(),
		Email:          body.Email,
		OrganizationID: orgID,
		Token:          randomToken(),
		InviterUserID:  inviterNarg,
		ExpiresAt:      time.Now().Add(invitationLifetime),
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "workos send invitation", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to create invitation")
		return
	}
	writeJSON(w, http.StatusCreated, workosInvitationView(row))
}

func (h *Handler) handleWorkosListInvitations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	limit, after, err := parsePageParams(q)
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, err.Error())
		return
	}
	orgIDRaw := q.Get("organization_id")
	if orgIDRaw == "" {
		writeWorkosError(w, http.StatusBadRequest, "organization_id is required")
		return
	}
	orgID, err := uuid.Parse(orgIDRaw)
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid organization_id")
		return
	}

	rows, err := repo.New(h.db).ListInvitationsByOrg(ctx, repo.ListInvitationsByOrgParams{
		OrganizationID: orgID,
		After:          after,
		MaxRows:        int64(limit) + 1, //nolint:gosec // clamped
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "workos list invitations", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}

	page, nextCursor := trimPage(rows, limit, func(inv repo.Invitation) string { return inv.ID.String() })
	out := workosInvitationList{
		Data:         make([]workosInvitation, 0, len(page)),
		ListMetadata: listMetadata{Before: "", After: nextCursor},
	}
	for _, inv := range page {
		out.Data = append(out.Data, workosInvitationView(inv))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleWorkosGetInvitation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid invitation id")
		return
	}
	row, err := repo.New(h.db).GetInvitation(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeWorkosError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "workos get invitation", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to load invitation")
		return
	}
	writeJSON(w, http.StatusOK, workosInvitationView(row))
}

func (h *Handler) handleWorkosFindInvitationByToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := r.PathValue("token")
	row, err := repo.New(h.db).GetInvitationByToken(ctx, token)
	if errors.Is(err, sql.ErrNoRows) {
		writeWorkosError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "workos find invitation", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to load invitation")
		return
	}
	writeJSON(w, http.StatusOK, workosInvitationView(row))
}

func (h *Handler) handleWorkosRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid invitation id")
		return
	}
	row, err := repo.New(h.db).RevokeInvitation(ctx, repo.RevokeInvitationParams{ID: id, Ts: time.Now()})
	if errors.Is(err, sql.ErrNoRows) {
		writeWorkosError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "workos revoke invitation", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to revoke invitation")
		return
	}
	writeJSON(w, http.StatusOK, workosInvitationView(row))
}

func (h *Handler) handleWorkosResendInvitation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid invitation id")
		return
	}
	row, err := repo.New(h.db).TouchInvitation(ctx, repo.TouchInvitationParams{ID: id, Ts: time.Now()})
	if errors.Is(err, sql.ErrNoRows) {
		writeWorkosError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "workos resend invitation", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to touch invitation")
		return
	}
	writeJSON(w, http.StatusOK, workosInvitationView(row))
}

// handleWorkosAcceptInvitation is a dev-idp-only endpoint. Real WorkOS ties
// invitation acceptance into the user-management session flow; for the
// emulator the dashboard's accept-flow UI hits this directly. Idempotent
// — flips state to accepted and creates the membership for the email's
// user (find-or-create).
func (h *Handler) handleWorkosAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid invitation id")
		return
	}

	queries := repo.New(h.db)
	inv, err := queries.GetInvitation(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeWorkosError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "workos accept: load invitation", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to load invitation")
		return
	}
	if inv.State == "revoked" {
		writeWorkosError(w, http.StatusConflict, "invitation has been revoked")
		return
	}

	// Find or create the user by the invited email; the display_name
	// defaults to the email local-part since we don't have a name yet.
	user, err := queries.UpsertUserByEmail(ctx, repo.UpsertUserByEmailParams{
		ID:          defaultuser.DeterministicUserID(inv.Email),
		Email:       inv.Email,
		DisplayName: emailLocalPart(inv.Email),
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "workos accept: upsert user", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to provision user")
		return
	}
	if _, err := queries.CreateMembership(ctx, repo.CreateMembershipParams{
		ID:             uuid.New(),
		UserID:         user.ID,
		OrganizationID: inv.OrganizationID,
		Role:           sql.NullString{String: "member", Valid: true},
	}); err != nil {
		h.logger.ErrorContext(ctx, "workos accept: upsert membership", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to attach membership")
		return
	}

	row, err := queries.AcceptInvitation(ctx, repo.AcceptInvitationParams{ID: id, Ts: time.Now()})
	if err != nil {
		h.logger.ErrorContext(ctx, "workos accept: flip state", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}
	writeJSON(w, http.StatusOK, workosInvitationView(row))
}

// =============================================================================
// Roles
// =============================================================================

func (h *Handler) handleWorkosListRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orgID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	rows, err := repo.New(h.db).ListOrganizationRoles(ctx, orgID)
	if err != nil {
		h.logger.ErrorContext(ctx, "workos list roles", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to list roles")
		return
	}
	out := workosRoleList{
		Data:         make([]workosRole, 0, len(rows)),
		ListMetadata: listMetadata{Before: "", After: ""},
	}
	for _, r := range rows {
		out.Data = append(out.Data, workosRoleView(r))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleWorkosCreateRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orgID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	var body struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" || body.Slug == "" {
		writeWorkosError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	row, err := repo.New(h.db).CreateOrganizationRole(ctx, repo.CreateOrganizationRoleParams{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Slug:           body.Slug,
		Name:           body.Name,
		Description:    sql.NullString{String: body.Description, Valid: body.Description != ""},
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "workos create role", slog.Any("error", err))
		writeWorkosError(w, http.StatusConflict, "role exists or insert failed")
		return
	}
	writeJSON(w, http.StatusCreated, workosRoleView(row))
}

func (h *Handler) handleWorkosUpdateRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orgID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	slug := r.PathValue("slug")
	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	row, err := repo.New(h.db).UpdateOrganizationRole(ctx, repo.UpdateOrganizationRoleParams{
		OrganizationID: orgID,
		Slug:           slug,
		Name:           ptrToNullString(body.Name),
		Description:    ptrToNullString(body.Description),
		Ts:             time.Now(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeWorkosError(w, http.StatusNotFound, "role not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "workos update role", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to update role")
		return
	}
	writeJSON(w, http.StatusOK, workosRoleView(row))
}

func (h *Handler) handleWorkosDeleteRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orgID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	slug := r.PathValue("slug")
	if err := repo.New(h.db).DeleteOrganizationRole(ctx, repo.DeleteOrganizationRoleParams{
		OrganizationID: orgID,
		Slug:           slug,
	}); err != nil {
		h.logger.ErrorContext(ctx, "workos delete role", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to delete role")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// =============================================================================
// View builders
// =============================================================================

func workosUserView(u repo.User) workosUser {
	first, last := splitName(u.DisplayName)
	return workosUser{
		ID:                u.ID.String(),
		FirstName:         first,
		LastName:          last,
		Email:             u.Email,
		EmailVerified:     true,
		ProfilePictureURL: pgTextOrEmpty(u.PhotoUrl),
		CreatedAt:         u.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         u.UpdatedAt.UTC().Format(time.RFC3339),
		LastSignInAt:      "",
		ExternalID:        "",
		Metadata:          map[string]string{},
	}
}

func workosOrganizationView(o repo.Organization) workosOrganization {
	return workosOrganization{
		ID:                               o.ID.String(),
		Name:                             o.Name,
		AllowProfilesOutsideOrganization: false,
		Domains:                          []workosOrganizationDomain{},
		StripeCustomerID:                 "",
		CreatedAt:                        o.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:                        o.UpdatedAt.UTC().Format(time.RFC3339),
		ExternalID:                       pgTextOrEmpty(o.WorkosID),
	}
}

func workosMembershipView(m repo.ListMembershipsWithOrgNameRow) workosOrganizationMembership {
	return workosOrganizationMembership{
		ID:               m.ID.String(),
		UserID:           m.UserID.String(),
		OrganizationID:   m.OrganizationID.String(),
		OrganizationName: m.OrganizationName,
		Role:             workosRoleSlug{Slug: m.Role},
		Status:           "active",
		CreatedAt:        m.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        m.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func workosInvitationView(inv repo.Invitation) workosInvitation {
	return workosInvitation{
		ID:                  inv.ID.String(),
		Email:               inv.Email,
		State:               inv.State,
		AcceptedAt:          pgTimestamptzOrEmpty(inv.AcceptedAt),
		RevokedAt:           pgTimestamptzOrEmpty(inv.RevokedAt),
		Token:               inv.Token,
		AcceptInvitationURL: "",
		OrganizationID:      inv.OrganizationID.String(),
		InviterUserID:       nullUUIDOrEmpty(inv.InviterUserID),
		ExpiresAt:           inv.ExpiresAt.UTC().Format(time.RFC3339),
		CreatedAt:           inv.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:           inv.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func workosRoleView(r repo.OrganizationRole) workosRole {
	return workosRole{
		ID:          r.ID.String(),
		Name:        r.Name,
		Slug:        r.Slug,
		Description: r.Description,
		Type:        "EnvironmentRole",
		CreatedAt:   r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   r.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// =============================================================================
// Helpers
// =============================================================================

// trimPage applies the standard `limit + 1` keyset-pagination trim used
// across the dev-idp's list endpoints.
func trimPage[T any](rows []T, limit int, cursorOf func(T) string) (page []T, nextCursor string) {
	if len(rows) <= limit {
		return rows, ""
	}
	page = rows[:limit]
	return page, cursorOf(page[len(page)-1])
}

func parsePageParams(q map[string][]string) (limit int, after uuid.UUID, err error) {
	limit = defaultPageSize
	if raw := firstQuery(q, "limit"); raw != "" {
		n, perr := strconv.Atoi(raw)
		if perr != nil {
			return 0, uuid.Nil, errors.New("invalid limit")
		}
		if n < 1 {
			n = 1
		}
		if n > maxPageSize {
			n = maxPageSize
		}
		limit = n
	}
	after = uuid.Nil
	if raw := firstQuery(q, "after"); raw != "" {
		parsed, perr := uuid.Parse(raw)
		if perr != nil {
			return 0, uuid.Nil, errors.New("invalid after cursor")
		}
		after = parsed
	}
	return limit, after, nil
}

func optionalQueryUUID(q map[string][]string, key string) (uuid.NullUUID, error) {
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	raw := firstQuery(q, key)
	if raw == "" {
		return none, nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return none, fmt.Errorf("invalid %s", key)
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}, nil
}

func firstQuery(q map[string][]string, key string) string {
	if vals, ok := q[key]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}

func splitName(displayName string) (first, last string) {
	trimmed := strings.TrimSpace(displayName)
	if trimmed == "" {
		return "", ""
	}
	parts := strings.SplitN(trimmed, " ", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func emailLocalPart(email string) string {
	before, _, ok := strings.Cut(email, "@")
	if !ok {
		return email
	}
	return before
}

func pgTextOrEmpty(t sql.NullString) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func pgTimestamptzOrEmpty(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func nullUUIDOrEmpty(u uuid.NullUUID) string {
	if !u.Valid {
		return ""
	}
	return u.UUID.String()
}

func ptrToNullString(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

// writeWorkosError follows the WorkOS error envelope shape closely enough
// that workos-go's `workos_errors.HTTPError` (which the wrapper translates
// into the unified APIError) parses status correctly without exposing
// `error_description` as a structured field.
func writeWorkosError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"message": msg,
		"code":    statusCodeForJSON(status),
	})
}

func statusCodeForJSON(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	default:
		return "server_error"
	}
}
