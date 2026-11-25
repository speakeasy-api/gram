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
