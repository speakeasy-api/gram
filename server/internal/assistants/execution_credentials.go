package assistants

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// executionTrigger is the root trigger whose workload identity a turn runs
// under. Wakes and OAuth continuations have no root trigger of their own, so
// they run under the assistant's dashboard trigger.
func (s *ServiceCore) executionTrigger(ctx context.Context, assistant assistantRecord, event assistantThreadEventRecord) (uuid.UUID, error) {
	var metadata struct {
		Source string `json:"_gram_source_kind"`
	}
	// Payloads that are not JSON objects carry no source and are not wakes.
	isWake := json.Unmarshal(event.NormalizedPayloadJSON, &metadata) == nil && metadata.Source == sourceKindWake
	if event.TriggerInstanceID.Valid && !isWake {
		return event.TriggerInstanceID.UUID, nil
	}
	return s.resolveDashboardTriggerInstance(ctx, assistant.OrganizationID, assistant.ProjectID, assistant.ID, assistant.Name)
}

// mintTurnCredential issues the principal credential an agent-backed turn runs
// with. A turn whose event identified its user runs as the agent acting for
// that user; any other turn runs as the workload identity of its trigger, with
// no user recorded. The credential carries no grants: admission bounds every
// request by the live policies of the agent, its owner, and that user.
// Identity rejections wrap ErrTurnIdentity.
func (s *ServiceCore) mintTurnCredential(ctx context.Context, assistant assistantRecord, thread assistantThreadRecord, event assistantThreadEventRecord, identity turnIdentity) (string, error) {
	trigger, err := s.executionTrigger(ctx, assistant, event)
	if err != nil {
		return "", fmt.Errorf("resolve execution trigger: %w", err)
	}
	resolution, err := s.identities.Resolve(ctx, s.db, assistant.OrganizationID, assistant.ProjectID, assistant.ID, trigger)
	if err != nil {
		return "", fmt.Errorf("resolve execution identity: %w", err)
	}
	if resolution.State != assistantidentity.Active {
		return "", fmt.Errorf("%w: trigger workload identity is %s", ErrTurnIdentity, resolution.State)
	}
	workload := *resolution.Identity
	credential := principalcredential.Credential{
		OrganizationID:   assistant.OrganizationID,
		ProjectID:        assistant.ProjectID,
		Principal:        urn.NewWorkloadPrincipal(workload.IssuerID, workload.Subject),
		AuthorizerUserID: "",
	}
	if identity.HumanKnown {
		credential.Principal = urn.NewPrincipal(urn.PrincipalTypeAgent, workload.AgentID.String())
		credential.AuthorizerUserID = identity.UserID
	}
	token, err := s.assistantTokens.MintTurnCredential(ctx, credential, assistanttokens.RuntimeBinding{
		OrganizationID: assistant.OrganizationID, ProjectID: assistant.ProjectID,
		AssistantID: assistant.ID, ThreadID: thread.ID, ChatID: thread.ChatID,
	})
	if err != nil {
		return "", fmt.Errorf("mint turn credential: %w", err)
	}
	return token, nil
}
