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

// setupTaskCompletion says how a task's effective status reaches done.
type setupTaskCompletion string

const (
	// setupTaskCompletionManual: an admin marks the task done.
	setupTaskCompletionManual setupTaskCompletion = "manual"
	// setupTaskCompletionFact: organization facts force the task done.
	setupTaskCompletionFact setupTaskCompletion = "fact"
	// setupTaskCompletionChildren: a group is done once every visible card
	// under it is done. Groups are never marked by hand.
	setupTaskCompletionChildren setupTaskCompletion = "children"
)

type setupTaskDefinition struct {
	Key           string
	Title         string
	Description   string
	Prerequisites []string
	// HiddenByDefault preserves legacy selection until staff explicitly save
	// visibility. New tasks must not expand untouched organizations' boards.
	HiddenByDefault bool

	// Parent is the setupTaskGroups key this card sits under, or empty for a
	// top-level card. Cards of one group are contiguous in the catalog.
	Parent string

	// Methods are the support matrix integration method slugs the card
	// configures. A card with none, such as identity, applies to every stack.
	Methods []string

	// Completion is manual or fact; groups derive theirs from their cards.
	Completion setupTaskCompletion
}

// setupTaskGroup nests cards one level. A group has no card of its own: it is
// hidden when every card under it is, done when every visible card is, and
// it cannot be assigned or marked by hand.
type setupTaskGroup struct {
	Key         string
	Title       string
	Description string
}

// setupTaskGroups lists the groups cards may name as Parent. A group appears
// in the wizard where its first card does.
var setupTaskGroups = []setupTaskGroup{
	{Key: "agent-observability", Title: "Set up agent observability", Description: "Connect coding agents to Speakeasy hook telemetry and confirm traffic arrives."},
	{Key: "mcp-distribution", Title: "Distribute MCP servers", Description: "Publish the plugin marketplace and distribute approved MCP servers through it."},
}

// setupTaskCatalog lists every setup card in wizard order. To add a card:
//  1. Add an entry here, under a group if it belongs to one and next to that
//     group's other cards. Only an optional card is HiddenByDefault.
//  2. Add its content to SETUP_CARDS in client/dashboard/src/pages/setup/setup-cards.tsx.
//  3. Add it to the playbooks that should walk it, from the admin dashboard.
//  4. Optionally mark it done from organization facts in projectSetupTasks.
//
// Tests on both sides fail if the catalog, SETUP_CARDS, groups, and presets
// disagree. The catalog is mirrored into onboarding_steps at start-up.
var setupTaskCatalog = []setupTaskDefinition{
	// Identity
	{Key: "identity-provider", Title: "Set up identity provider", Description: "Verify a domain, connect single sign-on, and sync people and groups from the identity provider.", Prerequisites: nil, HiddenByDefault: false, Parent: "", Methods: nil, Completion: setupTaskCompletionFact},
	// Observe
	{Key: "enable-logging", Title: "Enable logging", Description: "Record tool calls, I/O, and agent sessions.", Prerequisites: nil, HiddenByDefault: false, Parent: "", Methods: nil, Completion: setupTaskCompletionFact},
	{Key: "anthropic-observability", Title: "Set up Anthropic observability", Description: "Turn on Anthropic inference hooks in Claude.ai so Claude conversations reach Speakeasy, and confirm traffic arrives.", Prerequisites: nil, HiddenByDefault: false, Parent: "", Methods: []string{"inference"}, Completion: setupTaskCompletionManual},
	{Key: "instrument-agents", Title: "Set up observability in other platforms", Description: "Connect Cursor, Codex, and other coding agents to Speakeasy hook telemetry and confirm traffic arrives.", Prerequisites: nil, HiddenByDefault: false, Parent: "agent-observability", Methods: nil, Completion: setupTaskCompletionManual},
	{Key: "confirm-traffic", Title: "Confirm traffic", Description: "Verify that instrumented agents are sending hook events.", Prerequisites: []string{"instrument-agents"}, HiddenByDefault: false, Parent: "agent-observability", Methods: nil, Completion: setupTaskCompletionManual},
	{Key: "litellm", Title: "Set up LiteLLM", Description: "Point a LiteLLM proxy at Speakeasy so its traffic is scanned by risk policies and lands in observability, and confirm traffic arrives.", Prerequisites: nil, HiddenByDefault: true, Parent: "", Methods: []string{"litellm"}, Completion: setupTaskCompletionManual},
	{Key: "additional-agent-config", Title: "Configure integrations", Description: "Add optional provider integrations for agent activity.", Prerequisites: nil, HiddenByDefault: false, Parent: "", Methods: []string{"anthropic-api", "cursor-api", "openai-api", "conversations"}, Completion: setupTaskCompletionManual},
	// Distribute
	{Key: "create-marketplace", Title: "Create marketplace", Description: "Publish the organization's default project marketplace.", Prerequisites: nil, HiddenByDefault: false, Parent: "mcp-distribution", Methods: []string{"plugins"}, Completion: setupTaskCompletionFact},
	{Key: "distribute-servers", Title: "Distribute MCP servers", Description: "Distribute approved MCP servers through the plugin marketplace.", Prerequisites: []string{"create-marketplace"}, HiddenByDefault: false, Parent: "mcp-distribution", Methods: []string{"plugins"}, Completion: setupTaskCompletionManual},
	{Key: "platform-mcp", Title: "Set up Platform MCP", Description: "Connect Platform MCP and distribute its catalog.", Prerequisites: nil, HiddenByDefault: false, Parent: "", Methods: nil, Completion: setupTaskCompletionManual},
	// Secure
	{Key: "anthropic-admin-controls", Title: "Set up Anthropic admin controls", Description: "Publish the plugin marketplace, connect Claude Code and Claude Cowork through Claude.ai, and confirm traffic arrives.", Prerequisites: nil, HiddenByDefault: false, Parent: "", Methods: []string{"settings", "plugins"}, Completion: setupTaskCompletionManual},
	{Key: "configure-policies", Title: "Configure policies", Description: "Choose the organization's initial risk policies.", Prerequisites: nil, HiddenByDefault: false, Parent: "", Methods: nil, Completion: setupTaskCompletionManual},
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

	repo := orgrepo.New(s.db)
	tasks, err := projectSetupTasks(ctx, repo, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "project setup tasks").LogError(ctx, s.logger)
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
	beforeTasks, err := projectSetupTasks(ctx, repo, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "project setup tasks before update").LogError(ctx, s.logger)
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
	} else if before.Hidden {
		stored.HiddenAt = pgtype.Timestamptz{Time: time.Now().UTC(), InfinityModifier: pgtype.Finite, Valid: true}
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

	afterTasks, err := projectSetupTasks(ctx, repo, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "project setup tasks after update").LogError(ctx, s.logger)
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
			s.sendSetupTaskAssignmentEmail(emailCtx, ac, organization, after, updated.UpdatedAt.Time)
		}()
	}

	return after, nil
}

