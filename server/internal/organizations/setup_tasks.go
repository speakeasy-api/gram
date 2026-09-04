package organizations

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	setupTaskStatusTodo            = "todo"
	setupTaskStatusInProgress      = "in_progress"
	setupTaskStatusAwaitingSupport = "awaiting_support"
	setupTaskStatusDone            = "done"
)

type setupTaskDefinition struct {
	Key           string
	Title         string
	Description   string
	Prerequisites []string
}

var setupTaskCatalog = []setupTaskDefinition{
	{Key: "connect-idp", Title: "Connect identity provider", Description: "Configure single sign-on for the organization.", Prerequisites: nil},
	{Key: "directory-sync", Title: "Set up directory sync", Description: "Sync people and groups from the identity provider.", Prerequisites: nil},
	{Key: "create-marketplace", Title: "Create marketplace", Description: "Publish the organization's default project marketplace.", Prerequisites: nil},
	{Key: "instrument-agents", Title: "Instrument agents", Description: "Connect coding agents to Gram hook telemetry.", Prerequisites: nil},
	{Key: "additional-agent-config", Title: "Configure integrations", Description: "Add optional provider integrations for agent activity.", Prerequisites: nil},
	{Key: "confirm-traffic", Title: "Confirm traffic", Description: "Verify that instrumented agents are sending hook events.", Prerequisites: []string{"instrument-agents"}},
	{Key: "distribute-servers", Title: "Distribute MCP servers", Description: "Install and publish approved MCP servers.", Prerequisites: []string{"create-marketplace"}},
	{Key: "configure-policies", Title: "Configure policies", Description: "Choose the organization's initial risk policies.", Prerequisites: nil},
	{Key: "platform-mcp", Title: "Set up Platform MCP", Description: "Connect Platform MCP and distribute its catalog.", Prerequisites: nil},
}

var validSetupTaskStatuses = []string{
	setupTaskStatusTodo,
	setupTaskStatusInProgress,
	setupTaskStatusAwaitingSupport,
	setupTaskStatusDone,
}

