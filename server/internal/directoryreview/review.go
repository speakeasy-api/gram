// Package directoryreview raises and settles the decisions directory changes
// need from an organization's admins: a new group with no mapping, a new value
// of an attribute key that already has mappings, and a mapped group or
// attribute value that has gone. Every function here runs on the caller's
// transaction, so an item is written or settled together with the directory
// or mapping change that caused it, along with the webhook events announcing
// it. Nothing here writes a mapping.
package directoryreview

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directoryreview/repo"
)

// Kinds of decision.
const (
	KindNewGroup          = "new_group"
	KindNewAttributeValue = "new_attribute_value"
	KindSourceMissing     = "source_missing"
)

// States of an item. Only pending items can change; every other state is
// final, and a recurring condition opens a new occurrence instead.
const (
	StatePending    = "pending"
	StateApproved   = "approved"
	StateDismissed  = "dismissed"
	StateSuperseded = "superseded"
)

// Item is one review item as written or settled.
type Item struct {
	ID               uuid.UUID
	OrganizationID   string
	Kind             string
	DirectoryGroupID uuid.NullUUID
	AttributeKey     pgtype.Text
	AttributeValue   pgtype.Text
	Occurrence       int32
	State            string
}

// Changes lists the items a call opened and settled.
type Changes struct {
	Opened     []Item
	Superseded []Item
}

func (c *Changes) add(other Changes) {
	c.Opened = append(c.Opened, other.Opened...)
	c.Superseded = append(c.Superseded, other.Superseded...)
}

// Empty reports whether nothing was opened or settled.
func (c Changes) Empty() bool {
	return len(c.Opened) == 0 && len(c.Superseded) == 0
}

func items[T ~struct {
	ID               uuid.UUID
	OrganizationID   string
	Kind             string
	DirectoryGroupID uuid.NullUUID
	AttributeKey     pgtype.Text
	AttributeValue   pgtype.Text
	Occurrence       int32
	State            string
}](rows []T) []Item {
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, Item(row))
	}
	return out
}

// ReconcileGroup settles and opens the items for one group after a change to
// the group or its mappings.
func ReconcileGroup(ctx context.Context, dbtx DBTX, organizationID string, groupID uuid.UUID) (Changes, error) {
	changes, err := reconcileGroup(ctx, dbtx, organizationID, groupID)
	if err != nil {
		return changes, err
	}
	return changes, publishChanges(ctx, dbtx, organizationID, changes)
}

func reconcileGroup(ctx context.Context, dbtx database.DBTX, organizationID string, groupID uuid.UUID) (Changes, error) {
	q := repo.New(dbtx)
	group := uuid.NullUUID{UUID: groupID, Valid: true}
	var changes Changes

	superseded, err := q.SupersedeDirectoryReviewItems(ctx, repo.SupersedeDirectoryReviewItemsParams{
		OrganizationID: organizationID, DirectoryGroupID: group, AttributeKey: pgtype.Text{String: "", Valid: false}, AttributeValue: pgtype.Text{String: "", Valid: false},
	})
	if err != nil {
		return changes, fmt.Errorf("supersede group review items: %w", err)
	}
	changes.Superseded = items(superseded)

	opened, err := q.OpenNewGroupItems(ctx, repo.OpenNewGroupItemsParams{OrganizationID: organizationID, DirectoryGroupID: group})
	if err != nil {
		return changes, fmt.Errorf("open new group review item: %w", err)
	}
	changes.Opened = append(changes.Opened, items(opened)...)

	missing, err := q.OpenMissingGroupItems(ctx, repo.OpenMissingGroupItemsParams{OrganizationID: organizationID, DirectoryGroupID: group})
	if err != nil {
		return changes, fmt.Errorf("open missing group review item: %w", err)
	}
	changes.Opened = append(changes.Opened, items(missing)...)

	return changes, nil
}

// ReconcileAttributeValue settles the items for one attribute value after a
// change to its mappings, and opens a missing-source item if it has mappings
// and nobody carries it. It does not raise new_attribute_value: a value is
// only new when a directory event gives it to its first holder.
func ReconcileAttributeValue(ctx context.Context, dbtx DBTX, organizationID, key, value string) (Changes, error) {
	changes, err := reconcileAttributeValue(ctx, dbtx, organizationID, key, value, false)
	if err != nil {
		return changes, err
	}
	return changes, publishChanges(ctx, dbtx, organizationID, changes)
}

