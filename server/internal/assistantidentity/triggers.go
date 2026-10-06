package assistantidentity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agentownership"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/conv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// Lock order shared by every writer: assistant row, then trigger rows in ID
// order, then the workload issuer row (the workload policy write lock).
// Trigger-side callers never lock the assistant; they only read its binding.

// triggerSubjectPrefix prefixes a root trigger's ID to form its workload
// subject, so each trigger is one exact workload under the project issuer.
const triggerSubjectPrefix = "assistant-trigger:"

// BindRootTrigger points a root trigger at its workload identity when its
// target assistant has a dedicated agent. Legacy assistants, other targets,
// continuation wakes, and already-bound triggers are left unchanged.
//
// A trigger created while its assistant is being upgraded in another
// transaction can miss both binding paths; repeating the upgrade binds it.
func (s *Service) BindRootTrigger(ctx context.Context, tx pgx.Tx, project, trigger uuid.UUID) error {
	return s.bindRoot(ctx, tx, project, trigger, false)
}

// RetargetRootTrigger runs after the caller changes the trigger target in the
// same transaction. It withdraws the previous workload and binds the new one.
func (s *Service) RetargetRootTrigger(ctx context.Context, tx pgx.Tx, project, trigger uuid.UUID) error {
	return s.bindRoot(ctx, tx, project, trigger, true)
}

func (s *Service) bindRoot(ctx context.Context, tx pgx.Tx, project, trigger uuid.UUID, retarget bool) error {
	q := repo.New(tx)
	root, err := triggerrepo.New(tx).GetTriggerInstanceByIDForUpdate(ctx, triggerrepo.GetTriggerInstanceByIDForUpdateParams{ID: trigger, ProjectID: project})
	if err != nil {
		return resourceError("lock root trigger", err)
	}
	if root.DefinitionSlug == bgtriggers.DefinitionSlugWake || (!retarget && root.TargetKind != bgtriggers.TargetKindAssistant) {
		return nil
	}
	_, err = q.GetTriggerBinding(ctx, repo.GetTriggerBindingParams{ProjectID: project, TriggerID: trigger})
	bound := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read root trigger binding: %w", err)
	}
	if bound && !retarget {
		return nil
	}
	if bound {
		if err := s.withdrawTrigger(ctx, tx, project, trigger); err != nil {
			return err
		}
	}
	if root.TargetKind != bgtriggers.TargetKindAssistant {
		return nil
	}
	assistant, err := uuid.Parse(root.TargetRef)
	if err != nil {
		return nil //nolint:nilerr // a non-UUID target is not an assistant this package can bind
	}
	binding, err := q.GetAssistantBinding(ctx, repo.GetAssistantBindingParams{ProjectID: project, AssistantID: assistant})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read root target assistant binding: %w", err)
	}

	issuer, err := s.projectIssuer(ctx, tx, root.OrganizationID, project)
	if err != nil {
		return err
	}
	if _, err := workloadrepo.New(tx).LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: root.OrganizationID, ID: issuer}); err != nil {
		return resourceError("lock assistant trigger issuer", err)
	}
	subject := triggerSubjectPrefix + trigger.String()
	projectID := uuid.NullUUID{UUID: project, Valid: true}
	admitted, err := workloadidentity.IsAdmitted(ctx, tx, workloadidentity.AdmissionParams{OrganizationID: root.OrganizationID, ProjectID: projectID, WorkloadIssuerID: issuer, Subject: subject})
	if err != nil {
		return fmt.Errorf("check assistant trigger admission: %w", err)
	}
	workloads := workloadrepo.New(tx)
	if !admitted {
		if _, err := workloads.CreateWorkloadAdmission(ctx, workloadrepo.CreateWorkloadAdmissionParams{
			OrganizationID: root.OrganizationID, ProjectID: projectID, WorkloadIssuerID: issuer, Subject: subject,
			MatchKind: string(workloadidentity.MatchKindExact), Name: conv.ToPGTextEmpty(""), Tags: []string{},
		}); err != nil {
			return fmt.Errorf("admit assistant trigger workload: %w", err)
		}
	}
	if _, err := workloads.UpsertWorkloadAgentAssignment(ctx, workloadrepo.UpsertWorkloadAgentAssignmentParams{
		OrganizationID: root.OrganizationID, WorkloadIssuerID: issuer, Subject: subject,
		MatchKind: string(workloadidentity.MatchKindExact), AgentID: binding.OriginalAgentID,
	}); err != nil {
		return fmt.Errorf("assign assistant trigger workload: %w", err)
	}
	if err := q.CreateTriggerBinding(ctx, repo.CreateTriggerBindingParams{
		OrganizationID: root.OrganizationID, ProjectID: project, TriggerID: trigger,
		AssistantBindingID: binding.ID, AssistantBindingGeneration: binding.Generation,
		WorkloadIssuerID: issuer, Subject: subject,
	}); err != nil {
		return fmt.Errorf("create root trigger binding: %w", err)
	}
	return nil
}