func (s *Service) ListSetupTasks(ctx context.Context, payload *gen.ListSetupTasksPayload) (*gen.ListSetupTasksResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	tasks, err := s.projectSetupTasks(ctx, orgrepo.New(s.db), ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	includeHidden := payload.IncludeHidden != nil && *payload.IncludeHidden && ac.IsAdmin
	if !includeHidden {
		tasks = slices.DeleteFunc(tasks, func(task *gen.SetupTask) bool { return task.Hidden })
	}

	return &gen.ListSetupTasksResult{Tasks: tasks}, nil
}

func (s *Service) UpdateSetupTask(ctx context.Context, payload *gen.UpdateSetupTaskPayload) (*gen.SetupTask, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	if setupTaskDefinitionForKey(payload.TaskKey) == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown setup task").LogError(ctx, s.logger)
	}
	if payload.Status != nil && !slices.Contains(validSetupTaskStatuses, *payload.Status) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid setup task status").LogError(ctx, s.logger)
	}
	clearAssignee := payload.ClearAssignee != nil && *payload.ClearAssignee
	if payload.Assignee == nil && payload.Status == nil && payload.Hidden == nil && !clearAssignee {
		return nil, oops.E(oops.CodeBadRequest, nil, "setup task update is empty").LogError(ctx, s.logger)
	}
	if payload.Assignee != nil && clearAssignee {
		return nil, oops.E(oops.CodeBadRequest, nil, "assignee and clear_assignee cannot both be set").LogError(ctx, s.logger)
	}

	adminErr := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil})
	if (payload.Assignee != nil || clearAssignee) && adminErr != nil {
		return nil, adminErr
	}
	if payload.Hidden != nil {
		if _, _, err := auth.RequirePlatformAdmin(ctx, s.logger); err != nil {
			return nil, err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin setup task update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	repo := orgrepo.New(tx)

	organization, err := repo.LockOrganizationForSetupTaskUpdate(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock organization setup tasks").LogError(ctx, s.logger)
	}
	beforeTasks, err := s.projectSetupTasks(ctx, repo, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	before := setupTaskByKey(beforeTasks, payload.TaskKey)
	if before == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown setup task").LogError(ctx, s.logger)
	}

	if payload.Status != nil && adminErr != nil && !setupTaskAssignedTo(before, ac) {
		return nil, adminErr
	}
	if payload.Status != nil && *payload.Status != setupTaskStatusTodo && len(before.BlockedBy) > 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "blocked setup tasks must remain todo").LogError(ctx, s.logger)
	}

	emptyText := pgtype.Text{String: "", Valid: false}
	emptyTime := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	stored := orgrepo.OrganizationSetupTask{
		OrganizationID: ac.ActiveOrganizationID,
		TaskKey:        payload.TaskKey,
		Status:         setupTaskStatusTodo,
		AssigneeUserID: emptyText,
		AssigneeEmail:  emptyText,
		HiddenAt:       emptyTime,
		CreatedAt:      emptyTime,
		UpdatedAt:      emptyTime,
	}
	row, err := repo.GetOrganizationSetupTask(ctx, orgrepo.GetOrganizationSetupTaskParams{OrganizationID: ac.ActiveOrganizationID, TaskKey: payload.TaskKey})
	if err == nil {
		stored = row
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "get setup task state").LogError(ctx, s.logger)
	}

	if payload.Status != nil {
		stored.Status = *payload.Status
	}
	if payload.Assignee != nil {
		userID, email, err := validateSetupTaskAssignee(ctx, repo, ac.ActiveOrganizationID, payload.Assignee)
		if err != nil {
			return nil, err
		}
		stored.AssigneeUserID = userID
		stored.AssigneeEmail = email
		if payload.Status == nil && before.Status == setupTaskStatusTodo && len(before.BlockedBy) == 0 {
			stored.Status = setupTaskStatusInProgress
		}
	} else if clearAssignee {
		stored.AssigneeUserID = emptyText
		stored.AssigneeEmail = emptyText
	}
	if payload.Hidden != nil {
		if *payload.Hidden {
			stored.HiddenAt = pgtype.Timestamptz{Time: time.Now().UTC(), InfinityModifier: pgtype.Finite, Valid: true}
		} else {
			stored.HiddenAt = emptyTime
		}
	}

	updated, err := repo.UpsertOrganizationSetupTask(ctx, orgrepo.UpsertOrganizationSetupTaskParams{
		OrganizationID: stored.OrganizationID,
		TaskKey:        stored.TaskKey,
		Status:         stored.Status,
		AssigneeUserID: stored.AssigneeUserID,
		AssigneeEmail:  stored.AssigneeEmail,
		HiddenAt:       stored.HiddenAt,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update setup task").LogError(ctx, s.logger)
	}

	afterTasks, err := s.projectSetupTasks(ctx, repo, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	after := setupTaskByKey(afterTasks, payload.TaskKey)
	if err := s.audit.LogOrganizationSetupTaskUpdated(ctx, tx, audit.LogOrganizationSetupTaskUpdatedEvent{
		OrganizationID: ac.ActiveOrganizationID,
		Actor:          urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), ActorDisplayName: ac.Email, ActorSlug: nil,
		OrganizationName: organization.Name, OrganizationSlug: organization.Slug, TaskKey: payload.TaskKey,
		SetupTaskSnapshotBefore: setupTaskAuditSnapshot(before), SetupTaskSnapshotAfter: setupTaskAuditSnapshot(after),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log setup task update").LogError(ctx, s.logger)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit setup task update").LogError(ctx, s.logger)
	}

	if payload.Assignee != nil && !sameSetupTaskAssignee(before.Assignee, after.Assignee) {
		detached := context.WithoutCancel(ctx)
		go func() {
			emailCtx, cancel := context.WithTimeout(detached, 10*time.Second)
			defer cancel()
			s.sendSetupTaskAssignmentEmail(emailCtx, ac, organization.Name, organization.Slug, after, updated.UpdatedAt.Time)
		}()
	}

	return after, nil
}