func (s *Service) sendSetupTaskAssignmentEmail(ctx context.Context, ac *contextvalues.AuthContext, organization orgrepo.OrganizationMetadatum, task *gen.SetupTask, assignmentTime time.Time) {
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
	setupLink := fmt.Sprintf("%s/%s/setup?task=%s", strings.TrimRight(s.orgHosts.SiteURL(organization.DefaultHost).String(), "/"), organization.Slug, task.Key)
	idempotencyMaterial := fmt.Sprintf("%s\x00%s\x00%s\x00%s", ac.ActiveOrganizationID, task.Key, assignmentTime.UTC().Format(time.RFC3339Nano), recipient)
	idempotencyKey := fmt.Sprintf("setup-task-assignment:%x", sha256.Sum256([]byte(idempotencyMaterial)))
	tmpl := email.SetupTaskAssignment{
		AssignerName:     assignerName,
		OrganizationName: organization.Name,
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

func projectSetupTasks(ctx context.Context, repo *orgrepo.Queries, organizationID string) ([]*gen.SetupTask, error) {
	rows, err := repo.ListOrganizationSetupTasks(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list setup task state")
	}
	members, err := repo.ListOrganizationUsers(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list setup task assignees")
	}
	facts, err := repo.GetSetupTaskCompletionFacts(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get setup task completion facts")
	}

	stateByKey := make(map[string]orgrepo.OrganizationSetupTask, len(rows))
	for _, row := range rows {
		stateByKey[row.TaskKey] = row
	}
	// An assigned playbook decides what the wizard walks and in what order;
	// without one, the saved visibility and the catalog defaults do.
	playbookOrder, followPlaybook, err := assignedPlaybookOrder(ctx, repo, organizationID)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]bool, len(playbookOrder))
	for _, key := range playbookOrder {
		selected[key] = true
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
		// Persisted visibility overrides the catalog default in both directions.
		hidden := definition.HiddenByDefault
		var assignee *gen.SetupTaskAssignee
		if persisted {
			status = state.Status
			hidden = state.HiddenAt.Valid
			assignee = setupTaskAssigneeView(state, membersByID, membersByEmail)
		}
		if followPlaybook {
			hidden = !selected[definition.Key]
		}
		completedByFact := definition.Completion == setupTaskCompletionFact && setupTaskFact(definition.Key, facts)
		if completedByFact {
			status = setupTaskStatusDone
		}
		tasks = append(tasks, &gen.SetupTask{Key: definition.Key, Title: definition.Title, Description: definition.Description, Status: status, CompletedByFact: completedByFact, Assignee: assignee, BlockedBy: []string{}, Hidden: hidden, ParentKey: conv.PtrEmpty(definition.Parent), Group: false})
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

	if followPlaybook {
		tasks = inPlaybookOrder(tasks, playbookOrder)
	}
	return withSetupTaskGroups(tasks), nil
}

// assignedPlaybookOrder returns the cards of the organization's playbook in
// walking order, and whether a playbook is assigned at all.
func assignedPlaybookOrder(ctx context.Context, repo *orgrepo.Queries, organizationID string) ([]string, bool, error) {
	assignment, err := repo.GetOrganizationOnboardingPlaybookID(ctx, organizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.E(oops.CodeUnexpected, err, "load onboarding playbook assignment")
	}
	if !assignment.PlaybookID.Valid {
		return nil, false, nil
	}
	topLevel, err := repo.ListOrganizationOnboardingPlaybookSteps(ctx, organizationID)
	if err != nil {
		return nil, false, oops.E(oops.CodeUnexpected, err, "list onboarding playbook steps")
	}
	if len(topLevel) == 0 {
		// The playbook was retired: back to the saved selection.
		return nil, false, nil
	}
	return playbookCards(topLevel), true, nil
}

// inPlaybookOrder puts the playbook's cards first, in its order, and the rest
// after in catalog order.
func inPlaybookOrder(cards []*gen.SetupTask, order []string) []*gen.SetupTask {
	sorted := make([]*gen.SetupTask, 0, len(cards))
	for _, key := range order {
		if card := setupTaskByKey(cards, key); card != nil {
			sorted = append(sorted, card)
		}
	}
	for _, card := range cards {
		if !slices.Contains(order, card.Key) {
			sorted = append(sorted, card)
		}
	}
	return sorted
}

// setupTaskFact reports whether organization facts complete a fact-completed
// card.
func setupTaskFact(key string, facts orgrepo.GetSetupTaskCompletionFactsRow) bool {
	switch key {
	case "identity-provider":
		// The identity provider card covers domain verification, single
		// sign-on and directory sync, so it only completes by fact once single
		// sign-on and directory sync are configured; an admin who skips
		// directory sync marks the card done by hand.
		return facts.SsoConfigured && facts.DsyncConfigured
	case "create-marketplace":
		return facts.MarketplacePublished
	case "enable-logging":
		return facts.LoggingEnabled
	default:
		return false
	}
}

// withSetupTaskGroups places each group ahead of its first card and derives
// the group's state from its cards: hidden when every card is, done when
// every visible card is, and complete by fact only when every visible card is.
func withSetupTaskGroups(cards []*gen.SetupTask) []*gen.SetupTask {
	tasks := make([]*gen.SetupTask, 0, len(cards)+len(setupTaskGroups))
	placed := make(map[string]bool, len(setupTaskGroups))
	for _, card := range cards {
		parent := conv.PtrValOr(card.ParentKey, "")
		if parent != "" && !placed[parent] {
			placed[parent] = true
			tasks = append(tasks, setupTaskGroupView(parent, cards))
		}
		tasks = append(tasks, card)
	}
	return tasks
}

func setupTaskGroupView(key string, cards []*gen.SetupTask) *gen.SetupTask {
	group := setupTaskGroupForKey(key)
	hidden, done, byFact := true, true, true
	for _, card := range cards {
		if conv.PtrValOr(card.ParentKey, "") != key || card.Hidden {
			continue
		}
		hidden = false
		if card.Status != setupTaskStatusDone {
			done = false
		}
		if !card.CompletedByFact {
			byFact = false
		}
	}
	status := setupTaskStatusTodo
	if !hidden && done {
		status = setupTaskStatusDone
	}
	return &gen.SetupTask{Key: group.Key, Title: group.Title, Description: group.Description, Status: status, CompletedByFact: !hidden && byFact, Assignee: nil, BlockedBy: []string{}, Hidden: hidden, ParentKey: nil, Group: true}
}

func setupTaskGroupForKey(key string) *setupTaskGroup {
	for index := range setupTaskGroups {
		if setupTaskGroups[index].Key == key {
			return &setupTaskGroups[index]
		}
	}
	return nil
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

// SubmitOnboardingSurvey assigns the default playbook of the survey's use
// case, checked against the recorded stack like any assignment. Callers
// never pick steps.
func (s *Service) SubmitOnboardingSurvey(ctx context.Context, payload *gen.SubmitOnboardingSurveyPayload) (*gen.ListSetupTasksResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	queries := orgrepo.New(s.db)
	useCase, err := queries.GetOnboardingUseCaseBySlug(ctx, payload.UseCase)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeBadRequest, nil, "unknown onboarding use case").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load onboarding use case").LogError(ctx, s.logger)
	}
	playbook, err := queries.GetOnboardingDefaultPlaybook(ctx, conv.ToNullUUID(useCase.ID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeBadRequest, nil, "use case %q has no default playbook yet", payload.UseCase).LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load default onboarding playbook").LogError(ctx, s.logger)
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID)
	if _, err := AssignOrganizationOnboardingPlaybook(ctx, s.db, s.audit, ac.ActiveOrganizationID, &playbook.ID, actor, ac.Email); err != nil {
		return nil, fmt.Errorf("save onboarding survey result: %w", err)
	}
	return s.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{IncludeHidden: nil, SessionToken: payload.SessionToken})
}