func reconcileAttributeValue(ctx context.Context, dbtx database.DBTX, organizationID, key, value string, allowRecurrence bool) (Changes, error) {
	q := repo.New(dbtx)
	var changes Changes

	superseded, err := q.SupersedeDirectoryReviewItems(ctx, repo.SupersedeDirectoryReviewItemsParams{
		OrganizationID: organizationID, DirectoryGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, AttributeKey: conv.ToPGText(key), AttributeValue: conv.ToPGText(value),
	})
	if err != nil {
		return changes, fmt.Errorf("supersede attribute review items: %w", err)
	}
	changes.Superseded = items(superseded)

	missing, err := q.OpenMissingAttributeItems(ctx, repo.OpenMissingAttributeItemsParams{
		OrganizationID: organizationID, AttributeKey: conv.ToPGText(key), AttributeValue: conv.ToPGText(value), AllowRecurrence: allowRecurrence,
	})
	if err != nil {
		return changes, fmt.Errorf("open missing attribute review item: %w", err)
	}
	changes.Opened = append(changes.Opened, items(missing)...)

	return changes, nil
}

// SupersedeAttributeKey settles the pending items for every value of a key,
// such as after the key is opted out of new-value decisions.
func SupersedeAttributeKey(ctx context.Context, dbtx DBTX, organizationID, key string) (Changes, error) {
	superseded, err := repo.New(dbtx).SupersedeDirectoryReviewItems(ctx, repo.SupersedeDirectoryReviewItemsParams{
		OrganizationID: organizationID, DirectoryGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, AttributeKey: conv.ToPGText(key), AttributeValue: pgtype.Text{String: "", Valid: false},
	})
	if err != nil {
		return Changes{}, fmt.Errorf("supersede attribute key review items: %w", err)
	}
	changes := Changes{Opened: nil, Superseded: items(superseded)}
	return changes, publishChanges(ctx, dbtx, organizationID, changes)
}

// UserAttributes is a directory user's attribute values, as access evaluation
// reads them, before or after an event. Nil means the user is not live.
type UserAttributes map[string]string