func (s *Service) sendSetupTaskAssignmentEmail(ctx context.Context, ac *contextvalues.AuthContext, organizationName, organizationSlug string, task *gen.SetupTask, assignmentTime time.Time) {
	if s.email == nil || task == nil || task.Assignee == nil || strings.TrimSpace(task.Assignee.Email) == "" {
		return
	}

	assignerName := strings.TrimSpace(conv.PtrValOr(ac.Email, ac.UserID))
	if user, err := userrepo.New(s.db).GetUser(ctx, ac.UserID); err == nil {
		if displayName := strings.TrimSpace(user.DisplayName); displayName != "" {
			assignerName = displayName
		} else if userEmail := strings.TrimSpace(user.Email); userEmail != "" {
			assignerName = userEmail
		}
	}

	recipient := conv.NormalizeEmail(task.Assignee.Email)
	setupLink := fmt.Sprintf("%s/%s/setup?step=%s", strings.TrimRight(s.siteURL, "/"), organizationSlug, task.Key)
	idempotencyMaterial := fmt.Sprintf("%s\x00%s\x00%s\x00%s", ac.ActiveOrganizationID, task.Key, assignmentTime.UTC().Format(time.RFC3339Nano), recipient)
	idempotencyKey := fmt.Sprintf("setup-task-assignment:%x", sha256.Sum256([]byte(idempotencyMaterial)))
	tmpl := email.SetupTaskAssignment{
		AssignerName:     assignerName,
		OrganizationName: organizationName,
		TaskTitle:        task.Title,
		TaskDescription:  task.Description,
		SetupLink:        setupLink,
	}
	if err := s.email.SendIdempotent(ctx, recipient, idempotencyKey, tmpl); err != nil {
		s.logger.ErrorContext(ctx, "failed to send setup task assignment email", attr.SlogError(err), attr.SlogOrganizationID(ac.ActiveOrganizationID))
	}
}

func sameSetupTaskAssignee(before, after *gen.SetupTaskAssignee) bool {
	if before == nil || after == nil {
		return before == nil && after == nil
	}
	if before.UserID != nil && after.UserID != nil {
		return *before.UserID == *after.UserID
	}
	return conv.NormalizeEmail(before.Email) == conv.NormalizeEmail(after.Email)
}

func (s *Service) projectSetupTasks(ctx context.Context, repo *orgrepo.Queries, organizationID string) ([]*gen.SetupTask, error) {
	rows, err := repo.ListOrganizationSetupTasks(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list setup task state").LogError(ctx, s.logger)
	}
	members, err := repo.ListOrganizationUsers(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list setup task assignees").LogError(ctx, s.logger)
	}
	facts, err := repo.GetSetupTaskCompletionFacts(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get setup task completion facts").LogError(ctx, s.logger)
	}

	stateByKey := make(map[string]orgrepo.OrganizationSetupTask, len(rows))
	for _, row := range rows {
		stateByKey[row.TaskKey] = row
	}
	membersByID := make(map[string]orgrepo.ListOrganizationUsersRow, len(members))
	membersByEmail := make(map[string]orgrepo.ListOrganizationUsersRow, len(members))
	slices.SortFunc(members, func(a, b orgrepo.ListOrganizationUsersRow) int {
		if result := cmp.Compare(conv.NormalizeEmail(a.UserEmail), conv.NormalizeEmail(b.UserEmail)); result != 0 {
			return result
		}
		aNormalized := a.UserEmail == conv.NormalizeEmail(a.UserEmail)
		bNormalized := b.UserEmail == conv.NormalizeEmail(b.UserEmail)
		if aNormalized != bNormalized {
			if aNormalized {
				return -1
			}
			return 1
		}
		if result := a.CreatedAt.Time.Compare(b.CreatedAt.Time); result != 0 {
			return result
		}
		return cmp.Compare(a.UserID.String, b.UserID.String)
	})
	for _, member := range members {
		if member.UserID.Valid {
			membersByID[member.UserID.String] = member
		}
		normalizedEmail := conv.NormalizeEmail(member.UserEmail)
		if _, exists := membersByEmail[normalizedEmail]; !exists {
			membersByEmail[normalizedEmail] = member
		}
	}

	tasks := make([]*gen.SetupTask, 0, len(setupTaskCatalog))
	for _, definition := range setupTaskCatalog {
		state, persisted := stateByKey[definition.Key]
		status := setupTaskStatusTodo
		hidden := false
		var assignee *gen.SetupTaskAssignee
		if persisted {
			status = state.Status
			hidden = state.HiddenAt.Valid
			assignee = setupTaskAssigneeView(state, membersByID, membersByEmail)
		}
		completedByFact := (definition.Key == "connect-idp" && facts.SsoConfigured) ||
			(definition.Key == "directory-sync" && facts.DsyncConfigured) ||
			(definition.Key == "create-marketplace" && facts.MarketplacePublished)
		if completedByFact {
			status = setupTaskStatusDone
		}
		tasks = append(tasks, &gen.SetupTask{Key: definition.Key, Title: definition.Title, Description: definition.Description, Status: status, CompletedByFact: completedByFact, Assignee: assignee, BlockedBy: []string{}, Hidden: hidden})
	}

	for index, definition := range setupTaskCatalog {
		for _, prerequisite := range definition.Prerequisites {
			prerequisiteTask := setupTaskByKey(tasks, prerequisite)
			if prerequisiteTask != nil && !prerequisiteTask.Hidden && prerequisiteTask.Status != setupTaskStatusDone {
				tasks[index].BlockedBy = append(tasks[index].BlockedBy, prerequisite)
				tasks[index].Status = setupTaskStatusTodo
			}
		}
	}

	return tasks, nil
}

