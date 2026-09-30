package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// ConsentBindingService is the existing attachment management API. Its
// transactional ownership, lifecycle and audit checks also apply to consent.
type ConsentBindingService interface {
	ListRemoteSessions(context.Context, *gen.ListRemoteSessionsPayload) (*gen.ListRemoteSessionsResult, error)
	ListBindings(context.Context, *gen.ListBindingsPayload) (*gen.ListBindingsResult, error)
	AttachBinding(context.Context, *gen.AttachBindingPayload) (*gen.PrincipalRemoteSessionBinding, error)
	DetachBinding(context.Context, *gen.DetachBindingPayload) error
}

func optionalConsentCursor(cursor string) *string {
	if cursor == "" {
		return nil
	}
	return &cursor
}

func (s *Service) SetConsentBindingService(bindings ConsentBindingService) {
	s.consentBindings = bindings
}

// Runs after endpoint, browser, challenge and CSRF checks. Reuse the human
// already resolved by OAuth, without pretending it is a dashboard session.
func (s *Service) serveConsentAgentConnections(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)
	target := state.AgentAuthorizationTarget
	if target == nil || !target.matches(endpoint) {
		return oops.E(oops.CodeForbidden, nil, "agent authorization target does not match endpoint")
	}
	human, err := s.loadConsentHuman(ctx, state, *target)
	if err != nil {
		return consentAgentAuthorizationError(err, "consent authorizer is not eligible")
	}
	if enabled, _, _ := s.agentAuthorizationRollout(ctx, logger, endpoint); !enabled {
		return oops.C(oops.CodeNotFound)
	}
	action := r.PostForm.Get("action")
	var result any
	if s.consentBindings == nil {
		return oops.C(oops.CodeUnavailable)
	}
	var selected *AgentAuthorizationResult
	if action == "agent_detach" {
		id, err := uuid.Parse(r.PostForm.Get("agent_id"))
		if err != nil || id == uuid.Nil {
			return oops.C(oops.CodeBadRequest)
		}
		selected = &AgentAuthorizationResult{AgentID: id, AuthorizerUserID: state.AuthorizerUserID, Target: AgentAuthorizationTarget{Scope: "", OrganizationID: "", ProjectID: uuid.Nil, UserSessionIssuerID: uuid.Nil, MCPResourceID: uuid.Nil}}
	} else {
		selected, err = s.authorizeConsentAgent(ctx, state, endpoint, r.PostForm.Get("agent_id"))
		if err != nil {
			return consentAgentAuthorizationError(err, "selected agent is not eligible")
		}
	}
	// This trust is scoped to attachment management for this exact target. The
	// shared binding authorizer still locks membership/ownership and reloads grants.
	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, endpoint.OrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "load consent organization")
	}
	ctx = contextvalues.WithConsentBindingAuthorization(ctx, human.userID, endpoint.OrganizationID, endpoint.ProjectID, selected.AgentID, endpoint.UserSessionIssuerID)
	// Preserve authoritative feature-flag groups for the shared binding authorizer.
	auth, _ := contextvalues.GetAuthContext(ctx)
	auth.OrganizationSlug = organization.Slug
	principal, issuer := selected.AgentID.String(), endpoint.UserSessionIssuerID.String()
	switch action {
	case "agent_connections":
		candidates, err := s.consentBindings.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{SubjectUrn: nil, RemoteSessionClientID: nil, Limit: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, PrincipalID: &principal, UserSessionIssuerID: &issuer, Cursor: optionalConsentCursor(r.PostForm.Get("cursor"))})
		if err != nil {
			return fmt.Errorf("manage consent connection: %w", err)
		}
		bindings, err := s.consentBindings.ListBindings(ctx, &gen.ListBindingsPayload{SessionToken: nil, ProjectSlugInput: nil, PrincipalID: principal, UserSessionIssuerID: issuer})
		if err != nil {
			return fmt.Errorf("manage consent connection: %w", err)
		}
		result = struct {
			Candidates []*types.RemoteSession               `json:"candidates"`
			Bindings   []*gen.PrincipalRemoteSessionBinding `json:"bindings"`
			NextCursor *string                              `json:"nextCursor,omitempty"`
		}{Candidates: candidates.Items, Bindings: bindings.Items, NextCursor: candidates.NextCursor}
	case "agent_attach":
		result, err = s.consentBindings.AttachBinding(ctx, &gen.AttachBindingPayload{SessionToken: nil, ProjectSlugInput: nil, PrincipalID: principal, UserSessionIssuerID: issuer, RemoteSessionID: r.PostForm.Get("remote_session_id")})
		if err != nil {
			return fmt.Errorf("manage consent connection: %w", err)
		}
	case "agent_detach":
		if err := s.consentBindings.DetachBinding(ctx, &gen.DetachBindingPayload{SessionToken: nil, ProjectSlugInput: nil, PrincipalID: principal, UserSessionIssuerID: issuer, ID: r.PostForm.Get("binding_id")}); err != nil {
			return fmt.Errorf("manage consent connection: %w", err)
		}
		result = struct{}{}
	default:
		return oops.C(oops.CodeBadRequest)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write agent connections")
	}
	return nil
}
