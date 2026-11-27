package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

// ErrTurnIdentity signals that a turn's identity cannot be selected or is not
// allowed to act. Replaying the event selects the same identity, so callers
// fail the event terminally instead of retrying it.
var ErrTurnIdentity = errors.New("assistant turn identity rejected")

// ErrSlackSenderUnlinked means a Speakeasy Slack app sender is not linked to a
// Speakeasy user.
var ErrSlackSenderUnlinked = errors.New("slack sender is not linked to a Speakeasy user")

// wakeIdentityVersionRequester marks wakes that captured their requester when
// they were scheduled.
const wakeIdentityVersionRequester = 1

type slackUserLookup func(context.Context, slackrepo.ResolveSlackMappingUserParams) (string, error)

// legacyTurnUserID is the user a turn acts under for an assistant without a
// dedicated agent. Dashboard turns act as the sender carried on the payload;
// every other source acts as the assistant's creator, which may be empty.
func legacyTurnUserID(assistant assistantRecord, thread assistantThreadRecord, event assistantThreadEventRecord) string {
	if thread.SourceKind == sourceKindDashboard {
		var payload dashboardEventPayload
		if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err == nil && payload.UserID != "" {
			return payload.UserID
		}
	}
	return assistant.CreatedByUserID
}

// Canonical values for assistant_ate_actors.initiating_user_resolution.
// Go constants are the single source of truth (no Postgres CHECK enum).
const (
	// ATEInitiatorNotRecorded is the schema default on legacy pre-AIM-1217 rows.
	// New actor writes never persist this marker; AIM-1218 fails closed on it.
	ATEInitiatorNotRecorded = "not_recorded"

	// ATEInitiatorNoUser explicitly records that a thread started without an
	// attributable human initiator (e.g. cron, webhook, unmapped non-Speakeasy
	// Slack, or legacy wake). initiating_user_id is NULL.
	ATEInitiatorNoUser = "no_user"

	// ATEInitiatorDashboardSender records a dashboard message sender.
	ATEInitiatorDashboardSender = "dashboard_sender"

	// ATEInitiatorSlackMapping records a Slack user mapped to a Speakeasy user.
	ATEInitiatorSlackMapping = "slack_mapping"

	// ATEInitiatorWakeRequester records a scheduled wake's captured requester.
	ATEInitiatorWakeRequester = "wake_requester"

	// ATEInitiatorMCPAuthResume records the user whose turn initiated an MCP OAuth continuation.
	ATEInitiatorMCPAuthResume = "mcp_auth_resume"
)

// ateThreadInitiator captures the server-resolved initiating user recorded once
// when a thread's first ATE actor is created and inherited immutably by any
// replacement actors for that thread.
type ateThreadInitiator struct {
	UserID     string
	Resolution string
}

func validateATEThreadInitiator(initiator ateThreadInitiator) error {
	switch initiator.Resolution {
	case ATEInitiatorNoUser:
		if initiator.UserID != "" {
			return fmt.Errorf("invalid ATE thread initiator: resolution %q must not carry a user ID", initiator.Resolution)
		}
		return nil
	case ATEInitiatorDashboardSender, ATEInitiatorSlackMapping, ATEInitiatorWakeRequester, ATEInitiatorMCPAuthResume:
		if initiator.UserID == "" {
			return fmt.Errorf("invalid ATE thread initiator: resolution %q requires a user ID", initiator.Resolution)
		}
		return nil
	default:
		return fmt.Errorf("invalid ATE thread initiator resolution %q", initiator.Resolution)
	}
}

// selectTurnUser picks the user a turn of an agent-backed assistant acts
// under, and whether that user was identified by the event rather than taken
// as the owner. Only an unmapped Slack sender falls back to the owner; a selected
// user who is later denied is never retried as the owner.
//
// Fields are read by exact key. Ingress strips every spelling of the
// identity keys that JSON decoding would fold onto them, and only the server
// writes the exact ones.
func selectTurnUser(ctx context.Context, assistant assistantRecord, threadSource string, event assistantThreadEventRecord, lookup slackUserLookup) (string, bool, error) {
	user, known, _, err := selectTurnUserWithResolution(ctx, assistant, threadSource, event, lookup)
	return user, known, err
}

