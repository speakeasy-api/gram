package platformmcp

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const operationChooseMCPHeadlessCallerCredential = "choose_mcp_headless_caller_credential" //nolint:gosec // G101 false positive: a tool name, not a credential.

type ChooseMCPHeadlessCallerCredentialInput struct {
	ProjectID           string `json:"project_id" jsonschema:"explicit project ID that owns the configured MCP"`
	MCPID               string `json:"mcp_id" jsonschema:"configured MCP ID returned by find_mcp"`
	UserSessionClientID string `json:"user_session_client_id,omitempty" jsonschema:"the OAuth client that signs in without a browser; or set workload_issuer_id and workload_subject instead"`
	WorkloadIssuerID    string `json:"workload_issuer_id,omitempty" jsonschema:"the workload issuer of a workload caller"`
	WorkloadSubject     string `json:"workload_subject,omitempty" jsonschema:"the exact workload subject of a workload caller"`
	AuthClientID        string `json:"auth_client_id,omitempty" jsonschema:"credential the server reports in upstream_authentication; workloads can use shared credentials only"`
	NoAuth              bool   `json:"no_auth,omitempty" jsonschema:"use no upstream authentication, for a server that needs none"`
	Remove              bool   `json:"remove,omitempty" jsonschema:"remove the caller's choice for this server"`
	Name                string `json:"name,omitempty" jsonschema:"name for a caller that has no profile yet"`
	Confirmed           bool   `json:"confirmed" jsonschema:"false previews the server's credentials and every caller's choice; true only after the user confirms this exact caller, server and choice"`
}

type MCPHeadlessCallerChoice struct {
	ProfileID           string `json:"profile_id"`
	Name                string `json:"name"`
	UserSessionClientID string `json:"user_session_client_id,omitempty"`
	WorkloadIssuerID    string `json:"workload_issuer_id,omitempty"`
	WorkloadSubject     string `json:"workload_subject,omitempty"`
	// Chosen reports that the caller has a choice for this server; without
	// one, its sessions take the server's only ready credential or must name
	// one.
	Chosen       bool   `json:"chosen"`
	AuthClientID string `json:"auth_client_id,omitempty"`
	NoAuth       bool   `json:"no_auth"`
}

type ChooseMCPHeadlessCallerCredentialOutput struct {
	UpstreamAuthentication *MCPUpstreamAuthentication `json:"upstream_authentication" jsonschema:"the credentials the server offers"`
	Callers                []MCPHeadlessCallerChoice  `json:"callers" jsonschema:"each headless caller's choice for this server"`
}

