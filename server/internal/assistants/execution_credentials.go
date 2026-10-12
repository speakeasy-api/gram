package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
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

// delegableGrants narrows ceiling to what the agent's owner and the
// authorizing user can delegate right now, the same rule agent key issuance
// applies. Admission re-checks the ceiling and the owner on every use.
func (s *ServiceCore) delegableGrants(ctx context.Context, organizationID string, agentID uuid.UUID, ceiling []authz.Grant, authorizerUserID string) ([]authz.Grant, error) {
	agent, err := agentrepo.New(s.db).GetAgentByID(ctx, agentrepo.GetAgentByIDParams{OrganizationID: organizationID, ID: agentID})
	if err != nil {
		return nil, fmt.Errorf("load turn agent: %w", err)
	}
	policies := make([][]authz.Grant, 0, 2)
	for _, user := range []string{agent.OwnerUserID, authorizerUserID} {
		principals, err := authz.ResolveUserPrincipals(ctx, s.db, organizationID, user)
		if err != nil {
			return nil, fmt.Errorf("resolve delegating user principals: %w", err)
		}
		grants, err := authz.LoadGrants(ctx, s.db, organizationID, principals)
		if err != nil {
			return nil, fmt.Errorf("load delegating user grants: %w", err)
		}
		policies = append(policies, grants)
	}
	grants, err := runtimepolicy.DelegableGrantsWithExclusions(ceiling, policies[0], policies[1])
	if err != nil {
		return nil, fmt.Errorf("derive turn credential grants: %w", err)
	}
	return grants, nil
}

// mintTurnCredential issues the principal credential an agent-backed turn runs
// with, bounded by the agent's live policy. A turn whose event identified its
// user runs as the agent acting for that user, further bounded by what the
// agent's owner and that user can delegate; any other turn runs as the
// workload identity of its trigger, with no user recorded. Identity
// rejections wrap ErrTurnIdentity.
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
	ceiling, err := s.identities.SnapshotCeiling(ctx, s.db, workload)
	if errors.Is(err, assistantidentity.ErrInvalidIdentity) {
		return "", fmt.Errorf("%w: %w", ErrTurnIdentity, err)
	}
	if err != nil {
		return "", fmt.Errorf("snapshot agent ceiling: %w", err)
	}
	policy, err := runtimepolicy.DecodeDelegatedPolicy(ceiling.EncodingVersion, ceiling.Policy)
	if err != nil {
		return "", fmt.Errorf("decode agent ceiling: %w", err)
	}
	credential := principalcredential.Credential{
		OrganizationID:   assistant.OrganizationID,
		ProjectID:        assistant.ProjectID,
		Principal:        urn.NewWorkloadPrincipal(workload.IssuerID, workload.Subject),
		AuthorizerUserID: "",
		Grants:           policy.RuntimeGrants(),
	}
	if identity.HumanKnown {
		credential.Principal = urn.NewPrincipal(urn.PrincipalTypeAgent, workload.AgentID.String())
		credential.AuthorizerUserID = identity.UserID
		credential.Grants, err = s.delegableGrants(ctx, assistant.OrganizationID, workload.AgentID, credential.Grants, identity.UserID)
		if err != nil {
			return "", err
		}
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
