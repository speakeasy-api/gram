package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

const executionMetadataKey = "_gram_execution"

var errExecutionDenied = errors.New("assistant execution denied")

func reservedExecutionKey(key string) string {
	for _, reserved := range []string{executionMetadataKey, "_gram_resume_user_id", "gram_event_kind", "_gram_source_kind"} {
		// Match encoding/json's case-folded struct field lookup, including
		// Unicode folds. Never lowercase unrelated user payload keys/values.
		if strings.EqualFold(key, reserved) {
			return reserved
		}
	}
	return ""
}

func classifyExecutionDispatchError(err error) error {
	if errors.Is(err, assistantidentity.ErrInvalidIdentity) || errors.Is(err, assistantidentity.ErrActorIneligible) || errors.Is(err, assistantidentity.ErrExecutionAdmissionRequired) {
		return fmt.Errorf("%w: %w", errExecutionDenied, err)
	}
	return err
}

// captureExecution runs only after ingress normalization. Never copy a caller's
// reserved metadata, including on legacy paths. An insertion retry cannot replace
// the original event because InsertAssistantThreadEvent is DO NOTHING.
func (s *ServiceCore) captureExecution(ctx context.Context, assistant assistantRecord, source string, threadID uuid.UUID, trigger uuid.NullUUID, eventID string, raw []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return nil, fmt.Errorf("trigger event payload must be a JSON object")
	}
	for key := range payload {
		if reservedExecutionKey(key) != "" {
			delete(payload, key)
		}
	}
	// All duplicate and case-variant reserved keys were discarded. Only the
	// server-derived source is reintroduced; continuations use a trusted path.
	payload["_gram_source_kind"], _ = json.Marshal(source)
	if s.identities == nil {
		return nil, fmt.Errorf("assistant identity resolver unavailable")
	}
	if !trigger.Valid {
		// Older ingress did not always carry a trigger. Absence is compatible only
		// when durable assistant binding history is genuinely absent.
		_, err := identityrepo.New(s.db).GetAssistantBinding(ctx, identityrepo.GetAssistantBindingParams{OrganizationID: assistant.OrganizationID, ProjectID: assistant.ProjectID, AssistantID: assistant.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return marshalExecutionPayload(payload)
		}
		if err != nil {
			return nil, fmt.Errorf("assistant execution: %w", err)
		}

		if source != sourceKindDashboard {
			return nil, fmt.Errorf("assistant execution has no trigger")
		}
		id, err := s.resolveDashboardTriggerInstance(ctx, assistant.OrganizationID, assistant.ProjectID, assistant.ID, assistant.Name)
		if err != nil {
			return nil, fmt.Errorf("assistant execution: %w", err)
		}
		trigger = uuid.NullUUID{UUID: id, Valid: true}
	}
	resolved, err := s.identities.Resolve(ctx, s.db, assistant.OrganizationID, assistant.ProjectID, assistant.ID, trigger.UUID)
	if err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	if resolved.State == assistantidentity.NeverConfigured {
		return marshalExecutionPayload(payload)
	}
	if resolved.State != assistantidentity.Active || resolved.Identity == nil {
		return nil, assistantidentity.ErrInvalidIdentity
	}
	clean, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	var event assistantThreadEventRecord
	event.NormalizedPayloadJSON = clean
	event.TriggerInstanceID = trigger
	mode, human, fallback, err := selectExecutionActor(ctx, assistant, source, event, slackrepo.New(s.db).ResolveSlackMappingUser, assistantrepo.New(s.db).FindLegacyWakeRequester)
	if err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	// Capture identity now; independently recheck selected-user eligibility at
	// token issuance and dispatch. Never reselect an owner after a denial.
	ceiling, err := s.identities.SnapshotCeiling(ctx, s.db, *resolved.Identity)
	if err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	execution := assistantidentity.Execution{ContinuationEventID: "", Version: assistantidentity.ExecutionVersion, Identity: *resolved.Identity, Issuer: s.identities.Issuer(), ThreadID: threadID, EventID: eventID, Mode: mode, HumanUserID: human, FallbackReason: fallback, Ceiling: ceiling}
	if err := execution.Check(); err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	payload[executionMetadataKey], err = json.Marshal(execution)
	if err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	return marshalExecutionPayload(payload)
}