// LoadUserAttributes reads an organization's directory user's attributes for
// ReconcileUserChange. Postgres renders each value, so it is exactly the text
// ->> returns and mappings match on, nested values included. It returns nil
// when the user is missing or not live.
func LoadUserAttributes(ctx context.Context, dbtx database.DBTX, organizationID, workosDirectoryUserID string) (UserAttributes, error) {
	q := repo.New(dbtx)
	user, err := q.GetDirectoryUserForReview(ctx, repo.GetDirectoryUserForReviewParams{
		OrganizationID:        organizationID,
		WorkosDirectoryUserID: workosDirectoryUserID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("get directory user for review: %w", err)
	}
	if user.Deleted || user.WorkosDeleted {
		return nil, nil
	}
	rows, err := q.ListDirectoryUserAttributesForReview(ctx, repo.ListDirectoryUserAttributesForReviewParams{
		OrganizationID:        organizationID,
		WorkosDirectoryUserID: workosDirectoryUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("list directory user attributes for review: %w", err)
	}
	out := make(UserAttributes, len(rows))
	for _, row := range rows {
		out[row.AttributeKey] = row.AttributeValue
	}
	return out, nil
}

// ReconcileUserChange raises and settles items after a directory event
// changed one user's attributes or liveness. before and after are the user's
// attributes on either side of the event, nil when the user was or is not
// live. Only keys with mappings can matter, so others are skipped.
func ReconcileUserChange(ctx context.Context, dbtx DBTX, organizationID, workosDirectoryUserID string, before, after UserAttributes) (Changes, error) {
	changes, err := reconcileUserChange(ctx, dbtx, organizationID, workosDirectoryUserID, before, after)
	if err != nil {
		return changes, err
	}
	return changes, publishChanges(ctx, dbtx, organizationID, changes)
}

func reconcileUserChange(ctx context.Context, dbtx database.DBTX, organizationID, workosDirectoryUserID string, before, after UserAttributes) (Changes, error) {
	q := repo.New(dbtx)
	var changes Changes

	keys, err := q.ListMappedAttributeKeys(ctx, organizationID)
	if err != nil {
		return changes, fmt.Errorf("list mapped attribute keys: %w", err)
	}

	for _, key := range keys {
		oldValue, hadValue := before[key]
		newValue, hasValue := after[key]
		if hadValue == hasValue && oldValue == newValue {
			continue
		}

		if hasValue {
			others, err := q.CountOtherLiveAttributeHolders(ctx, repo.CountOtherLiveAttributeHoldersParams{
				OrganizationID: organizationID, WorkosDirectoryUserID: workosDirectoryUserID, AttributeKey: key, AttributeValue: newValue,
			})
			if err != nil {
				return changes, fmt.Errorf("count attribute holders: %w", err)
			}
			// The value has a holder now: settle a pending missing-source item.
			settled, err := reconcileAttributeValue(ctx, dbtx, organizationID, key, newValue, false)
			if err != nil {
				return changes, err
			}
			changes.add(settled)
			if others == 0 {
				opened, err := q.OpenNewAttributeValueItem(ctx, repo.OpenNewAttributeValueItemParams{
					OrganizationID: organizationID, AttributeKey: key, AttributeValue: newValue,
				})
				if err != nil {
					return changes, fmt.Errorf("open new attribute value review item: %w", err)
				}
				changes.Opened = append(changes.Opened, items(opened)...)
			}
		}

		if hadValue {
			// When this user was the value's last holder, the value has gone:
			// a mapped value is raised as missing, a new-value item is settled.
			// It may have gone before, so a recurrence is a new occurrence.
			settled, err := reconcileAttributeValue(ctx, dbtx, organizationID, key, oldValue, true)
			if err != nil {
				return changes, err
			}
			changes.add(settled)
		}
	}
	return changes, nil
}

// Sweep reconciles every organization that may need decisions, each in its own
// transaction, so one organization's failure neither rolls back nor blocks the
// others. It returns every organization's error joined.
func Sweep(ctx context.Context, db *pgxpool.Pool) error {
	organizationIDs, err := repo.New(db).ListOrganizationsForDirectoryReviewSweep(ctx)
	if err != nil {
		return fmt.Errorf("list organizations for directory review sweep: %w", err)
	}

	var errs []error
	for _, organizationID := range organizationIDs {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		if err := sweepOrganization(ctx, db, organizationID); err != nil {
			errs = append(errs, fmt.Errorf("organization %s: %w", organizationID, err))
		}
	}
	return errors.Join(errs...)
}

func sweepOrganization(ctx context.Context, db *pgxpool.Pool, organizationID string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin directory review sweep: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := ReconcileOrganization(ctx, tx, organizationID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit directory review sweep: %w", err)
	}
	return nil
}

// ReconcileOrganization recomputes every item for an organization from its
// current state, as the sweep does. On the first run it records the
// organization's baseline, so groups that already exist then are offered in
// bulk setup rather than raised as new.
func ReconcileOrganization(ctx context.Context, dbtx DBTX, organizationID string) (Changes, error) {
	changes, err := reconcileOrganization(ctx, dbtx, organizationID)
	if err != nil {
		return changes, err
	}
	return changes, publishChanges(ctx, dbtx, organizationID, changes)
}

func reconcileOrganization(ctx context.Context, dbtx database.DBTX, organizationID string) (Changes, error) {
	q := repo.New(dbtx)
	var changes Changes

	if err := q.InsertDirectoryReviewOrgStateIfMissing(ctx, organizationID); err != nil {
		return changes, fmt.Errorf("record directory review baseline: %w", err)
	}

	superseded, err := q.SupersedeDirectoryReviewItems(ctx, repo.SupersedeDirectoryReviewItemsParams{
		OrganizationID: organizationID, DirectoryGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, AttributeKey: pgtype.Text{String: "", Valid: false}, AttributeValue: pgtype.Text{String: "", Valid: false},
	})
	if err != nil {
		return changes, fmt.Errorf("supersede review items: %w", err)
	}
	changes.Superseded = items(superseded)

	newGroups, err := q.OpenNewGroupItems(ctx, repo.OpenNewGroupItemsParams{OrganizationID: organizationID, DirectoryGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	if err != nil {
		return changes, fmt.Errorf("open new group review items: %w", err)
	}
	changes.Opened = append(changes.Opened, items(newGroups)...)

	missingGroups, err := q.OpenMissingGroupItems(ctx, repo.OpenMissingGroupItemsParams{OrganizationID: organizationID, DirectoryGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	if err != nil {
		return changes, fmt.Errorf("open missing group review items: %w", err)
	}
	changes.Opened = append(changes.Opened, items(missingGroups)...)

	missingAttributes, err := q.OpenMissingAttributeItems(ctx, repo.OpenMissingAttributeItemsParams{
		OrganizationID: organizationID, AttributeKey: pgtype.Text{String: "", Valid: false}, AttributeValue: pgtype.Text{String: "", Valid: false}, AllowRecurrence: false,
	})
	if err != nil {
		return changes, fmt.Errorf("open missing attribute review items: %w", err)
	}
	changes.Opened = append(changes.Opened, items(missingAttributes)...)

	return changes, nil
}
