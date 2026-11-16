package directory

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/directory/repo"
)

// NameFillReport contains counts only. It deliberately omits names and emails
// from output.
type NameFillReport struct {
	DryRun        bool  `json:"dry_run"`
	Directories   int   `json:"directories"`
	ListedUsers   int   `json:"listed_users"`
	UsersWithName int   `json:"users_with_name"`
	UsersFilled   int64 `json:"users_filled"`
}

// FillDirectoryUserNames copies first and last names from the WorkOS directory
// inventory onto directory users that have no name yet. Rows with any name set
// are left alone because events are the source of truth, and the update only
// touches names, never event cursors, timestamps or deletion markers. The write
// runs under the organization's event-sync lock and is rejected when events
// arrived while the inventory was being listed. Dry runs roll back.
func FillDirectoryUserNames(ctx context.Context, beginner interface {
	Begin(context.Context) (pgx.Tx, error)
}, inventory DirectoryInventory, organizationID, workosOrganizationID string, dryRun bool) (NameFillReport, error) {
	if organizationID == "" || workosOrganizationID == "" {
		return NameFillReport{}, fmt.Errorf("organization IDs are required")
	}

	snapshot, err := SnapshotOrganizationSync(ctx, beginner, workosOrganizationID)
	if err != nil {
		return NameFillReport{}, err
	}

	directories, err := inventory.ListDirectories(ctx, workosOrganizationID)
	if err != nil {
		return NameFillReport{}, fmt.Errorf("list WorkOS directories: %w", err)
	}

	report := NameFillReport{DryRun: dryRun, Directories: len(directories), ListedUsers: 0, UsersWithName: 0, UsersFilled: 0}
	seen := map[string]struct{}{}
	ids := make([]string, 0)
	firstNames := make([]string, 0)
	lastNames := make([]string, 0)
	for _, dir := range directories {
		if dir.ID == "" || dir.OrganizationID != workosOrganizationID {
			return NameFillReport{}, fmt.Errorf("WorkOS directory %q has an unexpected organization ID", dir.ID)
		}
		users, err := inventory.ListDirectoryUsers(ctx, dir.ID)
		if err != nil {
			return NameFillReport{}, fmt.Errorf("list users for WorkOS directory %q: %w", dir.ID, err)
		}
		for _, user := range users {
			if user.ID == "" || user.OrganizationID != workosOrganizationID || user.DirectoryID != dir.ID {
				return NameFillReport{}, fmt.Errorf("WorkOS directory user %q has mismatched organization or directory IDs", user.ID)
			}
			// A user listed twice could match one row twice with different
			// names, so an inconsistent listing is refused outright.
			if _, ok := seen[user.ID]; ok {
				return NameFillReport{}, fmt.Errorf("WorkOS directory user %q is listed more than once", user.ID)
			}
			seen[user.ID] = struct{}{}
			report.ListedUsers++
			first := strings.TrimSpace(user.FirstName)
			last := strings.TrimSpace(user.LastName)
			if first == "" && last == "" {
				continue
			}
			report.UsersWithName++
			ids = append(ids, user.ID)
			firstNames = append(firstNames, first)
			lastNames = append(lastNames, last)
		}
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return NameFillReport{}, fmt.Errorf("begin name fill transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := snapshot.LockAndValidate(ctx, tx); err != nil {
		return NameFillReport{}, err
	}
	if len(ids) > 0 {
		filled, err := repo.New(tx).FillDirectoryUserNames(ctx, repo.FillDirectoryUserNamesParams{
			OrganizationID:         organizationID,
			WorkosDirectoryUserIds: ids,
			FirstNames:             firstNames,
			LastNames:              lastNames,
		})
		if err != nil {
			return NameFillReport{}, fmt.Errorf("fill directory user names: %w", err)
		}
		report.UsersFilled = filled
	}
	if dryRun {
		return report, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return NameFillReport{}, fmt.Errorf("commit name fill transaction: %w", err)
	}
	return report, nil
}
