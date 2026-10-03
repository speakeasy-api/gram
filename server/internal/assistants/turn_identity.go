package assistants

import (
	"context"
	"encoding/json"
	"fmt"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

type legacyWakeLookup func(context.Context, assistantrepo.FindLegacyWakeRequesterParams) ([]string, error)

type slackUserLookup func(context.Context, slackrepo.ResolveSlackMappingUserParams) (string, error)

// selectTurnUser resolves identity, not authority. Only unavailable Slack mappings
// fall back to the owner. A selected user's authorization failure must propagate.
func selectTurnUser(ctx context.Context, assistant assistantRecord, source string, event assistantThreadEventRecord, lookup slackUserLookup, legacyLookup legacyWakeLookup) (string, error) {
	var metadata struct {
		Source      string `json:"_gram_source_kind"`
		ScheduledAt string `json:"scheduled_at"`
	}
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &metadata); err != nil {
		return "", fmt.Errorf("decode turn identity: %w", err)
	}
	if metadata.Source != "" {
		source = metadata.Source
	} else if metadata.ScheduledAt != "" {
		// Historical wakes can share a Slack thread without stored source metadata.
		source = sourceKindWake
	}
	switch source {
	case sourceKindWake:
		var payload wakeEventPayload
		if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
			return "", fmt.Errorf("resolve turn identity: %w", err)
		}
		if payload.IdentityVersion != 0 && payload.IdentityVersion != 1 {
			return "", fmt.Errorf("unsupported wake identity version")
		}
		if payload.IdentityVersion == 1 {
			if payload.RequesterUserID == "" {
				return "", fmt.Errorf("wake has no captured requester")
			}
			return payload.RequesterUserID, nil
		}
		// Version zero has no captured-requester contract; ignore that field.
		// Only unversioned legacy wakes retain the old owner selection. Prefer an
		// unambiguous recorded scheduling actor, not a reconstructed historical user.
		if event.TriggerInstanceID.Valid && legacyLookup != nil {
			users, err := legacyLookup(ctx, assistantrepo.FindLegacyWakeRequesterParams{OrganizationID: assistant.OrganizationID, ProjectID: assistant.ProjectID, TriggerID: event.TriggerInstanceID.UUID.String()})
			if err == nil && len(users) == 1 {
				return users[0], nil
			}
		}

	case sourceKindDashboard:
		var payload dashboardEventPayload
		if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
			return "", fmt.Errorf("resolve turn identity: %w", err)
		}
		if payload.UserID != "" {
			return payload.UserID, nil
		}
	case sourceKindSlack:
		var payload slackEventPayload
		if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
			return "", fmt.Errorf("resolve turn identity: %w", err)
		}
		if payload.TeamID != "" && payload.UserID != "" && lookup != nil {
			user, err := lookup(ctx, slackrepo.ResolveSlackMappingUserParams{OrganizationID: assistant.OrganizationID, SlackTeamID: payload.TeamID, SlackUserID: payload.UserID})
			if err == nil && user != "" {
				return user, nil
			}
		}
	}
	if assistant.CreatedByUserID == "" {
		return "", fmt.Errorf("assistant owner is unavailable")
	}
	return assistant.CreatedByUserID, nil
}

func (s *ServiceCore) turnUserID(ctx context.Context, assistant assistantRecord, thread assistantThreadRecord, event assistantThreadEventRecord) (string, error) {
	user, err := selectTurnUser(ctx, assistant, thread.SourceKind, event, slackrepo.New(s.db).ResolveSlackMappingUser, assistantrepo.New(s.db).FindLegacyWakeRequester)
	if err != nil {
		return "", fmt.Errorf("resolve turn identity: %w", err)
	}
	active, err := orgrepo.New(s.db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{OrganizationID: assistant.OrganizationID, UserID: user})
	if err != nil {
		return "", fmt.Errorf("check turn user eligibility: %w", err)
	}
	if !active {
		return "", fmt.Errorf("turn user is not an active organization member")
	}
	// Resolve the selected identity's current grants, never the transport actor's
	// grants. Trusted event provenance establishes identity, not project access.
	principals, err := authz.ResolveUserPrincipals(ctx, s.db, assistant.OrganizationID, user)
	if err != nil {
		return "", fmt.Errorf("resolve turn user principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, s.db, assistant.OrganizationID, principals)
	if err != nil {
		return "", fmt.Errorf("load turn user grants: %w", err)
	}
	allowed, err := authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: assistant.ProjectID.String(), Dimensions: nil})
	if err != nil {
		return "", fmt.Errorf("check turn user project access: %w", err)
	}
	if !allowed {
		return "", fmt.Errorf("turn user does not have access to assistant project")
	}
	return user, nil
}