// ChooseHeadlessCallerCredential previews or changes the credential one
// headless caller's sessions use on one server: an app or workload that signs
// in without a browser and so cannot pick on the consent screen. A change
// replaces only this server's choice, through the same write the dashboard
// uses, and grants nothing: each issuance checks the caller may use it.
func (s *AccessRoleMutationService) ChooseHeadlessCallerCredential(ctx context.Context, principal Principal, input ChooseMCPHeadlessCallerCredentialInput) (ChooseMCPHeadlessCallerCredentialOutput, error) {
	var zero ChooseMCPHeadlessCallerCredentialOutput
	if !s.valid() || s.audit == nil {
		return zero, accessRoleMutationUnavailable(nil)
	}
	project, _, err := s.admit(ctx, principal, input.ProjectID)
	if err != nil {
		return zero, err
	}
	mcpID, err := uuid.Parse(strings.TrimSpace(input.MCPID))
	if err != nil {
		return zero, accessRoleMutationNotFound()
	}
	if _, err := platformrepo.New(s.reads.db).GetPlatformMCPInventoryItem(ctx, platformrepo.GetPlatformMCPInventoryItemParams{
		OrganizationID:       principal.OrganizationID,
		ConnectionID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ConnectionGeneration: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserID:               inventoryText(principal.UserID),
		ActingSurface:        inventoryText(string(principal.surface())),
		McpServerID:          mcpID,
		ProjectID:            project.ID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return zero, accessRoleMutationNotFound()
	} else if err != nil {
		return zero, accessRoleMutationUnavailable(err)
	}
	server, err := mcpserversrepo.New(s.reads.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: project.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, accessRoleMutationNotFound()
	} else if err != nil {
		return zero, accessRoleMutationUnavailable(err)
	}
	if !server.UserSessionIssuerID.Valid {
		return zero, accessRoleMutationInvalid("This server has no sign-in of its own, so no caller signs in to it without a browser.")
	}
	issuerID := server.UserSessionIssuerID.UUID

	if input.Confirmed {
		if err := s.writeHeadlessCallerChoice(ctx, principal, issuerID, mcpID, input); err != nil {
			return zero, err
		}
	}

	upstream, err := upstreamAuthentication(ctx, s.reads.db, principal.OrganizationID, mcpID)
	if err != nil {
		return zero, accessRoleMutationUnavailable(err)
	}
	profiles, err := authclients.ListIssuanceProfilesForIssuer(ctx, s.reads.db, principal.OrganizationID, issuerID)
	if err != nil {
		return zero, accessRoleMutationUnavailable(err)
	}
	out := ChooseMCPHeadlessCallerCredentialOutput{UpstreamAuthentication: upstream, Callers: []MCPHeadlessCallerChoice{}}
	for _, profile := range profiles {
		caller := MCPHeadlessCallerChoice{
			ProfileID: profile.ID, Name: profile.Name,
			UserSessionClientID: conv.PtrValOr(profile.UserSessionClientID, ""),
			WorkloadIssuerID:    conv.PtrValOr(profile.WorkloadIssuerID, ""),
			WorkloadSubject:     conv.PtrValOr(profile.WorkloadSubject, ""),
			Chosen:              false, AuthClientID: "", NoAuth: false,
		}
		for _, choice := range profile.Choices {
			if choice.McpServerID == mcpID.String() {
				caller.Chosen = true
				caller.AuthClientID = conv.PtrValOr(choice.AuthClientID, "")
				caller.NoAuth = choice.AuthClientID == nil
			}
		}
		out.Callers = append(out.Callers, caller)
	}
	return out, nil
}

func (s *AccessRoleMutationService) writeHeadlessCallerChoice(ctx context.Context, principal Principal, issuerID, mcpID uuid.UUID, input ChooseMCPHeadlessCallerCredentialInput) error {
	modes := 0
	for _, set := range []bool{input.AuthClientID != "", input.NoAuth, input.Remove} {
		if set {
			modes++
		}
	}
	if modes != 1 {
		return accessRoleMutationInvalid("Name exactly one of auth_client_id, no_auth or remove.")
	}
	clientID, err := optionalUUID(input.UserSessionClientID)
	if err != nil {
		return accessRoleMutationInvalid("user_session_client_id is not a valid ID.")
	}
	workloadIssuerID, err := optionalUUID(input.WorkloadIssuerID)
	if err != nil {
		return accessRoleMutationInvalid("workload_issuer_id is not a valid ID.")
	}
	var authClientID *string
	if input.AuthClientID != "" {
		if _, err := uuid.Parse(input.AuthClientID); err != nil {
			return accessRoleMutationInvalid("auth_client_id is not a valid ID.")
		}
		authClientID = &input.AuthClientID
	}
	if input.Remove {
		// Removing a choice from a caller with no profile changes nothing.
		profiles, err := authclients.ListIssuanceProfilesForIssuer(ctx, s.reads.db, principal.OrganizationID, issuerID)
		if err != nil {
			return accessRoleMutationUnavailable(err)
		}
		if !slices.ContainsFunc(profiles, func(p *types.IssuanceProfile) bool {
			if clientID.Valid {
				return conv.PtrValOr(p.UserSessionClientID, "") == clientID.UUID.String()
			}
			return conv.PtrValOr(p.WorkloadIssuerID, "") == workloadIssuerID.UUID.String() && conv.PtrValOr(p.WorkloadSubject, "") == input.WorkloadSubject
		}) {
			return nil
		}
	}
	if err := s.budget.AllowConnectionOrOrganization(ctx, principal); err != nil {
		if errors.Is(err, ErrOperationRateLimited) {
			return &AccessRoleMutationError{Code: "rate_limited", Message: "The access role mutation rate limit was reached.", Cause: err}
		}
		return accessRoleMutationUnavailable(err)
	}
	server := mcpID.String()
	_, err = authclients.WriteIssuanceProfile(ctx, s.reads.db, s.audit, authclients.IssuanceProfileWrite{
		OrganizationID: principal.OrganizationID, ActorUserID: principal.UserID, ActorDisplayName: nil,
		UserSessionIssuerID: issuerID, ClientID: clientID, WorkloadIssuerID: workloadIssuerID, WorkloadSubject: input.WorkloadSubject,
		Name: strings.TrimSpace(input.Name),
		Choices: func(current []*types.IssuanceProfileChoice) []*types.IssuanceProfileChoice {
			next := make([]*types.IssuanceProfileChoice, 0, len(current)+1)
			for _, choice := range current {
				if choice.McpServerID != server {
					next = append(next, choice)
				}
			}
			if !input.Remove {
				next = append(next, &types.IssuanceProfileChoice{McpServerID: server, AuthClientID: authClientID})
			}
			return next
		},
	})
	if err == nil {
		return nil
	}
	if shared, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shared.Code {
		case oops.CodeNotFound:
			return accessRoleMutationNotFound()
		case oops.CodeConflict:
			return accessRoleMutationConflict(shared.Error())
		default:
			return accessRoleMutationInvalid(shared.Error())
		}
	}
	return accessRoleMutationUnavailable(err)
}

func optionalUUID(value string) (uuid.NullUUID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, err //nolint:wrapcheck // parse error is mapped by the caller
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}
