package assistantidentity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agentownership"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// Lock order shared by every writer: assistant row, then trigger rows in ID
// order, then the workload issuer row (the workload policy write lock).
// Binding a trigger share-locks its target assistant before reading the
// assistant binding, so a bind and a concurrent upgrade serialize and the
// later one sees the other's committed rows. A caller that locks an existing
// trigger before binding it must first call LockTarget for the new target.

// triggerSubjectPrefix prefixes a root trigger's ID to form its workload
// subject, so each trigger is one exact workload under the project issuer.
const triggerSubjectPrefix = "assistant-trigger:"

// LockTarget share-locks the assistant a trigger is about to target. Other
// targets take no lock.
func (s *Service) LockTarget(ctx context.Context, tx pgx.Tx, project uuid.UUID, targetKind, targetRef string) error {
	if targetKind != bgtriggers.TargetKindAssistant {
		return nil
	}
	assistant, err := uuid.Parse(targetRef)
	if err != nil {
		return nil //nolint:nilerr // a non-UUID target is not an assistant this package can bind
	}
	return shareLockAssistant(ctx, tx, project, assistant)
}

func shareLockAssistant(ctx context.Context, tx pgx.Tx, project, assistant uuid.UUID) error {
	if _, err := repo.New(tx).ShareLockAssistant(ctx, repo.ShareLockAssistantParams{ProjectID: project, AssistantID: assistant}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock trigger target assistant: %w", err)
	}
	return nil
}

// BindRootTrigger points a root trigger at its workload identity when its
// target assistant has a dedicated agent. Legacy assistants, other targets,
// continuation wakes, and already-bound triggers are left unchanged.
func (s *Service) BindRootTrigger(ctx context.Context, tx pgx.Tx, project, trigger uuid.UUID) error {
	return s.bindRoot(ctx, tx, project, trigger, false, contextActor(ctx))
}

// RetargetRootTrigger runs after the caller changes the trigger target in the
// same transaction. It withdraws the previous workload and binds the new one.
func (s *Service) RetargetRootTrigger(ctx context.Context, tx pgx.Tx, project, trigger uuid.UUID) error {
	return s.bindRoot(ctx, tx, project, trigger, true, contextActor(ctx))
}

func (s *Service) bindRoot(ctx context.Context, tx pgx.Tx, project, trigger uuid.UUID, retarget bool, actor auditActor) error {
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
		if err := s.withdrawTrigger(ctx, tx, project, trigger, actor); err != nil {
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
	// Read the binding in a fresh statement after the lock, so an upgrade that
	// committed while this transaction waited is visible.
	if err := shareLockAssistant(ctx, tx, project, assistant); err != nil {
		return err
	}
	binding, err := q.GetAssistantBinding(ctx, repo.GetAssistantBindingParams{ProjectID: project, AssistantID: assistant})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read root target assistant binding: %w", err)
	}

	issuer, err := s.projectIssuer(ctx, tx, root.OrganizationID, project, actor)
	if err != nil {
		return err
	}
	workloads := workloadrepo.New(tx)
	subject := triggerSubjectPrefix + trigger.String()
	projectID := uuid.NullUUID{UUID: project, Valid: true}
	admitted, err := workloadidentity.IsAdmitted(ctx, tx, workloadidentity.AdmissionParams{OrganizationID: root.OrganizationID, ProjectID: projectID, WorkloadIssuerID: issuer.ID, Subject: subject})
	if err != nil {
		return fmt.Errorf("check assistant trigger admission: %w", err)
	}
	if _, err := workloads.UpsertWorkloadAgentAssignment(ctx, workloadrepo.UpsertWorkloadAgentAssignmentParams{
		OrganizationID: root.OrganizationID, WorkloadIssuerID: issuer.ID, Subject: subject,
		MatchKind: string(workloadidentity.MatchKindExact), AgentID: binding.OriginalAgentID,
	}); err != nil {
		return fmt.Errorf("assign assistant trigger workload: %w", err)
	}
	if !admitted {
		admission, err := workloads.CreateWorkloadAdmission(ctx, workloadrepo.CreateWorkloadAdmissionParams{
			OrganizationID: root.OrganizationID, ProjectID: projectID, WorkloadIssuerID: issuer.ID, Subject: subject,
			MatchKind: string(workloadidentity.MatchKindExact), Name: conv.ToPGTextEmpty(""), Tags: []string{},
		})
		if err != nil {
			return fmt.Errorf("admit assistant trigger workload: %w", err)
		}
		if err := s.audit.LogWorkloadAdmissionAdmit(ctx, tx, audit.LogWorkloadAdmissionAdmitEvent{
			OrganizationID: root.OrganizationID, ProjectID: projectID,
			Actor: actor.principal, ActorDisplayName: actor.displayName, ActorSlug: nil,
			AdmissionURN: urn.NewWorkloadAdmission(admission.ID), AdmissionDisplayName: admission.Subject,
			AdmissionSnapshotAfter: workloadpolicy.AdmissionSnapshot(admission, issuer, binding.OriginalAgentID.String()),
		}); err != nil {
			return fmt.Errorf("audit assistant trigger admission: %w", err)
		}
	}
	if err := q.CreateTriggerBinding(ctx, repo.CreateTriggerBindingParams{
		OrganizationID: root.OrganizationID, ProjectID: project, TriggerID: trigger,
		AssistantBindingID: binding.ID, AssistantBindingGeneration: binding.Generation,
		WorkloadIssuerID: issuer.ID, Subject: subject,
	}); err != nil {
		return fmt.Errorf("create root trigger binding: %w", err)
	}
	return nil
}

