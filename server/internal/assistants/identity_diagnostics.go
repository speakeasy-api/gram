package assistants

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/gen/types"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/tunnel/jwks"
)

// identityDiagnostics is deliberately a management-only read. Dispatch and list
// hydration do not scan roots/history; callers first authorize the exact assistant.
func (s *ServiceCore) identityDiagnostics(ctx context.Context, a assistantRecord) (*types.AssistantIdentityDiagnostics, error) {
	q := assistantrepo.New(s.db)
	health, err := q.GetAssistantIdentityHealth(ctx, assistantrepo.GetAssistantIdentityHealthParams{OrganizationID: a.OrganizationID, ProjectID: a.ProjectID, AssistantID: a.ID})
	if err != nil {
		return nil, fmt.Errorf("read assistant identity health: %w", err)
	}
	gates := s.identities.Rollout()
	result := &types.AssistantIdentityDiagnostics{Health: health, ProvisioningEnabled: !gates.DisableProvisioning, ExecutionEnabled: !gates.DisableExecution, SlackDelegationEnabled: !gates.DisableSlackDelegation, Bindings: []*types.AssistantIdentityBinding{}, BindingsTruncated: false, LastEventID: nil, LastExecutionMode: nil, LastFallbackReason: nil, LastEventStatus: nil, LastInitiatingUserID: nil}
	roots, err := q.ListAssistantIdentityRoots(ctx, assistantrepo.ListAssistantIdentityRootsParams{OrganizationID: a.OrganizationID, ProjectID: a.ProjectID, AssistantID: a.ID.String(), PlatformIssuer: s.identities.Issuer(), PlatformJwksUri: s.identities.Issuer() + jwks.Path})
	if err != nil {
		return nil, fmt.Errorf("read assistant workload roots: %w", err)
	}
	result.BindingsTruncated = len(roots) > 100
	if result.BindingsTruncated {
		roots = roots[:100]
	}
	for _, root := range roots {
		result.Bindings = append(result.Bindings, &types.AssistantIdentityBinding{TriggerID: root.ID.String(), TriggerKind: root.DefinitionSlug, TriggerStatus: root.Status, State: root.State, Generation: root.Generation})
	}
	event, err := q.GetLastAssistantIdentityEvent(ctx, assistantrepo.GetLastAssistantIdentityEventParams{OrganizationID: a.OrganizationID, ProjectID: a.ProjectID, AssistantID: a.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read assistant execution diagnostics: %w", err)
	}
	result.LastEventID = conv.PtrEmpty(event.EventID)
	result.LastExecutionMode = conv.PtrEmpty(event.ExecutionMode)
	result.LastFallbackReason = conv.PtrEmpty(event.FallbackReason)
	result.LastEventStatus = conv.PtrEmpty(event.Status)
	result.LastInitiatingUserID = conv.PtrEmpty(event.InitiatingUserID)
	return result, nil
}
