package channels

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/channels/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// RetentionResult reports what one sweep touched.
type RetentionResult struct {
	Organizations int
	Messages      int
	Instances     int
	// Truncated reports that the sweep left eligible rows behind, because it
	// hit its own bounds or an organization failed, so the caller should run it
	// again rather than assume the backlog is clear.
	Truncated bool

	// Next is the organization the following pass should resume after. It is
	// set when this pass stopped at its organization cap and empty when the
	// pass reached the last eligible organization, so a caller that always
	// resumes from it visits every organization before revisiting any.
	Next string
}

// RunRetention performs one bounded retention pass across the organizations
// that currently have something to retire.
//
// Each organization is swept in its own transaction and every statement is
// filtered by organization_id, so one tenant's retention can neither see nor
// delete another's rows, and one failing organization does not roll back the
// rest. Both the number of organizations visited and the rows touched per
// organization are capped, so a sweep costs the same whatever the backlog is;
// the caller runs it again while Truncated is true, resuming after Next.
//
// Removing a message raises the organization's retention floor. A client whose
// cursor falls below that floor is told its cursor is stale instead of
// silently receiving a page with a hole in it.
//
// The sweep runs in the worker, not in the process serving subscriptions, so
// it announces itself with the same transactional delivery hint a write uses.
// A local wake-up would reach nobody.
func RunRetention(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, retention Retention, now time.Time, after string) (RetentionResult, error) {
	result := RetentionResult{Organizations: 0, Messages: 0, Instances: 0, Truncated: false, Next: ""}
	if db == nil {
		return result, fmt.Errorf("%w: no database", ErrUnavailable)
	}
	queries := repo.New(db)
	for result.Organizations < retention.Organizations {
		page, err := queries.ListChannelsOrganizationsNeedingRetention(ctx, repo.ListChannelsOrganizationsNeedingRetentionParams{
			AfterOrganizationID: after,
			Now:                 conv.ToPGTimestamptz(now.UTC()),
			ResultLimit:         int32(min(retention.Organizations-result.Organizations, 100)), //nolint:gosec // bounded above.
		})
		if err != nil {
			return result, fmt.Errorf("list channels organizations needing retention: %w", err)
		}
		if len(page) == 0 {
			return result, nil
		}
		for _, organization := range page {
			after = organization.OrganizationID
			result.Organizations++
			swept, err := sweepOrganization(ctx, db, queries, retention, organization, now)
			if err != nil {
				// One tenant's failure must not stop the sweep: log it and
				// carry on, so a single bad organization cannot stall
				// retention for everyone else.
				// Its rows are still eligible, so the pass is not complete.
				logger.ErrorContext(ctx, "channels retention sweep failed for an organization",
					attr.SlogOrganizationID(organization.OrganizationID), attr.SlogError(err))
				result.Truncated = true
				continue
			}
			result.Messages += swept.Messages
			result.Instances += swept.Instances
			result.Truncated = result.Truncated || swept.Truncated
		}
	}
	result.Truncated = true
	result.Next = after
	return result, nil
}

// RunRetention performs one bounded retention pass from the first eligible
// organization, using the handler's own database, logger and sweep bounds.
func (h *Handler) RunRetention(ctx context.Context, now time.Time) (RetentionResult, error) {
	return RunRetention(ctx, h.db, h.logger, h.retention, now, "")
}

func sweepOrganization(ctx context.Context, db *pgxpool.Pool, queries *repo.Queries, retention Retention, organization repo.ListChannelsOrganizationsNeedingRetentionRow, now time.Time) (RetentionResult, error) {
	result := RetentionResult{Organizations: 1, Messages: 0, Instances: 0, Truncated: false, Next: ""}
	tx, err := db.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin channels retention: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := queries.WithTx(tx)

	messageCutoff := now.UTC().AddDate(0, 0, -int(organization.MessageRetentionDays))
	removed, err := q.SoftDeleteExpiredMessages(ctx, repo.SoftDeleteExpiredMessagesParams{
		OrganizationID: organization.OrganizationID,
		Cutoff:         conv.ToPGTimestamptz(messageCutoff),
		ResultLimit:    int32(retention.Batch), //nolint:gosec // bounded by DefaultRetention.
	})
	if err != nil {
		return result, fmt.Errorf("remove expired channels messages: %w", err)
	}
	result.Messages = len(removed)
	result.Truncated = len(removed) >= retention.Batch
	floor := int64(0)
	// The maintained per-conversation count is retention's responsibility as
	// much as the posting transaction's: a count that only ever goes up stops
	// describing the conversation the moment history is retired.
	perConversation := map[uuid.UUID]int64{}
	order := make([]uuid.UUID, 0, len(removed))
	for _, row := range removed {
		floor = max(floor, row.Seq)
		if _, seen := perConversation[row.ConversationID]; !seen {
			order = append(order, row.ConversationID)
		}
		perConversation[row.ConversationID]++
	}
	if len(order) > 0 {
		totals := make([]int64, 0, len(order))
		for _, id := range order {
			totals = append(totals, perConversation[id])
		}
		if err := q.DecrementConversationMessageCounts(ctx, repo.DecrementConversationMessageCountsParams{
			OrganizationID:  organization.OrganizationID,
			ConversationIds: order,
			Totals:          totals,
		}); err != nil {
			return result, fmt.Errorf("adjust channels conversation counts: %w", err)
		}
	}
	if floor > 0 {
		if _, err := q.RaiseRetentionFloor(ctx, repo.RaiseRetentionFloorParams{
			OrganizationID: organization.OrganizationID,
			FloorSeq:       floor,
		}); err != nil {
			return result, fmt.Errorf("raise channels retention floor: %w", err)
		}
	}

	instanceCutoff := now.UTC().AddDate(0, 0, -int(organization.InstanceRetentionDays))
	retired, err := q.SoftDeleteStaleInstances(ctx, repo.SoftDeleteStaleInstancesParams{
		OrganizationID: organization.OrganizationID,
		Cutoff:         conv.ToPGTimestamptz(instanceCutoff),
		ResultLimit:    int32(retention.Batch), //nolint:gosec // bounded by DefaultRetention.
	})
	if err != nil {
		return result, fmt.Errorf("retire stale channels instances: %w", err)
	}
	result.Instances = len(retired)
	result.Truncated = result.Truncated || len(retired) >= retention.Batch

	// Retention changes what a reader can still fetch and who is still listed,
	// so subscribers reconcile instead of waiting for their next interval. The
	// hint is published inside this transaction, so it is released only if the
	// removal actually commits.
	if result.Messages > 0 {
		if err := publishHint(ctx, q, organization.OrganizationID, hintMessages, floor); err != nil {
			return result, err
		}
	}
	if result.Instances > 0 {
		if err := publishHint(ctx, q, organization.OrganizationID, hintPresence, 0); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit channels retention: %w", err)
	}
	return result, nil
}