// projectIssuer returns the project's trust registration for the Gram issuer,
// registering one when none exists, and holds its workload policy write lock.
func (s *Service) projectIssuer(ctx context.Context, tx pgx.Tx, org string, project uuid.UUID, actor auditActor) (workloadrepo.WorkloadIssuer, error) {
	q := repo.New(tx)
	projectID := uuid.NullUUID{UUID: project, Valid: true}
	find := repo.FindProjectIssuerParams{OrganizationID: org, ProjectID: projectID, Issuers: s.issuerSpellings}
	issuers, err := q.FindProjectIssuer(ctx, find)
	if err != nil {
		return workloadrepo.WorkloadIssuer{}, fmt.Errorf("find assistant trigger issuer: %w", err)
	}
	created := false
	if len(issuers) == 0 {
		id, err := q.CreateProjectIssuer(ctx, repo.CreateProjectIssuerParams{
			OrganizationID: org, ProjectID: projectID, Name: "Assistant triggers " + project.String(), Issuer: s.issuer, JwksUri: s.jwksURI,
		})
		switch {
		case err == nil:
			issuers, created = []uuid.UUID{id}, true
		case errors.Is(err, pgx.ErrNoRows):
			// A concurrent bind in this project registered it first.
			issuers, err = q.FindProjectIssuer(ctx, find)
			if err != nil {
				return workloadrepo.WorkloadIssuer{}, fmt.Errorf("find concurrent assistant trigger issuer: %w", err)
			}
		default:
			return workloadrepo.WorkloadIssuer{}, fmt.Errorf("register assistant trigger issuer: %w", err)
		}
	}
	if len(issuers) == 0 {
		return workloadrepo.WorkloadIssuer{}, fmt.Errorf("assistant trigger issuer name is taken by another registration: %w", ErrInvalidIdentity)
	}
	workloads := workloadrepo.New(tx)
	if _, err := workloads.LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: org, ID: issuers[0]}); err != nil {
		return workloadrepo.WorkloadIssuer{}, resourceError("lock assistant trigger issuer", err)
	}
	issuer, err := workloads.GetWorkloadIssuer(ctx, workloadrepo.GetWorkloadIssuerParams{OrganizationID: org, ProjectID: projectID, ID: issuers[0]})
	if err != nil {
		return workloadrepo.WorkloadIssuer{}, fmt.Errorf("load assistant trigger issuer: %w", err)
	}
	if created {
		if err := s.audit.LogWorkloadIssuerCreate(ctx, tx, audit.LogWorkloadIssuerCreateEvent{
			OrganizationID: org, ProjectID: projectID, Actor: actor.principal, ActorDisplayName: actor.displayName, ActorSlug: nil,
			IssuerURN: urn.NewWorkloadIssuer(issuer.ID), IssuerName: issuer.Name, IssuerSnapshotAfter: workloadpolicy.IssuerSnapshot(issuer),
		}); err != nil {
			return workloadrepo.WorkloadIssuer{}, fmt.Errorf("audit assistant trigger issuer: %w", err)
		}
	}
	return issuer, nil
}

// auditActor is who workload identity audit entries are attributed to.
type auditActor struct {
	principal   urn.Principal
	displayName *string
}

// contextActor attributes changes to the authenticated principal, or to the
// system when none is present.
func contextActor(ctx context.Context) auditActor {
	principal, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok {
		return auditActor{principal: agentownership.SystemActor, displayName: nil}
	}
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	return auditActor{principal: principal, displayName: authCtx.Email}
}

// TombstoneTrigger withdraws the workload a root trigger was bound to and
// retires the binding. Admissions, assignments, or issuers a user already
// removed or changed are tolerated.
func (s *Service) TombstoneTrigger(ctx context.Context, tx pgx.Tx, project, trigger uuid.UUID) error {
	if _, err := repo.New(tx).LockTriggers(ctx, repo.LockTriggersParams{ProjectID: project, TriggerIds: []uuid.UUID{trigger}}); err != nil {
		return fmt.Errorf("lock root trigger for withdrawal: %w", err)
	}
	return s.withdrawTrigger(ctx, tx, project, trigger, contextActor(ctx))
}

