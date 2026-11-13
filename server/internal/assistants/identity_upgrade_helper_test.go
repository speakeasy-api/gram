package assistants

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
)

// UpgradeAssistantIdentity provisions an identity for an assistant created
// without one, as the staff migration does, and returns its committed state.
func (s *ServiceCore) UpgradeAssistantIdentity(ctx context.Context, p assistantidentity.ProvisionParams) (assistantRecord, error) {
	tx, err := s.db.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary only; provisioning uses generated SQLc methods.
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