// projectIssuer returns the project's trust registration for the Gram issuer,
// creating one when none exists.
func (s *Service) projectIssuer(ctx context.Context, tx pgx.Tx, org string, project uuid.UUID) (uuid.UUID, error) {
	q := repo.New(tx)
	find := repo.FindProjectIssuerParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, Issuers: s.issuerSpellings}
	issuers, err := q.FindProjectIssuer(ctx, find)
	if err != nil {
		return uuid.Nil, fmt.Errorf("find assistant trigger issuer: %w", err)
	}
	if len(issuers) > 0 {
		return issuers[0], nil
	}
	id, err := q.CreateProjectIssuer(ctx, repo.CreateProjectIssuerParams{
		OrganizationID: org, ProjectID: find.ProjectID, Name: "Assistant triggers " + project.String(), Issuer: s.issuer, JwksUri: s.jwksURI,
	})
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("register assistant trigger issuer: %w", err)
	}
	issuers, err = q.FindProjectIssuer(ctx, find)
	if err != nil {
		return uuid.Nil, fmt.Errorf("find concurrent assistant trigger issuer: %w", err)
	}
	if len(issuers) == 0 {
		return uuid.Nil, fmt.Errorf("assistant trigger issuer name is taken by another registration: %w", ErrInvalidIdentity)
	}
	return issuers[0], nil
}

// TombstoneTrigger withdraws the workload a root trigger was bound to and
// retires the binding. Admissions, assignments, or issuers a user already
// removed or changed are tolerated.
func (s *Service) TombstoneTrigger(ctx context.Context, tx pgx.Tx, project, trigger uuid.UUID) error {
	if _, err := repo.New(tx).LockTriggers(ctx, repo.LockTriggersParams{ProjectID: project, TriggerIds: []uuid.UUID{trigger}}); err != nil {
		return fmt.Errorf("lock root trigger for withdrawal: %w", err)
	}
	return s.withdrawTrigger(ctx, tx, project, trigger)
}

// withdrawTrigger is TombstoneTrigger for a caller that already holds the
// trigger row lock.
func (s *Service) withdrawTrigger(ctx context.Context, tx pgx.Tx, project, trigger uuid.UUID) error {
	q := repo.New(tx)
	binding, err := q.GetTriggerBinding(ctx, repo.GetTriggerBindingParams{ProjectID: project, TriggerID: trigger})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read root trigger binding for withdrawal: %w", err)
	}
	if err := withdrawWorkload(ctx, tx, binding.OrganizationID, project, binding.WorkloadIssuerID, binding.Subject); err != nil {
		return err
	}
	if err := q.TombstoneTriggerBinding(ctx, repo.TombstoneTriggerBindingParams{ProjectID: project, TriggerID: trigger}); err != nil {
		return fmt.Errorf("retire root trigger binding: %w", err)
	}
	return nil
}