func selectTurnUserWithResolution(ctx context.Context, assistant assistantRecord, threadSource string, event assistantThreadEventRecord, lookup slackUserLookup) (string, bool, string, error) {
	if !json.Valid(event.NormalizedPayloadJSON) {
		return "", false, "", errors.New("turn payload is not valid JSON")
	}
	// A valid non-object payload carries no identity fields and is never stamped.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &fields); err != nil {
		fields = nil
	}
	if kind, err := exactField[string](fields, mcpAuthEventKindKey); err != nil {
		return "", false, "", err
	} else if kind == mcpAuthEventKind {
		resume, err := exactField[string](fields, mcpAuthResumeUserIDKey)
		if err != nil {
			return "", false, "", err
		}
		if resume != "" {
			// An OAuth continuation acts as the user whose turn started it.
			return resume, true, ATEInitiatorMCPAuthResume, nil
		}
	}
	source := threadSource
	if stamped, err := exactField[string](fields, eventSourceKindKey); err != nil {
		return "", false, "", err
	} else if stamped != "" {
		source = stamped
	}
	switch source {
	case sourceKindWake:
		version, err := exactField[int](fields, wakeIdentityVersionKey)
		if err != nil {
			return "", false, "", err
		}
		switch version {
		case 0:
			// Wakes scheduled before requesters were captured act as the owner.
		case wakeIdentityVersionRequester:
			requester, err := exactField[string](fields, wakeRequesterUserIDKey)
			if err != nil {
				return "", false, "", err
			}
			if requester == "" {
				return "", false, "", errors.New("wake has no captured requester")
			}
			return requester, true, ATEInitiatorWakeRequester, nil
		default:
			return "", false, "", fmt.Errorf("unsupported wake identity version %d", version)
		}
	case sourceKindDashboard:
		sender, err := exactField[string](fields, "user_id")
		if err != nil {
			return "", false, "", err
		}
		if sender != "" {
			return sender, true, ATEInitiatorDashboardSender, nil
		}
	case sourceKindSlack, sourceKindSpeakeasySlack:
		team, err := exactField[string](fields, "team_id")
		if err != nil {
			return "", false, "", err
		}
		sender, err := exactField[string](fields, "user_id")
		if err != nil {
			return "", false, "", err
		}
		if team != "" && sender != "" {
			user, err := lookup(ctx, slackrepo.ResolveSlackMappingUserParams{OrganizationID: assistant.OrganizationID, SlackTeamID: team, SlackUserID: sender})
			if err == nil && user != "" {
				return user, true, ATEInitiatorSlackMapping, nil
			}
		}
		// The Speakeasy Slack app answers anyone in the workspace, so a turn
		// must never fall back to the owner's permissions.
		if source == sourceKindSpeakeasySlack {
			return "", false, "", ErrSlackSenderUnlinked
		}
	}
	if assistant.CreatedByUserID == "" {
		return "", false, "", errors.New("assistant owner is unavailable")
	}
	return assistant.CreatedByUserID, false, ATEInitiatorNoUser, nil
}

// turnEventSource returns the source an event's turn is selected by: the
// server-stamped source on the event when present, otherwise the thread's.
func turnEventSource(threadSource string, payload []byte) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return threadSource, nil //nolint:nilerr // non-object payloads are never stamped
	}
	stamped, err := exactField[string](fields, eventSourceKindKey)
	if err != nil {
		return "", err
	}
	if stamped != "" {
		return stamped, nil
	}
	return threadSource, nil
}

// exactField decodes fields[key], matching the key exactly. A missing key or
// JSON null yields the zero value.
func exactField[T any](fields map[string]json.RawMessage, key string) (T, error) {
	var value T
	raw, ok := fields[key]
	if !ok {
		return value, nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode turn identity field %s: %w", key, err)
	}
	return value, nil
}

// turnIdentity is who a turn acts as. AgentBacked turns run as the
// assistant's agent; HumanKnown says the event identified UserID rather than
// UserID being the owner it falls back to; Initiator is the server-resolved
// initiating-user record persisted once on ATE thread actor creation.
type turnIdentity struct {
	UserID      string
	AgentBacked bool
	HumanKnown  bool
	Initiator   ateThreadInitiator
}

