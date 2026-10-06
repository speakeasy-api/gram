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
func selectTurnUser(ctx context.Context, assistant assistantRecord, threadSource string, event assistantThreadEventRecord, lookup slackUserLookup) (string, error) {
	source := threadSource
	var metadata struct {
		Source string `json:"_gram_source_kind"`
	}
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &metadata); err == nil && metadata.Source != "" {
		source = metadata.Source
	}
	switch source {
	case sourceKindWake:
		var payload wakeEventPayload
		if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
			return "", fmt.Errorf("decode wake identity: %w", err)
		}
		switch payload.IdentityVersion {
		case 0:
			// Wakes scheduled before requesters were captured act as the owner.
		case wakeIdentityVersionRequester:
			if payload.RequesterUserID == "" {
				return "", errors.New("wake has no captured requester")
			}
			return payload.RequesterUserID, nil
		default:
			return "", fmt.Errorf("unsupported wake identity version %d", payload.IdentityVersion)
		}
	case sourceKindDashboard:
		var payload dashboardEventPayload
		if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
			return "", fmt.Errorf("decode dashboard identity: %w", err)
		}
		if payload.UserID != "" {
			return payload.UserID, nil
		}
	case sourceKindSlack:
		var payload slackEventPayload
		if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
			return "", fmt.Errorf("decode slack identity: %w", err)
		}
		if payload.TeamID != "" && payload.UserID != "" {
			user, err := lookup(ctx, slackrepo.ResolveSlackMappingUserParams{OrganizationID: assistant.OrganizationID, SlackTeamID: payload.TeamID, SlackUserID: payload.UserID})
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
