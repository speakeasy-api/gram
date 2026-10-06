package assistanttokens

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
)

// GenerateExecution signs an execution token after checking it against live
// state.
func (m *Manager) GenerateExecution(ctx context.Context, e assistantidentity.Execution) (string, error) {
	if err := m.validateExecutionAuthority(ctx, e); err != nil {
		return "", fmt.Errorf("assistant execution: %w", err)
	}
	raw, err := m.executionIssuer.MintAssistantExecution(e)
	if err != nil {
		return "", fmt.Errorf("sign assistant execution: %w", err)
	}
	return raw, nil
}

// validateExecutionAuthority reads live state directly rather than through the
// legacy revocation cache: the workload identity must still resolve, the
// assistant and thread must be live, and the turn user must still be an
// active member who can read the project.
func (m *Manager) validateExecutionAuthority(ctx context.Context, e assistantidentity.Execution) error {
	if err := m.identities.ValidateExecution(ctx, m.db, e); err != nil {
		return fmt.Errorf("validate execution identity: %w", err)
	}
	row, err := m.tokens.GetAssistantTokenRevocation(ctx, tokenrepo.GetAssistantTokenRevocationParams{ProjectID: e.Identity.ProjectID, AssistantID: e.Identity.AssistantID, ThreadID: e.ThreadID})
	if err != nil {
		return fmt.Errorf("load execution lifecycle: %w", err)
	}
	if row.ThreadDeleted || row.AssistantDeleted || row.AssistantStatus != "active" {
		return assistantidentity.ErrInvalidIdentity
	}
	if err := assistantidentity.CheckActor(ctx, m.db, m.authz, e.Identity.OrganizationID, e.Identity.ProjectID, e.HumanUserID); err != nil {
		return fmt.Errorf("check execution user: %w", err)
	}
	return nil
}
