package assistantidentity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// BindRootTrigger binds the persisted target, never a caller-supplied target.
// Only root trigger instances belong here; continuations reuse their root.
// Untouched legacy assistants remain untouched. Existing tombstones cannot be
// revived by ensure/retry; retarget is a separate, explicit operation.
func (s *Service) BindRootTrigger(ctx context.Context, tx pgx.Tx, org string, project, trigger uuid.UUID) error {
	return s.bindRoot(ctx, tx, org, project, trigger, false)
}

// RetargetRootTrigger runs after the caller changes the trigger target in the
// same transaction. It withdraws old authority and allocates a new incarnation.
func (s *Service) RetargetRootTrigger(ctx context.Context, tx pgx.Tx, org string, project, trigger uuid.UUID) error {
	return s.bindRoot(ctx, tx, org, project, trigger, true)
}

func (s *Service) bindRoot(ctx context.Context, tx pgx.Tx, org string, project, trigger uuid.UUID, retarget bool) error {
	if s == nil {
		return ErrInvalidIdentity
	}
	q := repo.New(tx)
	if err := lockProject(ctx, q, org, project); err != nil {
		return err
	}
	root, err := q.GetTrigger(ctx, repo.GetTriggerParams{OrganizationID: org, ProjectID: project, TriggerID: trigger})
	if err != nil {
		return resourceError("load root trigger", err)
	}
	if root.DefinitionSlug == "wake" {
		return ErrInvalidIdentity
	}
	if root.Deleted || root.Status != "active" {
		return ErrTombstoned
	}
	old, historyErr := q.GetTriggerBinding(ctx, repo.GetTriggerBindingParams{PlatformIssuer: s.issuer, PlatformJwksUri: s.jwksURI, OrganizationID: org, ProjectID: project, TriggerID: trigger})
	hasHistory := historyErr == nil
	if historyErr != nil && !errors.Is(historyErr, pgx.ErrNoRows) {
		return fmt.Errorf("load root history: %w", historyErr)
	}
	if root.TargetKind != "assistant" {
		if hasHistory && retarget {
			return tombstoneTrigger(ctx, tx, org, project, trigger)
		}
		if hasHistory {
			return ErrBrokenMapping
		}
		return nil
	}
	assistant, err := uuid.Parse(root.TargetRef)
	if err != nil {
		return fmt.Errorf("parse assistant root target: %w", ErrBrokenMapping)
	}
	a, err := q.GetAssistant(ctx, repo.GetAssistantParams{OrganizationID: org, ProjectID: project, AssistantID: assistant})
	if err != nil {
		return resourceError("load root target", err)
	}
	if a.Deleted || !a.ProjectLive {
		return ErrTombstoned
	}
	binding, bindingErr := q.GetAssistantBinding(ctx, repo.GetAssistantBindingParams{OrganizationID: org, ProjectID: project, AssistantID: assistant})
	if bindingErr != nil && !errors.Is(bindingErr, pgx.ErrNoRows) {
		return fmt.Errorf("load root assistant binding: %w", bindingErr)
	}
	if errors.Is(bindingErr, pgx.ErrNoRows) {
		if hasHistory && retarget {
			return tombstoneTrigger(ctx, tx, org, project, trigger)
		}
		if hasHistory {
			return ErrBrokenMapping
		}
		return nil
	}
	if !binding.Eligible {
		return ErrTombstoned
	}
	if hasHistory && !old.Deleted && old.OriginalAssistantBindingID == binding.ID && old.AssistantBindingGeneration == binding.Generation {
		if !old.Eligible {
			return ErrBrokenMapping
		}
		return nil
	}
	if hasHistory && !retarget {
		if old.Deleted {
			return ErrTombstoned
		}
		return ErrBrokenMapping
	}
	issuer, err := s.platformIssuer(ctx, q, org, project)
	if err != nil {
		return err
	}
	if _, err := q.LockDedicatedAgent(ctx, repo.LockDedicatedAgentParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, AgentID: binding.OriginalAgentID}); err != nil {
		return resourceError("lock dedicated agent", err)
	}
	if hasHistory {
		if err := tombstoneTrigger(ctx, tx, org, project, trigger); err != nil {
			return err
		}
	}
	subject := "assistant-trigger:" + trigger.String()
	generation := int64(1)
	if hasHistory {
		if old.OriginalWorkloadIssuerID != issuer || old.Subject != subject {
			return ErrBrokenMapping
		}
		generation = old.Generation + 1
	}
	if err := q.CreateAdmission(ctx, repo.CreateAdmissionParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, IssuerID: issuer, Subject: subject}); err != nil {
		return fmt.Errorf("create exact root admission: %w", err)
	}
	if err := q.CreateAssignment(ctx, repo.CreateAssignmentParams{OrganizationID: org, IssuerID: issuer, Subject: subject, AgentID: binding.OriginalAgentID}); err != nil {
		return fmt.Errorf("create exact root assignment: %w", err)
	}
	if err := q.CreateTriggerBinding(ctx, repo.CreateTriggerBindingParams{
		OrganizationID: org, ProjectID: project, TriggerID: trigger, AssistantBindingID: binding.ID,
		AssistantGeneration: binding.Generation, IssuerID: issuer, Subject: subject, Generation: generation,
	}); err != nil {
		return fmt.Errorf("create root workload binding: %w", err)
	}
	return nil
}

