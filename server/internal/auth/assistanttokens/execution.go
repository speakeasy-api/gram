package assistanttokens

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"reflect"

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

// GenerateExecution is a server-only signer, not an HTTP input boundary.
// Production dispatch passes only the envelope loaded from its persisted event:
// captureExecution strips caller metadata and snapshots policy before enqueue.
// Comparing against a new snapshot here would widen old events after upgrades.
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

func (m *Manager) validateExecutionWorkloadAuthority(ctx context.Context, e assistantidentity.Execution) error {
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
	return nil
}

func (m *Manager) validateExecutionAuthority(ctx context.Context, e assistantidentity.Execution) error {
	if err := m.validateExecutionWorkloadAuthority(ctx, e); err != nil {
		return err
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

// AuthorizeExecution validates independent target pins and performs positive
// model admission without entering the legacy user authorization path.
func (m *Manager) AuthorizeExecution(ctx context.Context, raw string, target ExecutionTarget) error {
	e, err := m.ValidateExecution(ctx, raw, target)
	if err != nil {
		return fmt.Errorf("assistant execution: %w", err)
	}
	if err := m.executionIdentities.AdmitModel(ctx, m.executionDB, *e); err != nil {
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
	if err := m.validateMCPAuthExecutionOrigin(ctx, *input.Execution, input.OriginatingEventID, input.OrgID, input.ProjectID, input.AssistantID, input.ThreadID); err != nil {
		return "", fmt.Errorf("assistant execution: %w", err)
	}
	return m.generateMCPAuthFlow(input)
}

// ValidateExecutionMCPAuthFlow requires signature/TTL validation through
// ValidateMCPAuthFlow first. The signed row ID independently pins the durable
// invocation; never use the thread's mutable latest event as the resume target.
func (m *Manager) ValidateExecutionMCPAuthFlow(ctx context.Context, claims *MCPAuthFlowClaims) error {
	if claims == nil || claims.Execution == nil {
		return assistantidentity.ErrInvalidIdentity
	}
	origin, err := uuid.Parse(claims.OriginatingEventID)
	if err != nil {
		return assistantidentity.ErrInvalidIdentity
	}
	project, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return assistantidentity.ErrInvalidIdentity
	}
	assistant, err := uuid.Parse(claims.AssistantID)
	if err != nil {
		return assistantidentity.ErrInvalidIdentity
	}
	thread, err := uuid.Parse(claims.ThreadID)
	if err != nil {
		return assistantidentity.ErrInvalidIdentity
	}
	return m.validateMCPAuthExecutionOrigin(ctx, *claims.Execution, origin, claims.OrgID, project, assistant, thread)
}

func (m *Manager) validateMCPAuthExecutionOrigin(ctx context.Context, execution assistantidentity.Execution, origin uuid.UUID, org string, project, assistant, thread uuid.UUID) error {
	if origin == uuid.Nil {
		return assistantidentity.ErrInvalidIdentity
	}
	row, err := m.tokens.GetMCPAuthExecutionOrigin(ctx, tokenrepo.GetMCPAuthExecutionOriginParams{OriginatingEventID: origin, OrganizationID: org, ProjectID: project, AssistantID: assistant, ThreadID: thread})
	if errors.Is(err, pgx.ErrNoRows) {
		return assistantidentity.ErrInvalidIdentity
	}
	if err != nil {
		return fmt.Errorf("load OAuth execution origin: %w", err)
	}
	var payload struct {
		Execution *assistantidentity.Execution `json:"_gram_execution"`
	}
	if err := json.Unmarshal(row.NormalizedPayloadJson, &payload); err != nil || payload.Execution == nil {
		return assistantidentity.ErrInvalidIdentity
	}
	stored := *payload.Execution
	if err := stored.Check(); err != nil {
		return fmt.Errorf("validate stored OAuth execution: %w", err)
	}
	if err := execution.Check(); err != nil {
		return fmt.Errorf("validate OAuth execution state: %w", err)
	}
	// Check validates each policy against its canonical digest. JSONB may change
	// its byte representation; compare the digest and all other envelope fields.
	stored.Ceiling.Policy = execution.Ceiling.Policy
	if !reflect.DeepEqual(stored, execution) {
		return assistantidentity.ErrInvalidIdentity
	}
	return m.ValidateExecutionEnvelope(ctx, execution, ExecutionTarget{EventID: row.EventID, OrganizationID: org, ProjectID: project, AssistantID: assistant, ThreadID: thread})
}
