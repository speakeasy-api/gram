package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

//go:embed postlogin_delegation_template.html
var postLoginDelegationHTML string
var postLoginDelegationTemplate = template.Must(template.Must(template.New("delegation").Parse(postLoginDelegationHTML)).Parse(consentpage.LogoTemplate))

type exactGrantDelegator interface {
	DelegateGrant(context.Context, uuid.UUID, remotesessions.RemoteGrant, uuid.UUID) error
}
type delegationOption struct {
	ID, Name, BindingID, Detail string
	Disabled                    bool
}
type postLoginDelegationData struct {
	AccountName, AccountEmail                                             string
	Styles                                                                template.CSS
	ScriptURL, ActionURL, ContinueURL, State, CSRF, SessionID, Generation string
	Options                                                               []delegationOption
}

func (s *Service) delegationHuman(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (consentHumanAuthorization, error) {
	if !state.FirstParty || state.BindingRefreshAttemptID != "" || state.WorkloadReconnect != nil || state.PendingDelegation == nil || state.Subject == nil || state.PendingDelegation.Subject != *state.Subject || state.PendingDelegation.UserSessionIssuerID != endpoint.UserSessionIssuerID {
		return consentHumanAuthorization{}, oops.C(oops.CodeForbidden)
	}
	return s.loadConsentHuman(ctx, state, AgentAuthorizationTarget{Scope: authz.ScopeAgentWrite, OrganizationID: endpoint.OrganizationID, ProjectID: endpoint.ProjectID, UserSessionIssuerID: endpoint.UserSessionIssuerID, MCPResourceID: uuid.Nil})
}

func consentBindingContext(ctx context.Context, endpoint *ResolvedMcpEndpoint, user string, agent uuid.UUID, organizationSlug string) context.Context {
	ctx = contextvalues.WithConsentBindingAuthorization(ctx, user, endpoint.OrganizationID, endpoint.ProjectID, agent, endpoint.UserSessionIssuerID)
	auth, _ := contextvalues.GetAuthContext(ctx)
	auth.OrganizationSlug = organizationSlug
	return ctx
}

func (s *Service) servePostLoginDelegation(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (bool, error) {
	if state.PendingDelegation == nil {
		return false, nil
	}
	if _, ok := s.consentBindings.(exactGrantDelegator); !ok {
		return false, nil
	}
	human, err := s.delegationHuman(r.Context(), endpoint, state)
	if err != nil {
		return true, consentAgentAuthorizationError(err, "connection delegation is unavailable")
	}
	candidates, err := agentsrepo.New(s.db).ListActiveAgentsForAuthorization(r.Context(), endpoint.OrganizationID)
	if err != nil {
		return true, fmt.Errorf("list delegation agents: %w", err)
	}
	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(r.Context(), endpoint.OrganizationID)
	if err != nil {
		return true, fmt.Errorf("load delegation organization: %w", err)
	}
	options := make([]delegationOption, 0)
	for _, agent := range candidates {
		if !agent.ProjectID.Valid || agent.ProjectID.UUID != endpoint.ProjectID {
			continue
		}
		if agent.OwnerUserID != human.userID && !authz.GrantsSatisfy(human.grants, authz.Check{Scope: authz.ScopeAgentWrite, ResourceKind: authz.ResourceKindAgent, ResourceID: agent.ID.String(), Dimensions: nil}) {
			continue
		}
		ctx := consentBindingContext(r.Context(), endpoint, human.userID, agent.ID, organization.Slug)
		bindings, err := s.consentBindings.ListBindings(ctx, &gen.ListBindingsPayload{SessionToken: nil, ProjectSlugInput: nil, PrincipalID: agent.ID.String(), UserSessionIssuerID: endpoint.UserSessionIssuerID.String()})
		if err != nil {
			return true, fmt.Errorf("load agent assignments: %w", err)
		}
		option := delegationOption{ID: agent.ID.String(), Name: agent.Name, BindingID: "", Detail: "Give this agent access to your new connection", Disabled: false}
		for _, binding := range bindings.Items {
			if binding.RemoteSessionClientID != state.PendingDelegation.RemoteSessionClientID.String() {
				continue
			}
			option.BindingID = binding.ID
			if binding.RemoteSessionID != state.PendingDelegation.RemoteSessionID.String() {
				option.Disabled = true
				option.Detail = "Another connection is assigned. Manage replacements in the agent editor."
			} else if binding.RemoteSession != nil {
				option.Disabled = true
				option.Detail = "Already using this connection"
			} else {
				option.Detail = "Renew this agent's previous connection"
			}
		}
		options = append(options, option)
	}
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].Disabled != options[j].Disabled {
			return !options[i].Disabled
		}
		return !options[i].Disabled && options[i].BindingID != "" && options[j].BindingID == ""
	})
	if len(options) == 0 {
		return false, nil
	}
	data := postLoginDelegationData{AccountName: state.PendingDelegation.UpstreamDisplayName, AccountEmail: state.PendingDelegation.UpstreamEmail, Styles: consentpage.Styles, ScriptURL: consentScriptURL, ActionURL: endpoint.consentPath() + "/remote-session", ContinueURL: endpoint.consentPath() + "?state=" + url.QueryEscape(state.ID), State: state.ID, CSRF: state.CSRFToken, SessionID: state.PendingDelegation.RemoteSessionID.String(), Generation: strconv.FormatInt(state.PendingDelegation.GrantGeneration, 10), Options: options}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	if err := postLoginDelegationTemplate.Execute(w, data); err != nil {
		return true, fmt.Errorf("render connection delegation: %w", err)
	}
	return true, nil
}

