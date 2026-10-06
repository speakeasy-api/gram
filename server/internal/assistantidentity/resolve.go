package assistantidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/lifecycle"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
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

// SnapshotCeiling validates expected and captures the representable subset of
// its agent's live policy in the same snapshot. Later policy edits cannot
// mutate the returned canonical bytes.
func (s *Service) SnapshotCeiling(ctx context.Context, db DB, expected Identity) (CeilingSnapshot, error) {
	tx, err := readSnapshot(ctx, db)
	if err != nil {
		return CeilingSnapshot{}, err
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	resolved, err := resolve(ctx, tx, expected.OrganizationID, expected.ProjectID, expected.AssistantID, expected.TriggerID)
	if err != nil {
		return CeilingSnapshot{}, err
	}
	if err := matchExpected(resolved, expected); err != nil {
		return CeilingSnapshot{}, err
	}
	policy, err := runtimepolicy.LoadAgentPolicy(ctx, tx, expected.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, expected.AgentID.String()))
	if err != nil {
		return CeilingSnapshot{}, fmt.Errorf("load ceiling agent policy: %w", err)
	}
	grants, err := runtimepolicy.DelegableGrantsWithExclusions(policy, policy, policy)
	if err != nil {
		return CeilingSnapshot{}, fmt.Errorf("derive assistant ceiling: %w", err)
	}
	version := runtimepolicy.CurrentDelegatedPolicyVersion
	delegated, err := runtimepolicy.NewDelegatedPolicy(version, grants)
	if err != nil {
		return CeilingSnapshot{}, fmt.Errorf("construct assistant ceiling: %w", err)
	}
	encoded, err := runtimepolicy.EncodeDelegatedPolicy(version, delegated)
	if err != nil {
		return CeilingSnapshot{}, fmt.Errorf("encode assistant ceiling: %w", err)
	}
	digest := sha256.Sum256(encoded)
	if err := tx.Commit(ctx); err != nil {
		return CeilingSnapshot{}, fmt.Errorf("commit ceiling snapshot: %w", err)
	}
	return CeilingSnapshot{EncodingVersion: version, Policy: encoded, Digest: hex.EncodeToString(digest[:])}, nil
}

func readSnapshot(ctx context.Context, db DB) (pgx.Tx, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable, BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, fmt.Errorf("begin identity read snapshot: %w", err)
	}
	return tx, nil
}