func selectExecutionActor(ctx context.Context, assistant assistantRecord, source string, event assistantThreadEventRecord, lookup slackUserLookup, legacy legacyWakeLookup) (assistantidentity.ExecutionMode, string, string, error) {
	switch source {
	case sourceKindDashboard, sourceKindWake:
		if source == sourceKindDashboard {
			var payload dashboardEventPayload
			if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
				return "", "", "", fmt.Errorf("assistant execution: %w", err)
			}
			if payload.UserID == "" {
				return "", "", "", fmt.Errorf("dashboard execution has no authenticated sender")
			}
		}
		human, err := selectTurnUser(ctx, assistant, source, event, lookup, legacy)
		if err != nil {
			return "", "", "", fmt.Errorf("select execution actor: %w", err)
		}
		return assistantidentity.ExecutionWorkloadHuman, human, "", nil
	case sourceKindSlack:
		mapped := false
		wrapped := func(ctx context.Context, p slackrepo.ResolveSlackMappingUserParams) (string, error) {
			if lookup == nil {
				return "", nil
			}
			user, err := lookup(ctx, p)
			mapped = err == nil && user != ""
			if err != nil {
				return "", fmt.Errorf("resolve Slack execution actor: %w", err)
			}
			return user, nil
		}
		human, err := selectTurnUser(ctx, assistant, source, event, wrapped, legacy)
		if err != nil {
			return "", "", "", fmt.Errorf("assistant execution: %w", err)
		}
		if mapped {
			return assistantidentity.ExecutionWorkloadHuman, human, "", nil
		}
		return assistantidentity.ExecutionWorkload, "", "slack_mapping_unavailable", nil
	default:
		return assistantidentity.ExecutionWorkload, "", "", nil
	}
}

func decodeExecution(raw []byte) (*assistantidentity.Execution, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	for key := range payload {
		if canonical := reservedExecutionKey(key); canonical != "" && key != canonical {
			return nil, assistantidentity.ErrInvalidIdentity
		}
	}
	value, exists := payload[executionMetadataKey]
	if !exists {
		return nil, nil
	}
	var execution assistantidentity.Execution
	if err := json.Unmarshal(value, &execution); err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	if err := execution.Check(); err != nil {
		return nil, fmt.Errorf("assistant execution: %w", err)
	}
	return &execution, nil
}

// checkExecutionDispatch preserves unversioned turns and positively admits
// bound turns against live agent policy intersected with the saved ceiling.
// Denial must never mint an owner token or reselect an invoker.
func (s *ServiceCore) checkExecutionDispatch(ctx context.Context, assistant assistantRecord, thread assistantThreadRecord, event assistantThreadEventRecord) error {
	execution, err := decodeExecution(event.NormalizedPayloadJSON)
	if err != nil {
		return fmt.Errorf("assistant execution payload: %w: %w", assistantidentity.ErrInvalidIdentity, err)
	}
	if execution == nil {
		// Legacy envelopes do not have live identity validation. Re-read the
		// lifecycle here: an already admitted turn may outlive a pause, and
		// the assistant record passed by the processing loop can be stale.
		current, err := assistantrepo.New(s.db).GetAssistant(ctx, assistantrepo.GetAssistantParams{AssistantID: assistant.ID, ProjectID: assistant.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return assistantidentity.ErrInvalidIdentity
		}
		if err != nil {
			return fmt.Errorf("read legacy assistant lifecycle: %w", err)
		}
		if current.ProjectID != assistant.ProjectID || current.Status != StatusActive {
			return assistantidentity.ErrInvalidIdentity
		}
		return nil
	}
	if execution.Identity.OrganizationID != assistant.OrganizationID || execution.Identity.ProjectID != assistant.ProjectID || execution.Identity.AssistantID != assistant.ID || execution.ThreadID != thread.ID || execution.InvocationEventID() != event.EventID {
		return assistantidentity.ErrInvalidIdentity
	}
	if err := s.identities.ValidateExecution(ctx, s.db, *execution); err != nil {
		return fmt.Errorf("assistant execution: %w", err)
	}
	if execution.HumanUserID != "" {
		if err := s.checkTurnUser(ctx, assistant, execution.HumanUserID); err != nil {
			return fmt.Errorf("assistant execution: %w", err)
		}
	}
	if err := s.identities.AdmitModel(ctx, s.db, *execution); err != nil {
		return fmt.Errorf("assistant execution admission: %w", err)
	}
	return nil
}

func marshalExecutionPayload(payload map[string]json.RawMessage) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal execution payload: %w", err)
	}
	return raw, nil
}