func (s *Service) servePostLoginDelegationAction(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	human, err := s.delegationHuman(r.Context(), endpoint, state)
	if err != nil {
		return consentAgentAuthorizationError(err, "connection delegation is unavailable")
	}
	grant := state.PendingDelegation
	if r.PostForm.Get("remote_session_id") != grant.RemoteSessionID.String() || r.PostForm.Get("grant_generation") != strconv.FormatInt(grant.GrantGeneration, 10) {
		return oops.E(oops.CodeConflict, nil, "connection changed; reload before choosing agents")
	}
	if r.PostForm.Get("action") == "finish_delegation" {
		completed := state
		completed.PendingDelegation = nil
		swapped, err := s.authnChallengeCache.CompareAndSwap(r.Context(), state, completed)
		if err != nil {
			return fmt.Errorf("finish connection delegation: %w", err)
		}
		if !swapped {
			return oops.C(oops.CodeConflict)
		}
	} else {
		delegator, ok := s.consentBindings.(exactGrantDelegator)
		if !ok {
			return oops.C(oops.CodeUnavailable)
		}
		agentID, err := uuid.Parse(r.PostForm.Get("agent_id"))
		if err != nil {
			return oops.C(oops.CodeBadRequest)
		}
		bindingID := uuid.Nil
		if raw := r.PostForm.Get("binding_id"); raw != "" {
			bindingID, err = uuid.Parse(raw)
			if err != nil {
				return oops.C(oops.CodeBadRequest)
			}
		}
		organization, err := orgrepo.New(s.db).GetOrganizationMetadata(r.Context(), endpoint.OrganizationID)
		if err != nil {
			return fmt.Errorf("load delegation organization: %w", err)
		}
		ctx := consentBindingContext(r.Context(), endpoint, human.userID, agentID, organization.Slug)
		if err := delegator.DelegateGrant(ctx, agentID, *grant, bindingID); err != nil {
			return fmt.Errorf("delegate connection: %w", err)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(struct {
		OK bool `json:"ok"`
	}{OK: true}); err != nil {
		return fmt.Errorf("write delegation result: %w", err)
	}
	return nil
}
