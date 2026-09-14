package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

// Runs after endpoint, challenge and CSRF checks. Management additionally
// requires a real Gram session; an OIDC challenge is not a dashboard session.
func (s *Service) serveConsentAgentConnections(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)
	cookie, err := r.Cookie(constants.SessionCookie)
	if err != nil || cookie.Value == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	ctx, err = s.sessions.Authenticate(ctx, cookie.Value)
	if err != nil {
		return fmt.Errorf("manage consent connection: %w", err)
	}
	if enabled, _ := s.agentAuthorizationRollout(ctx, logger, endpoint); !enabled {
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
			return oops.E(oops.CodeForbidden, err, "selected agent is not eligible")
		}
	}
	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || auth.UserID != selected.AuthorizerUserID || auth.ActiveOrganizationID != endpoint.OrganizationID {
		return oops.E(oops.CodeForbidden, nil, "sign in to Gram as the consent authorizer in this organization")
	}
	// Match the RPC middleware's project check using only the resolved endpoint.
	scoped := *auth
	scoped.ProjectID = &endpoint.ProjectID
	ctx = contextvalues.SetAuthContext(ctx, &scoped)
	// Consent routes do not run the RPC middleware that prepares session grants.
	ctx, err = s.authz.PrepareContext(ctx)
	if err != nil {
		return fmt.Errorf("manage consent connection: %w", err)
	}
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
