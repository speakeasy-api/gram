package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
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

// mintExecutionToken issues the credential an agent-backed turn runs with: the
// agent's live workload identity acting for userID, with a ceiling taken from
// the agent's live policy. Identity rejections wrap ErrTurnIdentity.
func (s *ServiceCore) mintExecutionToken(ctx context.Context, assistant assistantRecord, thread assistantThreadRecord, event assistantThreadEventRecord, userID string) (string, error) {
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
	ceiling, err := s.identities.SnapshotCeiling(ctx, s.db, *resolution.Identity)
	if err != nil {
		return "", executionError(err)
	}
	token, err := s.assistantTokens.GenerateExecution(ctx, assistantidentity.Execution{
		Version:     assistantidentity.ExecutionVersion,
		Identity:    *resolution.Identity,
		Issuer:      s.identities.Issuer(),
		ThreadID:    thread.ID,
		EventID:     event.EventID,
		HumanUserID: userID,
		Ceiling:     ceiling,
	})
	if err != nil {
		return "", executionError(err)
	}
	return token, nil
}

func executionError(err error) error {
	if errors.Is(err, assistantidentity.ErrInvalidIdentity) || errors.Is(err, assistantidentity.ErrActorIneligible) {
		return fmt.Errorf("%w: %w", ErrTurnIdentity, err)
	}
	return fmt.Errorf("mint execution token: %w", err)
}