// withdrawTrigger is TombstoneTrigger for a caller that already holds the
// trigger row lock.
func (s *Service) withdrawTrigger(ctx context.Context, tx pgx.Tx, project, trigger uuid.UUID, actor auditActor) error {
	q := repo.New(tx)
	binding, err := q.GetTriggerBinding(ctx, repo.GetTriggerBindingParams{ProjectID: project, TriggerID: trigger})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read root trigger binding for withdrawal: %w", err)
	}
	if err := s.withdrawWorkload(ctx, tx, binding.OrganizationID, project, binding.WorkloadIssuerID, binding.Subject, actor); err != nil {
		return err
	}
	if err := q.TombstoneTriggerBinding(ctx, repo.TombstoneTriggerBindingParams{ProjectID: project, TriggerID: trigger}); err != nil {
		return fmt.Errorf("retire root trigger binding: %w", err)
	}
	return nil
}

func (s *Service) withdrawWorkload(ctx context.Context, tx pgx.Tx, org string, project, issuerID uuid.UUID, subject string, actor auditActor) error {
	workloads := workloadrepo.New(tx)
	if _, err := workloads.LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: org, ID: issuerID}); errors.Is(err, pgx.ErrNoRows) {
		// Withdrawing the issuer already withdrew everything under it.
		return nil
	} else if err != nil {
		return fmt.Errorf("lock assistant trigger issuer for withdrawal: %w", err)
	}
	projectID := uuid.NullUUID{UUID: project, Valid: true}
	issuer, err := workloads.GetWorkloadIssuer(ctx, workloadrepo.GetWorkloadIssuerParams{OrganizationID: org, ProjectID: projectID, ID: issuerID})
	if err != nil {
		return fmt.Errorf("load assistant trigger issuer for withdrawal: %w", err)
	}
	exact := string(workloadidentity.MatchKindExact)
	assigned, err := workloads.GetWorkloadAgentAssignmentForSubject(ctx, workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: org, WorkloadIssuerID: issuerID, MatchKind: exact, Subject: subject})
	if err != nil {
		return fmt.Errorf("read assistant trigger assignment: %w", err)
	}
	assignedAgent := ""
	if len(assigned) > 0 {
		assignedAgent = assigned[0].AgentID.String()
	}
	admissions, err := repo.New(tx).ListExactAdmissions(ctx, repo.ListExactAdmissionsParams{OrganizationID: org, ProjectID: projectID, WorkloadIssuerID: issuerID, Subject: subject})
	if err != nil {
		return fmt.Errorf("list assistant trigger admissions: %w", err)
	}
	for _, id := range admissions {
		withdrawn, err := workloads.SoftDeleteWorkloadAdmission(ctx, workloadrepo.SoftDeleteWorkloadAdmissionParams{OrganizationID: org, ProjectID: projectID, ID: id})
		if err != nil {
			return fmt.Errorf("withdraw assistant trigger admission: %w", err)
		}
		if err := s.audit.LogWorkloadAdmissionWithdraw(ctx, tx, audit.LogWorkloadAdmissionWithdrawEvent{
			OrganizationID: org, ProjectID: projectID, Actor: actor.principal, ActorDisplayName: actor.displayName, ActorSlug: nil,
			AdmissionURN: urn.NewWorkloadAdmission(withdrawn.ID), AdmissionDisplayName: withdrawn.Subject,
			AdmissionSnapshotBefore: workloadpolicy.AdmissionSnapshot(withdrawn, issuer, assignedAgent),
		}); err != nil {
			return fmt.Errorf("audit assistant trigger admission withdrawal: %w", err)
		}
	}
	remaining, err := workloads.CountLiveAdmissionsForSubject(ctx, workloadrepo.CountLiveAdmissionsForSubjectParams{OrganizationID: org, WorkloadIssuerID: issuerID, MatchKind: exact, Subject: subject})
	if err != nil {
		return fmt.Errorf("count remaining assistant trigger admissions: %w", err)
	}
	if remaining > 0 {
		return nil
	}
	if _, err := workloads.SoftDeleteWorkloadAgentAssignmentForSubject(ctx, workloadrepo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{OrganizationID: org, WorkloadIssuerID: issuerID, MatchKind: exact, Subject: subject}); err != nil {
		return fmt.Errorf("withdraw assistant trigger assignment: %w", err)
	}
	return nil
}

// TombstoneAssistant withdraws every root trigger workload an assistant's
// binding created and retires the binding. The agent is left as it is: it is
// managed like any other agent and may outlive the assistant. Assistants
// without a binding, or already deleted, are left unchanged.
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
		if err := s.withdrawTrigger(ctx, tx, project, trigger, auditActor{principal: actor, displayName: actorDisplayName}); err != nil {
			return err
		}
	}
	if err := q.TombstoneAssistantBinding(ctx, repo.TombstoneAssistantBindingParams{ProjectID: project, AssistantID: assistant}); err != nil {
		return fmt.Errorf("retire assistant binding: %w", err)
	}
	return nil
}