// resolveTurnIdentity returns who a turn acts as. Identity failures wrap
// ErrTurnIdentity; storage failures do not.
func (s *ServiceCore) resolveTurnIdentity(ctx context.Context, assistant assistantRecord, thread assistantThreadRecord, event assistantThreadEventRecord) (turnIdentity, error) {
	states, err := assistantidentity.States(ctx, s.db, assistant.ProjectID, []uuid.UUID{assistant.ID})
	if err != nil {
		return turnIdentity{}, fmt.Errorf("load turn identity state: %w", err)
	}
	switch states[assistant.ID].State {
	case assistantidentity.NeverConfigured:
		// The event's stamped source decides, because a Speakeasy Slack app
		// event can land on a thread another source started, and a later event
		// on a Speakeasy Slack thread must not fall back to the owner either.
		eventSource, err := turnEventSource(thread.SourceKind, event.NormalizedPayloadJSON)
		if err != nil {
			return turnIdentity{}, fmt.Errorf("%w: %w", ErrTurnIdentity, err)
		}
		if thread.SourceKind != sourceKindSpeakeasySlack && eventSource != sourceKindSpeakeasySlack {
			initiator := ateThreadInitiator{UserID: "", Resolution: ATEInitiatorNoUser}
			if thread.SourceKind == sourceKindDashboard {
				var payload dashboardEventPayload
				if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err == nil && payload.UserID != "" {
					initiator = ateThreadInitiator{UserID: payload.UserID, Resolution: ATEInitiatorDashboardSender}
				}
			}
			return turnIdentity{UserID: legacyTurnUserID(assistant, thread, event), AgentBacked: false, HumanKnown: false, Initiator: initiator}, nil
		}
		// Speakeasy Slack app turns act as the linked sender even without a
		// dedicated agent.
		user, resolution, err := s.checkedTurnUser(ctx, assistant, thread, event)
		if err != nil {
			return turnIdentity{}, err
		}
		return turnIdentity{
			UserID:      user,
			AgentBacked: false,
			HumanKnown:  true,
			Initiator:   ateThreadInitiator{UserID: user, Resolution: resolution},
		}, nil
	case assistantidentity.Active:
	default:
		return turnIdentity{}, fmt.Errorf("%w: assistant agent is not active", ErrTurnIdentity)
	}

	user, known, resolution, err := selectTurnUserWithResolution(ctx, assistant, thread.SourceKind, event, slackrepo.New(s.db).ResolveSlackMappingUser)
	if err != nil {
		return turnIdentity{}, fmt.Errorf("%w: %w", ErrTurnIdentity, err)
	}
	if err := s.checkTurnActor(ctx, assistant, user); err != nil {
		return turnIdentity{}, err
	}
	initiator := ateThreadInitiator{UserID: "", Resolution: ATEInitiatorNoUser}
	if known {
		initiator = ateThreadInitiator{UserID: user, Resolution: resolution}
	}
	return turnIdentity{UserID: user, AgentBacked: true, HumanKnown: known, Initiator: initiator}, nil
}

// checkedTurnUser selects the turn user and checks it may act in the project.
func (s *ServiceCore) checkedTurnUser(ctx context.Context, assistant assistantRecord, thread assistantThreadRecord, event assistantThreadEventRecord) (string, string, error) {
	user, known, resolution, err := selectTurnUserWithResolution(ctx, assistant, thread.SourceKind, event, slackrepo.New(s.db).ResolveSlackMappingUser)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrTurnIdentity, err)
	}
	if err := s.checkTurnActor(ctx, assistant, user); err != nil {
		return "", "", err
	}
	if !known {
		resolution = ATEInitiatorNoUser
	}
	return user, resolution, nil
}

func (s *ServiceCore) checkTurnActor(ctx context.Context, assistant assistantRecord, user string) error {
	if err := assistantidentity.CheckActor(ctx, s.db, s.authz, assistant.OrganizationID, assistant.ProjectID, user); err != nil {
		if errors.Is(err, assistantidentity.ErrActorIneligible) {
			return fmt.Errorf("%w: turn user: %w", ErrTurnIdentity, err)
		}
		return fmt.Errorf("check turn user: %w", err)
	}
	return nil
}
