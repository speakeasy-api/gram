package assistants

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func (s *ServiceCore) hydrateAssistantIdentityStates(ctx context.Context, projectID uuid.UUID, records []assistantRecord) error {
	ids := make([]uuid.UUID, 0, len(records))
	for i := range records {
		records[i].IdentityState = string(assistantidentity.NeverConfigured)
		records[i].AgentID = nil
		records[i].IdentityGeneration = nil
		ids = append(ids, records[i].ID)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := assistantrepo.New(s.db).ListAssistantIdentityStates(ctx, assistantrepo.ListAssistantIdentityStatesParams{ProjectID: projectID, AssistantIds: ids})
	if err != nil {
		return fmt.Errorf("load assistant identity states: %w", err)
	}
	indexes := make(map[uuid.UUID]int, len(records))
	for i := range records {
		indexes[records[i].ID] = i
	}
	for _, row := range rows {
		i, ok := indexes[row.OriginalAssistantID]
		if !ok {
			continue
		}
		records[i].IdentityGeneration = conv.PtrEmpty(row.Generation)
		records[i].IdentityState = string(assistantidentity.Tombstoned)
		if !row.Tombstoned {
			records[i].IdentityState = string(assistantidentity.Active)
			records[i].AgentID = conv.PtrEmpty(row.OriginalAgentID.String())
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

// UpgradeAssistantIdentity is opt-in for a legacy assistant. Its creator is
// retained; the authenticated actor only authorizes provisioning now.
func (s *ServiceCore) UpgradeAssistantIdentity(ctx context.Context, organizationID string, projectID, assistantID uuid.UUID, actorUserID string) (assistantRecord, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("begin assistant identity upgrade: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := triggerrepo.New(tx).LockTriggerProject(ctx, projectID); err != nil {
		return assistantRecord{}, fmt.Errorf("lock assistant identity project: %w", err)
	}

	row, err := assistantrepo.New(tx).LockAssistantIdentityAnchor(ctx, assistantrepo.LockAssistantIdentityAnchorParams{ProjectID: projectID, AssistantID: assistantID})
	if err != nil {
		return assistantRecord{}, fmt.Errorf("lock assistant for identity upgrade: %w", err)
	}
	if row.OrganizationID != organizationID {
		return assistantRecord{}, pgx.ErrNoRows
	}
	if _, err = s.identities.Upgrade(ctx, tx, assistantidentity.ProvisionParams{OrganizationID: organizationID, ProjectID: projectID, AssistantID: assistantID, ActorUserID: actorUserID}); err != nil {
		return assistantRecord{}, fmt.Errorf("assistant identity Upgrade: %w", err)
	}
	if _, err = s.ensureDashboardRootTx(ctx, tx, organizationID, projectID, assistantID, row.Name); err != nil {
		return assistantRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit assistant identity upgrade: %w", err)
	}
	return s.GetAssistant(ctx, projectID, assistantID)
}
