package assistantidentity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RuntimeIdentity binds a managed runtime to the assistant's dedicated agent.
// It grants no event authority: each incoming event must still resolve its own
// trigger identity. BindingID and BindingGeneration fence replacement, including
// a replacement that selects the same agent.
type RuntimeIdentity struct {
	OrganizationID    string    `json:"organization_id"`
	ProjectID         uuid.UUID `json:"project_id"`
	AssistantID       uuid.UUID `json:"assistant_id"`
	AgentID           uuid.UUID `json:"agent_id"`
	BindingID         uuid.UUID `json:"binding_id"`
	BindingGeneration int64     `json:"binding_generation"`
}

// ResolveRuntimeLocked uses the lifecycle transaction's assistant lock. Runtime
// provisioning must not borrow any one trigger's admission or assignment.
func (s *Service) ResolveRuntimeLocked(ctx context.Context, tx pgx.Tx, org string, project, assistant uuid.UUID) (RuntimeIdentity, error) {
	a, err := repo.New(tx).LockAssistant(ctx, repo.LockAssistantParams{ProjectID: project, AssistantID: assistant})
	if err != nil {
		return RuntimeIdentity{}, resourceError("lock runtime identity assistant", err)
	}
	if a.OrganizationID != org {
		return RuntimeIdentity{}, ErrInvalidIdentity
	}
	return resolveRuntime(ctx, tx, org, project, assistant)
}

func resolveRuntime(ctx context.Context, tx pgx.Tx, org string, project, assistant uuid.UUID) (RuntimeIdentity, error) {
	var zero RuntimeIdentity
	if org == "" || project == uuid.Nil || assistant == uuid.Nil {
		return zero, ErrInvalidIdentity
	}
	q := repo.New(tx)
	binding, err := q.GetAssistantBinding(ctx, repo.GetAssistantBindingParams{ProjectID: project, AssistantID: assistant})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrInvalidIdentity
	}
	if err != nil {
		return zero, fmt.Errorf("read runtime assistant binding: %w", err)
	}
	states, err := States(ctx, tx, project, []uuid.UUID{assistant})
	if err != nil {
		return zero, err
	}
	state := states[assistant]
	if binding.OrganizationID != org || state.State != Active || state.AgentID == nil || *state.AgentID != binding.OriginalAgentID {
		return zero, ErrInvalidIdentity
	}
	agent, err := agents.ResolvePrincipal(ctx, tx, org, urn.NewPrincipal(urn.PrincipalTypeAgent, binding.OriginalAgentID.String()))
	if errors.Is(err, agents.ErrPrincipalNotFound) {
		return zero, ErrInvalidIdentity
	}
	if err != nil {
		return zero, fmt.Errorf("resolve runtime agent: %w", err)
	}
	if agent.OwnerReassignmentRequiredAt.Valid {
		return zero, ErrInvalidIdentity
	}
	return RuntimeIdentity{OrganizationID: org, ProjectID: project, AssistantID: assistant, AgentID: binding.OriginalAgentID, BindingID: binding.ID, BindingGeneration: binding.Generation}, nil
}

// ResolveRuntime takes a coherent snapshot without changing event resolution.
func (s *Service) ResolveRuntime(ctx context.Context, db DB, org string, project, assistant uuid.UUID) (RuntimeIdentity, error) {
	tx, err := readSnapshot(ctx, db)
	if err != nil {
		return RuntimeIdentity{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	identity, err := resolveRuntime(ctx, tx, org, project, assistant)
	if err != nil {
		return RuntimeIdentity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RuntimeIdentity{}, fmt.Errorf("commit runtime identity read snapshot: %w", err)
	}
	return identity, nil
}

func (s *Service) ValidateRuntime(ctx context.Context, db DB, expected RuntimeIdentity) error {
	current, err := s.ResolveRuntime(ctx, db, expected.OrganizationID, expected.ProjectID, expected.AssistantID)
	if err != nil {
		return err
	}
	if current != expected {
		return ErrInvalidIdentity
	}
	return nil
}
