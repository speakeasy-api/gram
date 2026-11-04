// Package chatplan lists chat sessions through an authorization access plan:
// the visibility predicate is compiled from the registry's chat permission and
// applied in SQL, before pagination and counting, in one database snapshot.
package chatplan

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// Adapter maps the chat type onto chats rows aliased c. An agent is joined in
// the chat's own organization, so a stamped agent ID from another tenant
// never resolves.
var Adapter = authz.PlanAdapter{
	Name: "chats.v1",
	Table: authz.PlanTable{
		ID: "c.id::text",
		Facts: map[string]authz.PlanColumn{
			"internal_user_id": {SQL: "c.user_id", Nullable: true},
			"external_user_id": {SQL: "c.external_user_id", Nullable: true},
		},
		Relations: map[string]authz.PlanRelation{
			"agent": {
				From: "agents a",
				Join: "a.id = c.agent_id AND a.organization_id = c.organization_id",
				Target: authz.PlanTable{
					ID: "a.id::text",
					Facts: map[string]authz.PlanColumn{
						"owner":                          {SQL: "a.owner_user_id", Nullable: false},
						"deleted":                        {SQL: "a.deleted", Nullable: false},
						"owner_reassignment_required_at": {SQL: "a.owner_reassignment_required_at", Nullable: true},
					},
					Relations: nil,
				},
			},
		},
	},
}

// ErrSnapshotIsolation reports a transaction that cannot hold one snapshot
// across statements.
var ErrSnapshotIsolation = errors.New("chat listing needs a repeatable read or serializable transaction")

// Bind ties plan to the snapshot of tx. tx must be repeatable read or
// serializable, so every statement in it reads the same snapshot.
func Bind(ctx context.Context, tx pgx.Tx, plan authz.AccessPlan) (authz.AccessPlan, error) {
	token, err := snapshot(ctx, tx)
	if err != nil {
		return authz.AccessPlan{}, err
	}
	bound, err := plan.BindSnapshot(token)
	if err != nil {
		return authz.AccessPlan{}, fmt.Errorf("bind chat plan: %w", err)
	}
	return bound, nil
}

// Row is one visible chat session.
type Row struct {
	ID             uuid.UUID
	UserID         *string
	ExternalUserID *string
	AgentID        uuid.NullUUID
}

// Page is a page of visible chats and the number of visible chats in all.
type Page struct {
	Rows  []Row
	Total int64
}

// ListVisible returns one page of the chats plan admits in its project,
// newest first. The page and total come from the plan's snapshot: the total
// is counted with the page when the page has rows, and by a second statement
// in the same snapshot when the page is past the end.
func ListVisible(ctx context.Context, tx pgx.Tx, plan authz.AccessPlan, limit, offset int32) (Page, error) {
	where, args, err := visible(ctx, tx, plan)
	if err != nil {
		return Page{}, err
	}
	n := len(args)
	query := `SELECT c.id, c.user_id, c.external_user_id, c.agent_id, COUNT(*) OVER ()
FROM chats c
WHERE ` + where + `
ORDER BY c.created_at DESC, c.id DESC
LIMIT $` + fmt.Sprint(n+1) + ` OFFSET $` + fmt.Sprint(n+2)
	rows, err := tx.Query(ctx, query, append(args, limit, offset)...)
	if err != nil {
		return Page{}, fmt.Errorf("list visible chats: %w", err)
	}
	defer rows.Close()

	page := Page{Rows: []Row{}, Total: 0}
	for rows.Next() {
		var row Row
		if err := rows.Scan(&row.ID, &row.UserID, &row.ExternalUserID, &row.AgentID, &page.Total); err != nil {
			return Page{}, fmt.Errorf("scan visible chat: %w", err)
		}
		page.Rows = append(page.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("list visible chats: %w", err)
	}
	if len(page.Rows) > 0 || offset == 0 {
		return page, nil
	}
	page.Total, err = CountVisible(ctx, tx, plan)
	if err != nil {
		return Page{}, err
	}
	return page, nil
}

// CountVisible counts the chats plan admits in its project.
func CountVisible(ctx context.Context, tx pgx.Tx, plan authz.AccessPlan) (int64, error) {
	where, args, err := visible(ctx, tx, plan)
	if err != nil {
		return 0, err
	}
	var total int64
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM chats c WHERE "+where, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count visible chats: %w", err)
	}
	return total, nil
}

// visible renders the filter every listing shares: the plan's tenant, live
// rows, and the plan's predicate. It refuses a plan for another adapter or
// another snapshot.
func visible(ctx context.Context, tx pgx.Tx, plan authz.AccessPlan) (string, []any, error) {
	binding := plan.Binding()
	if binding.Adapter != Adapter.Name {
		return "", nil, fmt.Errorf("%w: plan was compiled for adapter %q", authz.ErrPlanStale, binding.Adapter)
	}
	projectID, err := uuid.Parse(binding.ProjectID)
	if err != nil || binding.OrganizationID == "" {
		return "", nil, fmt.Errorf("list visible chats: %w", authz.ErrPlanTenant)
	}
	token, err := snapshot(ctx, tx)
	if err != nil {
		return "", nil, err
	}
	if token != binding.Snapshot {
		return "", nil, fmt.Errorf("%w: plan is bound to another snapshot", authz.ErrPlanStale)
	}
	predicate, args, err := plan.Predicate(3)
	if err != nil {
		return "", nil, fmt.Errorf("render chat plan: %w", err)
	}
	where := "c.organization_id = $1 AND c.project_id = $2 AND c.deleted IS FALSE AND " + predicate
	return where, append([]any{binding.OrganizationID, projectID}, args...), nil
}

func snapshot(ctx context.Context, tx pgx.Tx) (string, error) {
	var isolation, token string
	err := tx.QueryRow(ctx, "SELECT current_setting('transaction_isolation'), pg_current_snapshot()::text").Scan(&isolation, &token)
	if err != nil {
		return "", fmt.Errorf("read chat listing snapshot: %w", err)
	}
	if isolation != "repeatable read" && isolation != "serializable" {
		return "", fmt.Errorf("%w: transaction is %s", ErrSnapshotIsolation, isolation)
	}
	return token, nil
}
