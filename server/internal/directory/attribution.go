package directory

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// DirectoryInventory lists the authoritative WorkOS Directory Sync inventory.
type DirectoryInventory interface {
	ListDirectories(context.Context, string) ([]workos.Directory, error)
	ListDirectoryUsers(context.Context, string) ([]workos.DirectoryUser, error)
	ListDirectoryGroups(context.Context, string) ([]workos.DirectoryGroup, error)
}

// AttributionReport contains IDs and counts only. It deliberately omits WorkOS
// user emails and attributes from output.
type AttributionReport struct {
	DryRun           bool     `json:"dry_run"`
	Directories      int      `json:"directories"`
	UsersAttributed  int64    `json:"users_attributed"`
	GroupsAttributed int64    `json:"groups_attributed"`
	Unmatched        []string `json:"unmatched"`
}

// AttributeDirectorySources assigns directory IDs only to existing, currently
// unattributed rows that occur in the complete WorkOS inventory. Dry runs run
// the same updates inside a transaction and roll them back.
func AttributeDirectorySources(ctx context.Context, beginner interface {
	Begin(context.Context) (pgx.Tx, error)
}, inventory DirectoryInventory, organizationID, workosOrganizationID string, dryRun bool) (AttributionReport, error) {
	if organizationID == "" || workosOrganizationID == "" {
		return AttributionReport{}, fmt.Errorf("organization IDs are required")
	}

	directories, err := inventory.ListDirectories(ctx, workosOrganizationID)
	if err != nil {
		return AttributionReport{}, fmt.Errorf("list WorkOS directories: %w", err)
	}
	directoryByID := make(map[string]struct{}, len(directories))
	for _, dir := range directories {
		if dir.ID == "" || dir.OrganizationID != workosOrganizationID {
			return AttributionReport{}, fmt.Errorf("WorkOS directory %q has an unexpected organization ID", dir.ID)
		}
		if _, exists := directoryByID[dir.ID]; exists {
			return AttributionReport{}, fmt.Errorf("duplicate WorkOS directory ID %q", dir.ID)
		}
		directoryByID[dir.ID] = struct{}{}
	}

	usersByID := make(map[string]string)
	groupsByID := make(map[string]string)
	for _, dir := range directories {
		users, err := inventory.ListDirectoryUsers(ctx, dir.ID)
		if err != nil {
			return AttributionReport{}, fmt.Errorf("list users for WorkOS directory %q: %w", dir.ID, err)
		}
		for _, user := range users {
			if user.ID == "" || user.OrganizationID != workosOrganizationID || user.DirectoryID != dir.ID {
				return AttributionReport{}, fmt.Errorf("WorkOS directory user %q has mismatched organization or directory IDs", user.ID)
			}
			if previous, exists := usersByID[user.ID]; exists && previous != dir.ID {
				return AttributionReport{}, fmt.Errorf("WorkOS directory user %q appears in multiple directories", user.ID)
			}
			usersByID[user.ID] = dir.ID
		}

		groups, err := inventory.ListDirectoryGroups(ctx, dir.ID)
		if err != nil {
			return AttributionReport{}, fmt.Errorf("list groups for WorkOS directory %q: %w", dir.ID, err)
		}
		for _, group := range groups {
			if group.ID == "" || group.OrganizationID != workosOrganizationID || group.DirectoryID != dir.ID {
				return AttributionReport{}, fmt.Errorf("WorkOS directory group %q has mismatched organization or directory IDs", group.ID)
			}
			if previous, exists := groupsByID[group.ID]; exists && previous != dir.ID {
				return AttributionReport{}, fmt.Errorf("WorkOS directory group %q appears in multiple directories", group.ID)
			}
			groupsByID[group.ID] = dir.ID
		}
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return AttributionReport{}, fmt.Errorf("begin attribution transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repo.New(tx)
	unattributed, err := queries.ListUnattributedDirectorySources(ctx, organizationID)
	if err != nil {
		return AttributionReport{}, fmt.Errorf("list unattributed directory sources: %w", err)
	}
	report := AttributionReport{DryRun: dryRun, Directories: len(directories), UsersAttributed: 0, GroupsAttributed: 0, Unmatched: make([]string, 0)}
	var users []string
	var groups []string
	for _, row := range unattributed {
		switch row.Kind {
		case "user":
			if _, found := usersByID[row.WorkosID]; found {
				users = append(users, row.WorkosID)
			} else {
				report.Unmatched = append(report.Unmatched, "user:"+row.WorkosID)
			}
		case "group":
			if _, found := groupsByID[row.WorkosID]; found {
				groups = append(groups, row.WorkosID)
			} else {
				report.Unmatched = append(report.Unmatched, "group:"+row.WorkosID)
			}
		default:
			return AttributionReport{}, fmt.Errorf("unknown unattributed directory source kind %q", row.Kind)
		}
	}

	for _, id := range users {
		count, err := queries.AttributeDirectoryUser(ctx, repo.AttributeDirectoryUserParams{
			DirectoryID: usersByID[id], OrganizationID: organizationID, WorkosDirectoryUserID: id,
		})
		if err != nil {
			return AttributionReport{}, fmt.Errorf("attribute WorkOS directory user %q: %w", id, err)
		}
		report.UsersAttributed += count
	}
	for _, id := range groups {
		count, err := queries.AttributeDirectoryGroup(ctx, repo.AttributeDirectoryGroupParams{
			DirectoryID: groupsByID[id], OrganizationID: organizationID, WorkosDirectoryGroupID: id,
		})
		if err != nil {
			return AttributionReport{}, fmt.Errorf("attribute WorkOS directory group %q: %w", id, err)
		}
		report.GroupsAttributed += count
	}
	if dryRun {
		return report, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return AttributionReport{}, fmt.Errorf("commit attribution transaction: %w", err)
	}
	return report, nil
}
