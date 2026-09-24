package organizations

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) AssignSetupWorkstream(ctx context.Context, payload *gen.AssignSetupWorkstreamPayload) (*gen.ListSetupTasksResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	workstream := setupWorkstreamForID(payload.Workstream)
	if workstream == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown setup workstream")
	}
	clearAssignee := payload.ClearAssignee != nil && *payload.ClearAssignee
	if (payload.Assignee == nil && !clearAssignee) || (payload.Assignee != nil && clearAssignee) {
		return nil, oops.E(oops.CodeBadRequest, nil, "provide exactly one of assignee or clear_assignee=true")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin setup workstream assignment")
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	repo := orgrepo.New(tx)
	organization, err := repo.LockOrganizationForSetupTaskUpdate(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock organization setup tasks")
	}
	beforeTasks, err := projectSetupTasks(ctx, repo, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	var userID, email pgtype.Text
	if payload.Assignee != nil {
		userID, email, err = validateSetupTaskAssignee(ctx, repo, ac.ActiveOrganizationID, payload.Assignee)
		if err != nil {
			return nil, err
		}
	}
	changedKeys := make([]string, 0, len(workstream.TaskKeys))
	var assignmentTime time.Time
	for _, key := range workstream.TaskKeys {
		before := setupTaskByKey(beforeTasks, key)
		if before == nil {
			return nil, oops.E(oops.CodeUnexpected, nil, "setup workstream contains unknown task")
		}
		stored, err := repo.GetOrganizationSetupTask(ctx, orgrepo.GetOrganizationSetupTaskParams{OrganizationID: ac.ActiveOrganizationID, TaskKey: key})
		if errors.Is(err, pgx.ErrNoRows) {
			stored = orgrepo.OrganizationSetupTask{
				OrganizationID: ac.ActiveOrganizationID, TaskKey: key, Status: setupTaskStatusTodo,
				AssigneeUserID: pgtype.Text{String: "", Valid: false}, AssigneeEmail: pgtype.Text{String: "", Valid: false},
				HiddenAt:  pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
				CreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
				UpdatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			}
			if before.Hidden {
				stored.HiddenAt = pgtype.Timestamptz{Time: time.Now().UTC(), InfinityModifier: pgtype.Finite, Valid: true}
			}
		} else if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "get setup task state")
		}
		// Compare the resolved identity, including email assignments that resolve to members.
		unchanged := !stored.AssigneeUserID.Valid && !stored.AssigneeEmail.Valid && clearAssignee
		if before.Assignee != nil && payload.Assignee != nil {
			unchanged = (userID.Valid && before.Assignee.UserID != nil && *before.Assignee.UserID == userID.String) || (email.Valid && conv.NormalizeEmail(before.Assignee.Email) == email.String)
		}
		if unchanged {
			continue
		}
		if payload.Assignee != nil && before.Status == setupTaskStatusTodo && len(before.BlockedBy) == 0 {
			stored.Status = setupTaskStatusInProgress
		}
		updated, err := repo.UpsertOrganizationSetupTask(ctx, orgrepo.UpsertOrganizationSetupTaskParams{
			OrganizationID: ac.ActiveOrganizationID, TaskKey: key, Status: stored.Status, AssigneeUserID: userID, AssigneeEmail: email, HiddenAt: stored.HiddenAt,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "assign setup workstream task")
		}
		assignmentTime = updated.UpdatedAt.Time
		changedKeys = append(changedKeys, key)
	}
	afterTasks, err := projectSetupTasks(ctx, repo, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	for _, key := range changedKeys {
		if err := s.audit.LogOrganizationSetupTaskUpdated(ctx, tx, audit.LogOrganizationSetupTaskUpdatedEvent{
			OrganizationID: ac.ActiveOrganizationID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), ActorDisplayName: ac.Email, ActorSlug: nil,
			OrganizationName: organization.Name, OrganizationSlug: organization.Slug, TaskKey: key,
			SetupTaskSnapshotBefore: setupTaskAuditSnapshot(setupTaskByKey(beforeTasks, key)), SetupTaskSnapshotAfter: setupTaskAuditSnapshot(setupTaskByKey(afterTasks, key)),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log setup workstream assignment")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit setup workstream assignment")
	}
	if payload.Assignee != nil && len(changedKeys) > 0 {
		notification := &gen.SetupTask{Key: "workstream:" + payload.Workstream, Title: workstream.Title, Description: workstream.Description, Assignee: setupTaskByKey(afterTasks, changedKeys[0]).Assignee, Status: "", CompletedByFact: false, BlockedBy: nil, Hidden: false}
		detached := context.WithoutCancel(ctx)
		go func() {
			emailCtx, cancel := context.WithTimeout(detached, 10*time.Second)
			defer cancel()
			s.sendSetupTaskAssignmentEmail(emailCtx, ac, organization.Name, organization.Slug, notification, assignmentTime)
		}()
	}
	// Match listSetupTasks' normal visibility. Hidden membership is updated, never disclosed.
	afterTasks = slices.DeleteFunc(afterTasks, func(task *gen.SetupTask) bool { return task.Hidden })
	return &gen.ListSetupTasksResult{Tasks: afterTasks, Workstreams: setupWorkstreamViewsForTasks(afterTasks)}, nil
}
