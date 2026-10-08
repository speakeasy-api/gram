package directory

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
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

	snapshotTx, err := beginner.Begin(ctx)
	if err != nil {
		return AttributionReport{}, fmt.Errorf("begin sync cursor snapshot: %w", err)
	}
	snapshot, err := organizationSyncState(ctx, workosrepo.New(snapshotTx), workosOrganizationID)
	_ = snapshotTx.Rollback(ctx)
	if err != nil {
		return AttributionReport{}, err
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
	syncQueries := workosrepo.New(tx)
	if err := syncQueries.LockOrganizationSync(ctx, workosOrganizationID); err != nil {
		return AttributionReport{}, fmt.Errorf("lock organization event sync: %w", err)
	}
	current, err := organizationSyncState(ctx, syncQueries, workosOrganizationID)
	if err != nil {
		return AttributionReport{}, err
	}
	if current.ID != snapshot.ID || current.LastEventID != snapshot.LastEventID ||
		current.UpdatedAt.Valid != snapshot.UpdatedAt.Valid || !current.UpdatedAt.Time.Equal(snapshot.UpdatedAt.Time) {
		return AttributionReport{}, fmt.Errorf("organization events changed during inventory retrieval; rerun with fresh inventory and review unattributed residuals")
	}
	queries := repo.New(tx)
	unattributed, err := queries.ListUnattributedDirectorySources(ctx, organizationID)
	if err != nil {
		return AttributionReport{}, fmt.Errorf("list unattributed directory sources: %w", err)
	}
	report := AttributionReport{DryRun: dryRun, Directories: len(directories), UsersAttributed: 0, GroupsAttributed: 0, Unmatched: make([]string, 0)}
	users := make(map[string][]string)
	groups := make(map[string][]string)
	for _, row := range unattributed {
		switch row.Kind {
		case "user":
			if directoryID, found := usersByID[row.WorkosID]; found {
				users[directoryID] = append(users[directoryID], row.WorkosID)
			} else {
				report.Unmatched = append(report.Unmatched, "user:"+row.WorkosID)
			}
		case "group":
			if directoryID, found := groupsByID[row.WorkosID]; found {
				groups[directoryID] = append(groups[directoryID], row.WorkosID)
			} else {
				report.Unmatched = append(report.Unmatched, "group:"+row.WorkosID)
			}
		default:
			return AttributionReport{}, fmt.Errorf("unknown unattributed directory source kind %q", row.Kind)
		}
	}

	for _, dir := range directories {
		if ids := users[dir.ID]; len(ids) > 0 {
			count, err := queries.AttributeDirectoryUsers(ctx, repo.AttributeDirectoryUsersParams{
				DirectoryID: dir.ID, OrganizationID: organizationID, WorkosDirectoryUserIds: ids,
			})
			if err != nil {
				return AttributionReport{}, fmt.Errorf("attribute users for WorkOS directory %q: %w", dir.ID, err)
			}
			report.UsersAttributed += count
		}
		if ids := groups[dir.ID]; len(ids) > 0 {
			count, err := queries.AttributeDirectoryGroups(ctx, repo.AttributeDirectoryGroupsParams{
				DirectoryID: dir.ID, OrganizationID: organizationID, WorkosDirectoryGroupIds: ids,
			})
			if err != nil {
				return AttributionReport{}, fmt.Errorf("attribute groups for WorkOS directory %q: %w", dir.ID, err)
			}
			report.GroupsAttributed += count
		}
	}
	if dryRun {
		return report, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return AttributionReport{}, fmt.Errorf("commit attribution transaction: %w", err)
	}
	return report, nil
}

func organizationSyncState(ctx context.Context, queries *workosrepo.Queries, workosOrganizationID string) (workosrepo.GetOrganizationSyncStateRow, error) {
	state, err := queries.GetOrganizationSyncState(ctx, workosOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return workosrepo.GetOrganizationSyncStateRow{}, fmt.Errorf("read organization event sync cursor: %w", err)
	}
	return state, nil
}
