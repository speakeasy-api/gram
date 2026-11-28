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

// errAssistantOwnerUnavailable is returned when an automated turn falls back
// to an assistant that has no recorded owner.
var errAssistantOwnerUnavailable = errors.New("assistant owner is unavailable")

// wakeIdentityVersionRequester marks wakes that captured their requester when
// they were scheduled.
const wakeIdentityVersionRequester = 1

type slackUserLookup func(context.Context, slackrepo.ResolveSlackMappingUserParams) (string, error)

// Canonical values for assistant_ate_actors.initiating_user_resolution.
// Go constants are the single source of truth (no Postgres CHECK enum).
const (
	// ATEInitiatorNotRecorded is the default marker for rows that have no
	// recorded initiator. New actor writes never persist this marker and
	// authorization fails closed on it.
	ATEInitiatorNotRecorded = "not_recorded"

	// ATEInitiatorNoUser explicitly records that a thread started without an
	// attributable human initiator (e.g. cron, webhook, unmapped non-Speakeasy
	// Slack, or unversioned wake). initiating_user_id is NULL.
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

// selectTurnUser picks the user a turn acts under, and whether that user was
// identified by the event rather than taken as the owner. Only an unmapped
// Slack sender falls back to the owner; a selected user who is later denied is
// never retried as the owner.
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
		return "", false, "", errAssistantOwnerUnavailable
	}
	return assistant.CreatedByUserID, false, ATEInitiatorNoUser, nil
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

// turnIdentity is who a turn acts as. HumanKnown reports whether the event
// identified UserID rather than UserID being the owner fallback; Initiator is
// the server-resolved initiating-user record persisted once on ATE thread
// actor creation.
type turnIdentity struct {
	UserID     string
	HumanKnown bool
	Initiator  ateThreadInitiator
}

// resolveTurnIdentity returns who a turn acts as. Identity failures wrap
// ErrTurnIdentity; storage failures do not.
func (s *ServiceCore) resolveTurnIdentity(ctx context.Context, assistant assistantRecord, thread assistantThreadRecord, event assistantThreadEventRecord) (turnIdentity, error) {
	states, err := assistantidentity.States(ctx, s.db, assistant.ProjectID, []uuid.UUID{assistant.ID})
	if err != nil {
		return turnIdentity{}, fmt.Errorf("load turn identity state: %w", err)
	}
	switch states[assistant.ID].State {
	case assistantidentity.Active:
	case assistantidentity.NeverConfigured:
		return turnIdentity{}, fmt.Errorf("%w: assistant agent identity is not configured", ErrTurnIdentity)
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
	return turnIdentity{UserID: user, HumanKnown: known, Initiator: initiator}, nil
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
