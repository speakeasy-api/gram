package assistants

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
)

func (s *ServiceCore) hydrateAssistantIdentityStates(ctx context.Context, projectID uuid.UUID, records []assistantRecord) error {
	ids := make([]uuid.UUID, 0, len(records))
	for i := range records {
		ids = append(ids, records[i].ID)
	}
	states, err := assistantidentity.States(ctx, s.db, projectID, ids)
	if err != nil {
		return fmt.Errorf("hydrate assistant identity states: %w", err)
	}
	for i := range records {
		state := states[records[i].ID]
		records[i].IdentityState = string(state.State)
		records[i].AgentID = nil
		if state.AgentID != nil {
			records[i].AgentID = new(state.AgentID.String())
		}
	}
	return nil
}

func (s *ServiceCore) hydrateAssistantIdentityState(ctx context.Context, projectID uuid.UUID, record *assistantRecord) error {
	records := []assistantRecord{*record}
	if err := s.hydrateAssistantIdentityStates(ctx, projectID, records); err != nil {
		return err
	}
	*record = records[0]
	return nil
}

// UpgradeAssistantIdentity points an existing assistant at an agent: a new one
// owned by the assistant's creator, or the existing one p.AgentID names. The
// actor is recorded as who configured it.
func (s *ServiceCore) UpgradeAssistantIdentity(ctx context.Context, p assistantidentity.ProvisionParams) (assistantRecord, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("begin assistant identity upgrade: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err = s.identities.Provision(ctx, tx, p); err != nil {
		return assistantRecord{}, fmt.Errorf("provision assistant identity: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit assistant identity upgrade: %w", err)
	}
	return s.GetAssistant(ctx, p.ProjectID, p.AssistantID)
}
