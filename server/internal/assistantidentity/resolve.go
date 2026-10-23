package assistantidentity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/lifecycle"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// States reports the configuration state of each assistant in one project.
// Assistants without a binding are NeverConfigured. This is the single
// definition of assistant identity state used by the API and by Resolve.
func States(ctx context.Context, db repo.DBTX, project uuid.UUID, assistants []uuid.UUID) (map[uuid.UUID]AssistantState, error) {
	states := make(map[uuid.UUID]AssistantState, len(assistants))
	for _, id := range assistants {
		states[id] = AssistantState{State: NeverConfigured, AgentID: nil}
	}
	if len(assistants) == 0 {
		return states, nil
	}
	rows, err := repo.New(db).ListAssistantAgentStates(ctx, repo.ListAssistantAgentStatesParams{ProjectID: project, AssistantIds: assistants})
	if err != nil {
		return nil, fmt.Errorf("load assistant identity states: %w", err)
	}
	for _, row := range rows {
		state := Unavailable
		if row.AgentActive {
			state = Active
		}
		states[row.AssistantID] = AssistantState{State: state, AgentID: new(row.AgentID)}
	}
	return states, nil
}

// Resolve reads an assistant's state and, for an active assistant, the live
// workload identity of one root trigger: its bound issuer and subject, the
// current admission, and the agent the current assignment names.
func (s *Service) Resolve(ctx context.Context, db DB, org string, project, assistant, trigger uuid.UUID) (Resolution, error) {
	tx, err := readSnapshot(ctx, db)
	if err != nil {
		return Resolution{}, err
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	result, err := resolve(ctx, tx, org, project, assistant, trigger)
	if err != nil {
		return Resolution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Resolution{}, fmt.Errorf("commit identity read snapshot: %w", err)
	}
	return result, nil
}

func resolve(ctx context.Context, tx pgx.Tx, org string, project, assistant, trigger uuid.UUID) (Resolution, error) {
	if trigger == uuid.Nil {
		return resolveStable(ctx, tx, org, project, assistant)
	}
	if org == "" || project == uuid.Nil || assistant == uuid.Nil || trigger == uuid.Nil {
		return Resolution{}, ErrInvalidIdentity
	}
	states, err := States(ctx, tx, project, []uuid.UUID{assistant})
	if err != nil {
		return Resolution{}, err
	}
	if state := states[assistant].State; state != Active {
		return Resolution{State: state, Identity: nil}, nil
	}
	unavailable := Resolution{State: Unavailable, Identity: nil}

	// Only a binding made for this assistant counts; a trigger bound for
	// another assistant resolves as unavailable here.
	binding, err := repo.New(tx).GetAssistantTriggerBinding(ctx, repo.GetAssistantTriggerBindingParams{ProjectID: project, TriggerID: trigger, AssistantID: assistant})
	if errors.Is(err, pgx.ErrNoRows) {
		return unavailable, nil
	}
	if err != nil {
		return Resolution{}, fmt.Errorf("read trigger binding: %w", err)
	}
	if binding.OrganizationID != org {
		return Resolution{}, ErrInvalidIdentity
	}
	admitted, err := workloadidentity.IsAdmitted(ctx, tx, workloadidentity.AdmissionParams{
		OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, WorkloadIssuerID: binding.WorkloadIssuerID, Subject: binding.Subject,
	})
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve trigger workload admission: %w", err)
	}
	if !admitted {
		return unavailable, nil
	}
	agentID, assigned, err := workloadidentity.ResolveAssignedAgent(ctx, tx, workloadidentity.AssignmentParams{
		OrganizationID: org, WorkloadIssuerID: binding.WorkloadIssuerID, Subject: binding.Subject,
	})
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve trigger workload agent: %w", err)
	}
	if !assigned {
		return unavailable, nil
	}
	agent, err := agents.ResolvePrincipal(ctx, tx, org, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()))
	if errors.Is(err, agents.ErrPrincipalNotFound) {
		return unavailable, nil
	}
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve trigger agent principal: %w", err)
	}
	if lifecycle.Derive(agent) != lifecycle.Active {
		return unavailable, nil
	}
	return Resolution{State: Active, Identity: &Identity{
		OrganizationID: org, ProjectID: project, AssistantID: assistant, AgentID: agent.ID,
		TriggerID: trigger, IssuerID: binding.WorkloadIssuerID, Subject: binding.Subject,
	}}, nil
}

// Validate reports ErrInvalidIdentity unless expected still resolves exactly.
func (s *Service) Validate(ctx context.Context, db DB, expected Identity) error {
	resolved, err := s.Resolve(ctx, db, expected.OrganizationID, expected.ProjectID, expected.AssistantID, expected.TriggerID)
	if err != nil {
		return err
	}
	return matchExpected(resolved, expected)
}

func matchExpected(resolved Resolution, expected Identity) error {
	if resolved.State != Active || resolved.Identity == nil || *resolved.Identity != expected {
		return ErrInvalidIdentity
	}
	return nil
}

func readSnapshot(ctx context.Context, db DB) (pgx.Tx, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable, BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, fmt.Errorf("begin identity read snapshot: %w", err)
	}
	return tx, nil
}

// resolveStable uses the existing assistant-to-agent binding, without selecting
// any trigger or requiring a workload issuer for a native agent credential.
func resolveStable(ctx context.Context, tx pgx.Tx, org string, project, assistant uuid.UUID) (Resolution, error) {
	if org == "" || project == uuid.Nil || assistant == uuid.Nil {
		return Resolution{}, ErrInvalidIdentity
	}
	binding, err := repo.New(tx).GetAssistantBinding(ctx, repo.GetAssistantBindingParams{ProjectID: project, AssistantID: assistant})
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve stable assistant agent: %w", err)
	}
	if binding.OrganizationID != org {
		return Resolution{}, ErrInvalidIdentity
	}
	agent, err := agents.ResolvePrincipal(ctx, tx, org, urn.NewPrincipal(urn.PrincipalTypeAgent, binding.OriginalAgentID.String()))
	if err != nil {
		return Resolution{}, fmt.Errorf("resolve stable assistant agent: %w", err)
	}
	if lifecycle.Derive(agent) != lifecycle.Active {
		return Resolution{State: Unavailable, Identity: nil}, nil
	}
	return Resolution{State: Active, Identity: &Identity{OrganizationID: org, ProjectID: project, AssistantID: assistant, AgentID: agent.ID, TriggerID: uuid.Nil, IssuerID: uuid.Nil, Subject: ""}}, nil
}
