package assistanttokens

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// ExecutionTarget comes from trusted route/event metadata, never token claims.
type ExecutionTarget struct {
	EventID        string
	OrganizationID string
	ProjectID      uuid.UUID
	AssistantID    uuid.UUID
	ThreadID       uuid.UUID
}

// ConfigureExecutionIdentity reuses the platform issuer. Call only at startup;
// there is no runtime invoker mutation or tenant-specific signing state.
func (m *Manager) ConfigureExecutionIdentity(issuer *mcpauthz.Issuer, identities *assistantidentity.Service) {
	m.executionIssuer = issuer
	m.executionIdentities = identities
}

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

// ValidateExecution establishes live identity, not model or MCP permissions.
// Every use must supply tenant and invocation pins; signature verification alone
// is insufficient. No legacy positive revocation-cache entry is consulted.
func (m *Manager) ValidateExecution(ctx context.Context, raw string, target ExecutionTarget) (*assistantidentity.Execution, error) {
	if m.executionIssuer == nil {
		return nil, assistantidentity.ErrInvalidIdentity
	}
	claims, err := m.executionIssuer.ValidateAssistantExecution(raw)
	if err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	e := claims.Execution
	if err := m.ValidateExecutionEnvelope(ctx, e, target); err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	return &e, nil
}

func (m *Manager) validateExecutionAuthority(ctx context.Context, e assistantidentity.Execution) error {
	if m.executionIssuer == nil || m.executionIdentities == nil || m.executionDB == nil {
		return assistantidentity.ErrInvalidIdentity
	}
	if err := m.executionIdentities.ValidateExecution(ctx, m.executionDB, e); err != nil {
		return fmt.Errorf("assistant execution: %w", err)
	}
	// Use the existing DB-backed lifecycle query directly, not its five-second
	// memo. Bound tokens may not survive thread deletion or reassignment.
	row, err := m.tokens.GetAssistantTokenRevocation(ctx, tokenrepo.GetAssistantTokenRevocationParams{ProjectID: e.Identity.ProjectID, AssistantID: e.Identity.AssistantID, ThreadID: e.ThreadID})
	if err != nil {
		return fmt.Errorf("assistant execution: %w", err)
	}
	if row.ThreadDeleted || row.AssistantDeleted || row.AssistantStatus != "active" {
		return assistantidentity.ErrInvalidIdentity
	}
	if e.HumanUserID != "" {
		active, err := m.orgs.HasActiveOrganizationUser(ctx, organizationsrepo.HasActiveOrganizationUserParams{OrganizationID: e.Identity.OrganizationID, UserID: e.HumanUserID})
		if err != nil {
			return fmt.Errorf("assistant execution: %w", err)
		}
		if !active {
			return assistantidentity.ErrActorIneligible
		}
		principals, err := authz.ResolveUserPrincipals(ctx, m.executionDB, e.Identity.OrganizationID, e.HumanUserID)
		if err != nil {
			return fmt.Errorf("assistant execution: %w", err)
		}
		grants, err := authz.LoadGrants(ctx, m.executionDB, e.Identity.OrganizationID, principals)
		if err != nil {
			return fmt.Errorf("assistant execution: %w", err)
		}
		allowed, err := authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: e.Identity.ProjectID.String(), ResourceKind: "", Dimensions: nil})
		if err != nil {
			return fmt.Errorf("assistant execution: %w", err)
		}
		if !allowed {
			return assistantidentity.ErrActorIneligible
		}
	}
	return nil
}

// AuthorizeExecution is intentionally closed during AIM-410 rollout. Identity
// tokens must not enter the legacy user authorization path. AIM-411 replaces
// this gate with positive mode-aware model/business admission.
func (m *Manager) AuthorizeExecution(ctx context.Context, raw string, target ExecutionTarget) error {
	e, err := m.ValidateExecution(ctx, raw, target)
	if err != nil {
		return fmt.Errorf("assistant execution: %w", err)
	}
	if err := assistantidentity.AdmitExecution(*e); err != nil {
		return fmt.Errorf("assistant execution admission: %w", err)
	}
	return nil
}

// ValidateExecutionEnvelope is also used for signed OAuth continuation state.
func (m *Manager) ValidateExecutionEnvelope(ctx context.Context, e assistantidentity.Execution, target ExecutionTarget) error {
	if e.Identity.OrganizationID != target.OrganizationID || e.Identity.ProjectID != target.ProjectID || e.Identity.AssistantID != target.AssistantID || e.ThreadID != target.ThreadID || e.InvocationEventID() != target.EventID {
		return assistantidentity.ErrInvalidIdentity
	}
	return m.validateExecutionAuthority(ctx, e)
}

func (m *Manager) GenerateExecutionMCPAuthFlow(ctx context.Context, input MCPAuthFlowInput) (string, error) {
	if input.Execution == nil {
		return "", assistantidentity.ErrInvalidIdentity
	}
	if err := m.ValidateExecutionEnvelope(ctx, *input.Execution, ExecutionTarget{EventID: input.Execution.InvocationEventID(), OrganizationID: input.OrgID, ProjectID: input.ProjectID, AssistantID: input.AssistantID, ThreadID: input.ThreadID}); err != nil {
		return "", fmt.Errorf("assistant execution: %w", err)
	}
	return m.generateMCPAuthFlow(input)
}
