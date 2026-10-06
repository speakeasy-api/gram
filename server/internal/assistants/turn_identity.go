package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/authz"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

// ErrTurnIdentity signals that a turn's identity cannot be selected or is not
// allowed to act. Replaying the event selects the same identity, so callers
// fail the event terminally instead of retrying it.
var ErrTurnIdentity = errors.New("assistant turn identity rejected")

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

// selectTurnUser picks the user a turn of an agent-backed assistant acts
// under. Only an unmapped Slack sender falls back to the owner; a selected
// user who is later denied is never retried as the owner.
//
// Fields are read by exact key. Ingress strips every spelling of the
// identity keys that JSON decoding would fold onto them, and only the server
// writes the exact ones.
func selectTurnUser(ctx context.Context, assistant assistantRecord, threadSource string, event assistantThreadEventRecord, lookup slackUserLookup) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &fields); err != nil {
		fields = nil
	}
	source := threadSource
	if stamped, err := exactField[string](fields, eventSourceKindKey); err != nil {
		return "", err
	} else if stamped != "" {
		source = stamped
	}
	switch source {
	case sourceKindWake:
		version, err := exactField[int](fields, wakeIdentityVersionKey)
		if err != nil {
			return "", err
		}
		switch version {
		case 0:
			// Wakes scheduled before requesters were captured act as the owner.
		case wakeIdentityVersionRequester:
			requester, err := exactField[string](fields, wakeRequesterUserIDKey)
			if err != nil {
				return "", err
			}
			if requester == "" {
				return "", errors.New("wake has no captured requester")
			}
			return requester, nil
		default:
			return "", fmt.Errorf("unsupported wake identity version %d", version)
		}
	case sourceKindDashboard:
		sender, err := exactField[string](fields, "user_id")
		if err != nil {
			return "", err
		}
		if sender != "" {
			return sender, nil
		}
	case sourceKindSlack:
		team, err := exactField[string](fields, "team_id")
		if err != nil {
			return "", err
		}
		sender, err := exactField[string](fields, "user_id")
		if err != nil {
			return "", err
		}
		if team != "" && sender != "" {
			user, err := lookup(ctx, slackrepo.ResolveSlackMappingUserParams{OrganizationID: assistant.OrganizationID, SlackTeamID: team, SlackUserID: sender})
			if err == nil && user != "" {
				return user, nil
			}
		}
	}
	if assistant.CreatedByUserID == "" {
		return "", errors.New("assistant owner is unavailable")
	}
	return assistant.CreatedByUserID, nil
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

// turnUserID returns the user a turn acts under. Identity failures wrap
// ErrTurnIdentity; storage failures do not.
func (s *ServiceCore) turnUserID(ctx context.Context, assistant assistantRecord, thread assistantThreadRecord, event assistantThreadEventRecord) (string, error) {
	states, err := assistantidentity.States(ctx, s.db, assistant.ProjectID, []uuid.UUID{assistant.ID})
	if err != nil {
		return "", fmt.Errorf("load turn identity state: %w", err)
	}
	switch states[assistant.ID].State {
	case assistantidentity.NeverConfigured:
		return legacyTurnUserID(assistant, thread, event), nil
	case assistantidentity.Active:
	default:
		return "", fmt.Errorf("%w: assistant agent is not active", ErrTurnIdentity)
	}

	user, err := selectTurnUser(ctx, assistant, thread.SourceKind, event, slackrepo.New(s.db).ResolveSlackMappingUser)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrTurnIdentity, err)
	}
	active, err := orgrepo.New(s.db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{OrganizationID: assistant.OrganizationID, UserID: user})
	if err != nil {
		return "", fmt.Errorf("check turn user membership: %w", err)
	}
	if !active {
		return "", fmt.Errorf("%w: turn user is not an active organization member", ErrTurnIdentity)
	}
	// The selected user's own grants decide project access, never the grants
	// of whatever transport delivered the event.
	principals, err := authz.ResolveUserPrincipals(ctx, s.db, assistant.OrganizationID, user)
	if err != nil {
		return "", fmt.Errorf("resolve turn user principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, s.db, assistant.OrganizationID, principals)
	if err != nil {
		return "", fmt.Errorf("load turn user grants: %w", err)
	}
	check := authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: assistant.ProjectID.String(), Dimensions: nil}
	if err := s.authz.EvaluateLoadedGrants(ctx, grants, check); err != nil {
		return "", fmt.Errorf("%w: turn user cannot read the assistant project: %w", ErrTurnIdentity, err)
	}
	return user, nil
}