func withdrawWorkload(ctx context.Context, tx pgx.Tx, org string, project, issuer uuid.UUID, subject string) error {
	workloads := workloadrepo.New(tx)
	if _, err := workloads.LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: org, ID: issuer}); errors.Is(err, pgx.ErrNoRows) {
		// Withdrawing the issuer already withdrew everything under it.
		return nil
	} else if err != nil {
		return fmt.Errorf("lock assistant trigger issuer for withdrawal: %w", err)
	}
	if err := repo.New(tx).WithdrawExactAdmission(ctx, repo.WithdrawExactAdmissionParams{
		OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, WorkloadIssuerID: issuer, Subject: subject,
	}); err != nil {
		return fmt.Errorf("withdraw assistant trigger admission: %w", err)
	}
	exact := string(workloadidentity.MatchKindExact)
	remaining, err := workloads.CountLiveAdmissionsForSubject(ctx, workloadrepo.CountLiveAdmissionsForSubjectParams{OrganizationID: org, WorkloadIssuerID: issuer, MatchKind: exact, Subject: subject})
	if err != nil {
		return fmt.Errorf("count remaining assistant trigger admissions: %w", err)
	}
	if remaining > 0 {
		return nil
	}
	if _, err := workloads.SoftDeleteWorkloadAgentAssignmentForSubject(ctx, workloadrepo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{OrganizationID: org, WorkloadIssuerID: issuer, MatchKind: exact, Subject: subject}); err != nil {
		return fmt.Errorf("withdraw assistant trigger assignment: %w", err)
	}
	return nil
}

// TombstoneAssistant withdraws every root trigger workload an assistant's
// binding created, revokes its dedicated agent, and retires the binding.
// Assistants without a binding, or already deleted, are left unchanged.
func (s *Service) TombstoneAssistant(ctx context.Context, tx pgx.Tx, project, assistant uuid.UUID, actor urn.Principal, actorDisplayName *string) error {
	q := repo.New(tx)
	if _, err := q.LockAssistant(ctx, repo.LockAssistantParams{ProjectID: project, AssistantID: assistant}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock assistant for withdrawal: %w", err)
	}
	triggers, err := q.ListAssistantTriggerBindings(ctx, repo.ListAssistantTriggerBindingsParams{ProjectID: project, AssistantID: assistant})
	if err != nil {
		return fmt.Errorf("list assistant trigger bindings: %w", err)
	}
	if _, err := q.LockTriggers(ctx, repo.LockTriggersParams{ProjectID: project, TriggerIds: triggers}); err != nil {
		return fmt.Errorf("lock assistant root triggers: %w", err)
	}
	for _, trigger := range triggers {
		if err := s.withdrawTrigger(ctx, tx, project, trigger); err != nil {
			return err
		}
	}
	binding, err := q.TombstoneAssistantBinding(ctx, repo.TombstoneAssistantBindingParams{ProjectID: project, AssistantID: assistant})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("retire assistant binding: %w", err)
	}
	return s.revokeAgent(ctx, tx, binding.OrganizationID, binding.OriginalAgentID, actor, actorDisplayName)
}

// revokeAgent revokes the dedicated agent unless a user already revoked or
// deleted it.
func (s *Service) revokeAgent(ctx context.Context, tx pgx.Tx, org string, agentID uuid.UUID, actor urn.Principal, actorDisplayName *string) error {
	agents := agentrepo.New(tx)
	before, err := agents.GetAgentByIDForUpdate(ctx, agentrepo.GetAgentByIDForUpdateParams{OrganizationID: org, ID: agentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock dedicated assistant agent: %w", err)
	}
	after, err := agents.RevokeAgent(ctx, agentrepo.RevokeAgentParams{OrganizationID: org, ID: agentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revoke dedicated assistant agent: %w", err)
	}
	if err := s.audit.LogAgent(ctx, tx, audit.LogAgentEvent{
		OrganizationID: org, AgentURN: urn.NewAgentIdentity(after.ID.String()), Actor: actor, ActorDisplayName: actorDisplayName,
		Action: audit.ActionAgentRevoke, Name: after.Name,
		Before: agentownership.AgentAuditSnapshot(before), After: agentownership.AgentAuditSnapshot(after),
	}); err != nil {
		return fmt.Errorf("audit dedicated assistant agent revocation: %w", err)
	}
	return nil
}