func (s *Service) platformIssuer(ctx context.Context, q *repo.Queries, org string, project uuid.UUID) (uuid.UUID, error) {
	issuer, err := q.GetPlatformIssuer(ctx, repo.GetPlatformIssuerParams{PlatformIssuer: s.issuer, OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}})
	if err == nil {
		if issuer.Deleted || issuer.JwksUri != s.jwksURI || issuer.AllowWildcardAdmission {
			return uuid.Nil, ErrTombstoned
		}
		return issuer.ID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("load platform trust registration: %w", err)
	}
	id, err := q.CreatePlatformIssuer(ctx, repo.CreatePlatformIssuerParams{
		OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true},
		Name: "Assistant roots " + project.String(), Issuer: s.issuer, JwksUri: s.jwksURI,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create platform trust registration: %w", err)
	}
	return id, nil
}

// TombstoneTrigger permanently withdraws this root's current association and
// revokes its existing wake sessions in the caller's transaction. History stays.
func TombstoneTrigger(ctx context.Context, tx pgx.Tx, org string, project, trigger uuid.UUID) error {
	if err := lockProject(ctx, repo.New(tx), org, project); err != nil {
		return err
	}
	return tombstoneTrigger(ctx, tx, org, project, trigger)
}

func tombstoneTrigger(ctx context.Context, tx pgx.Tx, org string, project, trigger uuid.UUID) error {
	q := repo.New(tx)
	old, err := q.GetTriggerBinding(ctx, repo.GetTriggerBindingParams{PlatformIssuer: "", PlatformJwksUri: "", OrganizationID: org, ProjectID: project, TriggerID: trigger})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read root authority for withdrawal: %w", err)
	}
	if err := workloadidentity.RevokeWorkloadSessionsTx(ctx, tx, org, old.OriginalWorkloadIssuerID, old.Subject); err != nil {
		return fmt.Errorf("revoke root sessions: %w", err)
	}
	if err := q.RevokeAdmission(ctx, repo.RevokeAdmissionParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, IssuerID: old.OriginalWorkloadIssuerID, Subject: old.Subject}); err != nil {
		return fmt.Errorf("withdraw root admission: %w", err)
	}
	if err := q.RevokeAssignment(ctx, repo.RevokeAssignmentParams{OrganizationID: org, IssuerID: old.OriginalWorkloadIssuerID, Subject: old.Subject}); err != nil {
		return fmt.Errorf("withdraw root assignment: %w", err)
	}
	if err := q.TombstoneTriggerBinding(ctx, repo.TombstoneTriggerBindingParams{OrganizationID: org, ProjectID: project, TriggerID: trigger}); err != nil {
		return fmt.Errorf("tombstone root mapping: %w", err)
	}
	return nil
}

// TombstoneAssistant withdraws all current root and agent wake authority,
// including assignments not reachable through a now-deleted root reference.
func TombstoneAssistant(ctx context.Context, tx pgx.Tx, org string, project, assistant uuid.UUID) error {
	q := repo.New(tx)
	if err := lockProject(ctx, q, org, project); err != nil {
		return err
	}
	b, err := q.GetAssistantBinding(ctx, repo.GetAssistantBindingParams{OrganizationID: org, ProjectID: project, AssistantID: assistant})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read assistant authority for withdrawal: %w", err)
	}
	// Session issuance locks issuer before agent. Preserve that order even when
	// withdrawing all roots at once; missing hard-deleted issuer is harmless.
	if err := q.LockAssistantIssuers(ctx, repo.LockAssistantIssuersParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, AssistantID: assistant}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock assistant issuer for withdrawal: %w", err)
	}
	if err := q.RevokeDedicatedAgent(ctx, repo.RevokeDedicatedAgentParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, AgentID: b.OriginalAgentID}); err != nil {
		return fmt.Errorf("revoke dedicated agent: %w", err)
	}
	if err := workloadidentity.RevokeAgentWorkloadSessionsTx(ctx, tx, org, b.OriginalAgentID); err != nil {
		return fmt.Errorf("revoke dedicated agent sessions: %w", err)
	}
	roots, err := q.ListAssistantTriggerHistory(ctx, repo.ListAssistantTriggerHistoryParams{OrganizationID: org, ProjectID: project, AssistantID: assistant})
	if err != nil {
		return fmt.Errorf("list assistant authority history: %w", err)
	}
	for _, trigger := range roots {
		if err := tombstoneTrigger(ctx, tx, org, project, trigger); err != nil {
			return err
		}
	}
	if err := q.RevokeDedicatedAdmissions(ctx, repo.RevokeDedicatedAdmissionsParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, AgentID: b.OriginalAgentID}); err != nil {
		return fmt.Errorf("revoke dedicated admissions: %w", err)
	}
	if err := q.RevokeDedicatedAssignments(ctx, repo.RevokeDedicatedAssignmentsParams{OrganizationID: org, AgentID: b.OriginalAgentID}); err != nil {
		return fmt.Errorf("revoke dedicated assignments: %w", err)
	}
	if err := q.TombstoneAssistantBinding(ctx, repo.TombstoneAssistantBindingParams{OrganizationID: org, ProjectID: project, AssistantID: assistant}); err != nil {
		return fmt.Errorf("tombstone assistant binding: %w", err)
	}
	return nil
}