func setupTaskDefinitionForKey(key string) *setupTaskDefinition {
	for index := range setupTaskCatalog {
		if setupTaskCatalog[index].Key == key {
			return &setupTaskCatalog[index]
		}
	}
	return nil
}

func setupTaskByKey(tasks []*gen.SetupTask, key string) *gen.SetupTask {
	for _, task := range tasks {
		if task.Key == key {
			return task
		}
	}
	return nil
}

func setupTaskAssigneeView(state orgrepo.OrganizationSetupTask, membersByID, membersByEmail map[string]orgrepo.ListOrganizationUsersRow) *gen.SetupTaskAssignee {
	var member orgrepo.ListOrganizationUsersRow
	var found bool
	if state.AssigneeUserID.Valid {
		member, found = membersByID[state.AssigneeUserID.String]
	} else if state.AssigneeEmail.Valid {
		member, found = membersByEmail[conv.NormalizeEmail(state.AssigneeEmail.String)]
	}
	if found {
		return &gen.SetupTaskAssignee{UserID: conv.FromPGText[string](member.UserID), Email: member.UserEmail, Name: conv.PtrEmpty(member.UserDisplayName), PhotoURL: conv.FromPGText[string](member.UserPhotoUrl)}
	}
	if state.AssigneeEmail.Valid {
		return &gen.SetupTaskAssignee{UserID: nil, Email: state.AssigneeEmail.String, Name: nil, PhotoURL: nil}
	}
	return nil
}

func validateSetupTaskAssignee(ctx context.Context, repo *orgrepo.Queries, organizationID string, input *gen.SetupTaskAssigneeInput) (pgtype.Text, pgtype.Text, error) {
	emptyText := pgtype.Text{String: "", Valid: false}
	userID := strings.TrimSpace(conv.PtrValOr(input.UserID, ""))
	email := conv.NormalizeEmail(conv.PtrValOr(input.Email, ""))
	if (userID == "") == (email == "") {
		return emptyText, emptyText, oops.E(oops.CodeBadRequest, nil, "assignee must contain exactly one of user_id or email")
	}
	if userID != "" {
		active, err := repo.HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{UserID: userID, OrganizationID: organizationID})
		if err != nil {
			return emptyText, emptyText, oops.E(oops.CodeUnexpected, err, "validate setup task assignee")
		}
		if !active {
			return emptyText, emptyText, oops.E(oops.CodeBadRequest, nil, "assignee must be an active organization member")
		}
		return conv.ToPGText(userID), emptyText, nil
	}
	if _, ok := inviteEmailDomain(email); !ok {
		return emptyText, emptyText, oops.E(oops.CodeBadRequest, nil, "assignee email must be valid")
	}
	return emptyText, conv.ToPGText(email), nil
}

func setupTaskAssignedTo(task *gen.SetupTask, ac *contextvalues.AuthContext) bool {
	if task.Assignee == nil {
		return false
	}
	return (task.Assignee.UserID != nil && *task.Assignee.UserID == ac.UserID) || conv.NormalizeEmail(task.Assignee.Email) == conv.NormalizeEmail(conv.PtrValOr(ac.Email, ""))
}

func setupTaskAuditSnapshot(task *gen.SetupTask) *audit.OrganizationSetupTaskSnapshot {
	if task == nil {
		return nil
	}
	var assignee *audit.OrganizationSetupTaskAssigneeSnapshot
	if task.Assignee != nil {
		assignee = &audit.OrganizationSetupTaskAssigneeSnapshot{
			UserID: task.Assignee.UserID, Email: task.Assignee.Email,
			Name: task.Assignee.Name, PhotoURL: task.Assignee.PhotoURL,
		}
	}
	return &audit.OrganizationSetupTaskSnapshot{
		Key: task.Key, Title: task.Title, Description: task.Description, Status: task.Status,
		Assignee: assignee, BlockedBy: task.BlockedBy, Hidden: task.Hidden,
	}
}
