package assistanttokens

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
)

// ExecutionTarget comes from trusted route or event metadata, never from token
// claims.
type ExecutionTarget struct {
	EventID        string
	OrganizationID string
	ProjectID      uuid.UUID
	AssistantID    uuid.UUID
	ThreadID       uuid.UUID
}

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

// ValidateExecution verifies an execution token, pins it to target, and
// revalidates it against live state. It establishes identity only, not model
// or MCP permissions.
func (m *Manager) ValidateExecution(ctx context.Context, raw string, target ExecutionTarget) (*assistantidentity.Execution, error) {
	claims, err := m.executionIssuer.ValidateAssistantExecution(raw)
	if err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	e := claims.Execution
	if e.Identity.OrganizationID != target.OrganizationID || e.Identity.ProjectID != target.ProjectID || e.Identity.AssistantID != target.AssistantID || e.ThreadID != target.ThreadID || e.EventID != target.EventID {
		return nil, assistantidentity.ErrInvalidIdentity
	}
	if err := m.validateExecutionAuthority(ctx, e); err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	return &e, nil
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
