package mockworkos

// Directory Sync emulation: GET /directory_groups[/{id}] and
// GET /directory_users[/{id}]. Each org has one mock directory
// ("directory_mock_<workos org id>", see handleWorkosListDirectories). The
// first list call for an org seeds a fixed set of groups and users, including
// the org's own dev-idp members, so Gram has directory data to map to roles.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
)

const (
	workosDirectoryIDPrefix      = "directory_mock_"
	workosDirectoryGroupIDPrefix = "directory_group_devidp_"
	workosDirectoryUserIDPrefix  = "directory_user_devidp_"
)

func (h *Handler) handleWorkosListDirectoryGroups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	orgID, err := h.directoryOrgID(ctx, firstQuery(q, "directory"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "directory is required")
		return
	}
	limit, after, err := parseDirectoryPageParams(q, workosDirectoryGroupIDPrefix)
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, err.Error())
		return
	}
	userID, err := optionalPrefixedQueryID(q, "user", workosDirectoryUserIDPrefix)
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.ensureDirectoryFixtures(ctx, orgID); err != nil {
		h.logger.ErrorContext(ctx, "workos seed directory fixtures", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to list directory groups")
		return
	}
	orgWorkosID, err := h.orgWorkosID(ctx, orgID)
	if err != nil {
		h.logger.ErrorContext(ctx, "workos list directory groups", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to list directory groups")
		return
	}

	rows, err := repo.New(h.db).ListDirectoryGroups(ctx, repo.ListDirectoryGroupsParams{
		OrganizationID: orgID,
		After:          after,
		UserID:         userID,
		MaxRows:        int64(limit + 1),
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "workos list directory groups", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to list directory groups")
		return
	}

	page, next := trimPage(rows, limit, func(g repo.DirectoryGroup) string { return workosDirectoryGroupID(g.ID) })
	out := workosDirectoryGroupList{
		Data:         make([]workosDirectoryGroup, 0, len(page)),
		ListMetadata: listMetadata{Before: "", After: next},
	}
	for _, g := range page {
		out.Data = append(out.Data, workosDirectoryGroupView(g, orgWorkosID))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleWorkosGetDirectoryGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parsePrefixedID(r.PathValue("id"), workosDirectoryGroupIDPrefix)
	if err != nil {
		writeWorkosError(w, http.StatusNotFound, "directory group not found")
		return
	}
	g, err := repo.New(h.db).GetDirectoryGroup(ctx, id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeWorkosError(w, http.StatusNotFound, "directory group not found")
		return
	case err != nil:
		h.logger.ErrorContext(ctx, "workos get directory group", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to get directory group")
		return
	}
	orgWorkosID, err := h.orgWorkosID(ctx, g.OrganizationID)
	if err != nil {
		h.logger.ErrorContext(ctx, "workos get directory group", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to get directory group")
		return
	}
	writeJSON(w, http.StatusOK, workosDirectoryGroupView(g, orgWorkosID))
}

func (h *Handler) handleWorkosListDirectoryUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	orgID, err := h.directoryOrgID(ctx, firstQuery(q, "directory"))
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, "directory is required")
		return
	}
	limit, after, err := parseDirectoryPageParams(q, workosDirectoryUserIDPrefix)
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, err.Error())
		return
	}
	groupID, err := optionalPrefixedQueryID(q, "group", workosDirectoryGroupIDPrefix)
	if err != nil {
		writeWorkosError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.ensureDirectoryFixtures(ctx, orgID); err != nil {
		h.logger.ErrorContext(ctx, "workos seed directory fixtures", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to list directory users")
		return
	}
	orgWorkosID, err := h.orgWorkosID(ctx, orgID)
	if err != nil {
		h.logger.ErrorContext(ctx, "workos list directory users", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to list directory users")
		return
	}

	rows, err := repo.New(h.db).ListDirectoryUsers(ctx, repo.ListDirectoryUsersParams{
		OrganizationID: orgID,
		After:          after,
		GroupID:        groupID,
		MaxRows:        int64(limit + 1),
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "workos list directory users", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to list directory users")
		return
	}

	page, next := trimPage(rows, limit, func(u repo.DirectoryUser) string { return workosDirectoryUserID(u.ID) })
	out := workosDirectoryUserList{
		Data:         make([]workosDirectoryUser, 0, len(page)),
		ListMetadata: listMetadata{Before: "", After: next},
	}
	for _, u := range page {
		out.Data = append(out.Data, workosDirectoryUserView(u, orgWorkosID))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleWorkosGetDirectoryUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parsePrefixedID(r.PathValue("id"), workosDirectoryUserIDPrefix)
	if err != nil {
		writeWorkosError(w, http.StatusNotFound, "directory user not found")
		return
	}
	u, err := repo.New(h.db).GetDirectoryUser(ctx, id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeWorkosError(w, http.StatusNotFound, "directory user not found")
		return
	case err != nil:
		h.logger.ErrorContext(ctx, "workos get directory user", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to get directory user")
		return
	}
	orgWorkosID, err := h.orgWorkosID(ctx, u.OrganizationID)
	if err != nil {
		h.logger.ErrorContext(ctx, "workos get directory user", slog.Any("error", err))
		writeWorkosError(w, http.StatusInternalServerError, "failed to get directory user")
		return
	}
	writeJSON(w, http.StatusOK, workosDirectoryUserView(u, orgWorkosID))
}

// directoryOrgID maps a mock directory ID back to the dev-idp org that owns it.
func (h *Handler) directoryOrgID(ctx context.Context, directoryID string) (uuid.UUID, error) {
	raw, ok := strings.CutPrefix(directoryID, workosDirectoryIDPrefix)
	if !ok || raw == "" {
		return uuid.Nil, fmt.Errorf("invalid directory %q", directoryID)
	}
	return h.resolveOrgID(ctx, raw)
}

type directoryFixtureUser struct {
	email, first, last, title, department, division string
	groups                                          []string
}

var directoryFixtureGroups = []string{"Engineering", "Design", "Sales", "Security", "Support"}

var directoryFixtureUsers = []directoryFixtureUser{
	{email: "alex.rivera@example.com", first: "Alex", last: "Rivera", title: "Staff Engineer", department: "Engineering", division: "Product", groups: []string{"Engineering", "Security"}},
	{email: "jordan.lee@example.com", first: "Jordan", last: "Lee", title: "Engineering Manager", department: "Engineering", division: "Product", groups: []string{"Engineering"}},
	{email: "priya.shah@example.com", first: "Priya", last: "Shah", title: "Product Designer", department: "Design", division: "Product", groups: []string{"Design"}},
	{email: "morgan.chen@example.com", first: "Morgan", last: "Chen", title: "Account Executive", department: "Sales", division: "Go To Market", groups: []string{"Sales"}},
	{email: "sam.taylor@example.com", first: "Sam", last: "Taylor", title: "Support Engineer", department: "Support", division: "Go To Market", groups: []string{"Support", "Engineering"}},
}

// ensureDirectoryFixtures seeds an org's mock directory on first read. IDs
// are derived from the org and name, so concurrent first reads converge on
// the same rows.
func (h *Handler) ensureDirectoryFixtures(ctx context.Context, orgID uuid.UUID) error {
	count, err := repo.New(h.db).CountDirectoryGroups(ctx, orgID)
	if err != nil {
		return fmt.Errorf("count directory groups: %w", err)
	}
	if count > 0 {
		return nil
	}

	// Seed in one transaction so a failed or concurrent first read never
	// leaves groups without their users and memberships.
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin directory fixture seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	queries := repo.New(tx)

	count, err = queries.CountDirectoryGroups(ctx, orgID)
	if err != nil {
		return fmt.Errorf("count directory groups: %w", err)
	}
	if count > 0 {
		return nil
	}

	groupIDs := make(map[string]uuid.UUID, len(directoryFixtureGroups))
	for _, name := range directoryFixtureGroups {
		id := directoryFixtureID(orgID, "group", name)
		groupIDs[name] = id
		if err := queries.InsertDirectoryGroup(ctx, repo.InsertDirectoryGroupParams{
			ID:             id,
			OrganizationID: orgID,
			Name:           name,
		}); err != nil {
			return fmt.Errorf("insert directory group %q: %w", name, err)
		}
	}

	users := append([]directoryFixtureUser(nil), directoryFixtureUsers...)
	members, err := orgMemberFixtureUsers(ctx, queries, orgID)
	if err != nil {
		return err
	}
	users = append(users, members...)

	for _, u := range users {
		attrs, err := json.Marshal(map[string]string{
			"department_name":  u.department,
			"division_name":    u.division,
			"job_title":        u.title,
			"employee_type":    "Full-time",
			"cost_center_name": u.department,
		})
		if err != nil {
			return fmt.Errorf("marshal directory user attributes: %w", err)
		}
		id := directoryFixtureID(orgID, "user", u.email)
		if err := queries.InsertDirectoryUser(ctx, repo.InsertDirectoryUserParams{
			ID:               id,
			OrganizationID:   orgID,
			Email:            u.email,
			FirstName:        u.first,
			LastName:         u.last,
			JobTitle:         u.title,
			CustomAttributes: string(attrs),
		}); err != nil {
			return fmt.Errorf("insert directory user %q: %w", u.email, err)
		}
		for _, g := range u.groups {
			if err := queries.InsertDirectoryGroupMember(ctx, repo.InsertDirectoryGroupMemberParams{
				GroupID:        groupIDs[g],
				UserID:         id,
				OrganizationID: orgID,
			}); err != nil {
				return fmt.Errorf("insert directory group member %q/%q: %w", g, u.email, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit directory fixture seed: %w", err)
	}
	return nil
}

// orgMemberFixtureUsers puts the org's real dev-idp members in the directory
// so rules written against the fixtures match the person logged in locally.
func orgMemberFixtureUsers(ctx context.Context, queries *repo.Queries, orgID uuid.UUID) ([]directoryFixtureUser, error) {
	memberships, err := queries.ListMemberships(ctx, repo.ListMembershipsParams{
		After:          uuid.Nil,
		UserID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID: uuid.NullUUID{UUID: orgID, Valid: true},
		MaxRows:        maxPageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list org memberships: %w", err)
	}

	out := make([]directoryFixtureUser, 0, len(memberships))
	for _, m := range memberships {
		user, err := queries.GetUser(ctx, m.UserID)
		if err != nil {
			return nil, fmt.Errorf("get org member %s: %w", m.UserID, err)
		}
		first, last := splitName(user.DisplayName)
		out = append(out, directoryFixtureUser{
			email:      user.Email,
			first:      first,
			last:       last,
			title:      "Software Engineer",
			department: "Engineering",
			division:   "Product",
			groups:     []string{"Engineering", "Security"},
		})
	}
	return out, nil
}

func directoryFixtureID(orgID uuid.UUID, kind, name string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(orgID.String()+"/"+kind+"/"+name))
}

func workosDirectoryIDFor(orgWorkosID string) string {
	return workosDirectoryIDPrefix + orgWorkosID
}

func workosDirectoryGroupID(id uuid.UUID) string {
	return workosDirectoryGroupIDPrefix + strings.ReplaceAll(id.String(), "-", "")
}

func workosDirectoryUserID(id uuid.UUID) string {
	return workosDirectoryUserIDPrefix + strings.ReplaceAll(id.String(), "-", "")
}

// parsePrefixedID parses "<prefix><32 hex>" (or a bare UUID) back to a UUID.
func parsePrefixedID(raw, prefix string) (uuid.UUID, error) {
	hex, _ := strings.CutPrefix(raw, prefix)
	id, err := uuid.Parse(hex)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse id %q: %w", raw, err)
	}
	return id, nil
}

func optionalPrefixedQueryID(q map[string][]string, key, prefix string) (uuid.NullUUID, error) {
	raw := firstQuery(q, key)
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if raw == "" {
		return none, nil
	}
	id, err := parsePrefixedID(raw, prefix)
	if err != nil {
		return none, fmt.Errorf("invalid %s", key)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// parseDirectoryPageParams is parsePageParams for cursors carrying a
// directory ID prefix instead of the user prefix.
func parseDirectoryPageParams(q map[string][]string, prefix string) (limit int, after uuid.UUID, err error) {
	cursor := firstQuery(q, "after")
	rest := make(map[string][]string, len(q))
	for k, v := range q {
		if k != "after" {
			rest[k] = v
		}
	}
	limit, _, err = parsePageParams(rest)
	if err != nil || cursor == "" {
		return limit, uuid.Nil, err
	}
	after, err = parsePrefixedID(cursor, prefix)
	if err != nil {
		return 0, uuid.Nil, errors.New("invalid after cursor")
	}
	return limit, after, nil
}

// orgWorkosID is the WorkOS-style ID Gram knows the org by.
func (h *Handler) orgWorkosID(ctx context.Context, orgID uuid.UUID) (string, error) {
	org, err := repo.New(h.db).GetOrganization(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("get organization %s: %w", orgID, err)
	}
	if org.WorkosID.Valid {
		return org.WorkosID.String, nil
	}
	return workosOrgIDFor(orgID), nil
}

func workosDirectoryGroupView(g repo.DirectoryGroup, orgWorkosID string) workosDirectoryGroup {
	return workosDirectoryGroup{
		Object:         "directory_group",
		ID:             workosDirectoryGroupID(g.ID),
		IdpID:          g.ID.String(),
		DirectoryID:    workosDirectoryIDFor(orgWorkosID),
		OrganizationID: orgWorkosID,
		Name:           g.Name,
		RawAttributes:  map[string]any{},
		CreatedAt:      g.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      g.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func workosDirectoryUserView(u repo.DirectoryUser, orgWorkosID string) workosDirectoryUser {
	return workosDirectoryUser{
		Object:           "directory_user",
		ID:               workosDirectoryUserID(u.ID),
		IdpID:            u.ID.String(),
		DirectoryID:      workosDirectoryIDFor(orgWorkosID),
		OrganizationID:   orgWorkosID,
		Email:            u.Email,
		Username:         u.Email,
		Emails:           []workosDirectoryUserEmail{{Primary: true, Type: "work", Value: u.Email}},
		Groups:           []workosDirectoryGroup{},
		FirstName:        u.FirstName,
		LastName:         u.LastName,
		JobTitle:         u.JobTitle,
		State:            u.State,
		RawAttributes:    map[string]any{},
		CustomAttributes: json.RawMessage(u.CustomAttributes),
		CreatedAt:        u.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        u.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
