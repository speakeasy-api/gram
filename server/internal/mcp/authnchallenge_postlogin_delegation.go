package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

//go:embed postlogin_delegation_template.html
var postLoginDelegationHTML string

//go:embed postlogin_delegation_fields.html
var postLoginDelegationFields string
var postLoginDelegationTemplate = template.Must(template.Must(template.New("delegation").Parse(postLoginDelegationHTML)).Parse(consentpage.LogoTemplate + postLoginDelegationFields))

type exactGrantDelegator interface {
	DelegateGrant(context.Context, uuid.UUID, remotesessions.RemoteGrant, uuid.UUID) error
}
type delegationOption struct {
	ID, Name, BindingID, Detail string
	Disabled                    bool
	Existing                    bool
	Selected                    bool
	Expiry                      string
	ExpiryExact                 string
	PermissionSummary           string
	PreviousScopes              []string
	Scopes                      []delegationScope
	ComparisonKnown             bool
}
type delegationScope struct {
	Name  string
	Added bool
}
type postLoginDelegationData struct {
	PreviousScopes    []string
	HasPreviousScopes bool
	Scopes            []delegationScope

	AccountName, AccountEmail                                             string
	Styles                                                                template.CSS
	ScriptURL, ActionURL, ContinueURL, State, CSRF, SessionID, Generation string
	Options                                                               []delegationOption
}

func (s *Service) delegationHuman(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (consentHumanAuthorization, error) {
	if state.BindingRefreshAttemptID != "" || state.WorkloadReconnect != nil || state.PendingDelegation == nil || state.Subject == nil || state.PendingDelegation.Subject != *state.Subject || state.PendingDelegation.UserSessionIssuerID != endpoint.UserSessionIssuerID {
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

func (s *Service) loadPostLoginDelegation(r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (*postLoginDelegationData, error) {
	if state.PendingDelegation == nil {
		return nil, nil
	}
	if _, ok := s.consentBindings.(exactGrantDelegator); !ok {
		return nil, nil
	}
	human, err := s.delegationHuman(r.Context(), endpoint, state)
	if err != nil {
		return nil, consentAgentAuthorizationError(err, "connection delegation is unavailable")
	}
	candidates, err := agentsrepo.New(s.db).ListActiveAgentsForAuthorization(r.Context(), endpoint.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list delegation agents: %w", err)
	}
	client, err := remotesessionsrepo.New(s.db).GetRemoteSessionClientByID(r.Context(), remotesessionsrepo.GetRemoteSessionClientByIDParams{ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID, ID: state.PendingDelegation.RemoteSessionClientID})
	if err != nil {
		return nil, fmt.Errorf("load delegation connection type: %w", err)
	}
	sharedCredential := remotesessions.CredentialOwner(client.RemoteSessionClient.CredentialOwner) == remotesessions.CredentialOwnerSelf
	options := make([]delegationOption, 0)
	for _, agent := range candidates {
		if !agent.ProjectID.Valid || agent.ProjectID.UUID != endpoint.ProjectID {
			continue
		}
		if agent.OwnerUserID != human.userID && !authz.GrantsSatisfy(human.grants, authz.Check{Scope: authz.ScopeAgentWrite, ResourceKind: authz.ResourceKindAgent, ResourceID: agent.ID.String(), Dimensions: nil}) {
			continue
		}
		bindings, err := remotesessionsrepo.New(s.db).ListPrincipalRemoteSessionBindings(r.Context(), remotesessionsrepo.ListPrincipalRemoteSessionBindingsParams{ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID, PrincipalID: agent.ID, UserSessionIssuerID: endpoint.UserSessionIssuerID})
		if err != nil {
			return nil, fmt.Errorf("load agent assignments: %w", err)
		}
		option := delegationOption{ID: agent.ID.String(), Name: agent.Name, BindingID: "", Detail: "New connection", Disabled: false, Existing: false, Selected: false, Expiry: "", ExpiryExact: "", PermissionSummary: "", PreviousScopes: nil, Scopes: nil, ComparisonKnown: true}
		for _, binding := range bindings {
			if binding.RemoteSessionClientID != state.PendingDelegation.RemoteSessionClientID {
				continue
			}
			option.BindingID = binding.ID.String()
			if binding.RemoteSessionID != state.PendingDelegation.RemoteSessionID {
				option.Disabled = true
				option.Detail = "Another user's connection"
				if binding.SubjectDisplayName.Valid && binding.SubjectDisplayName.String != "" {
					option.Detail = binding.SubjectDisplayName.String + "'s connection"
				} else if binding.SubjectEmail.Valid && binding.SubjectEmail.String != "" {
					option.Detail = "Connection granted by " + binding.SubjectEmail.String
				}
			} else if binding.GrantGeneration == state.PendingDelegation.GrantGeneration {
				option.Existing = true
				option.Selected = true
				option.PreviousScopes = state.PendingDelegation.Scopes
				option.Expiry, option.ExpiryExact = delegationExpiry(state.PendingDelegation.ExpiresAt, time.Now())
				option.Disabled = true
				option.Detail = "Already using this connection"
			} else {
				option.Existing = true
				option.Selected = true
				option.Detail = "Your connection"
				option.ComparisonKnown = binding.GrantGeneration == state.PendingDelegation.PreviousGeneration
				if option.ComparisonKnown {
					option.PreviousScopes = state.PendingDelegation.PreviousScopes
					option.Expiry, option.ExpiryExact = delegationExpiry(state.PendingDelegation.PreviousExpiresAt, time.Now())
				} else {
					option.Expiry = "Previous expiry unavailable"
				}
			}
		}
		if sharedCredential {
			option.Detail = "Shared credential"
			option.Existing = false
			option.Selected = false
			option.Disabled = true
		}
		describeDelegationPermissions(&option, state.PendingDelegation.Scopes)
		options = append(options, option)
	}
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].Disabled != options[j].Disabled {
			return !options[i].Disabled
		}
		return !options[i].Disabled && options[i].BindingID != "" && options[j].BindingID == ""
	})
	if len(options) == 0 {
		return nil, nil
	}
	data := postLoginDelegationData{AccountName: state.PendingDelegation.UpstreamDisplayName, AccountEmail: state.PendingDelegation.UpstreamEmail, Styles: consentpage.Styles, ScriptURL: consentScriptURL, ActionURL: endpoint.consentPath() + "/remote-session", ContinueURL: endpoint.consentPath() + "?state=" + url.QueryEscape(state.ID), State: state.ID, CSRF: state.CSRFToken, SessionID: state.PendingDelegation.RemoteSessionID.String(), Generation: strconv.FormatInt(state.PendingDelegation.GrantGeneration, 10), Options: options, PreviousScopes: nil, HasPreviousScopes: false, Scopes: nil}
	data.PreviousScopes = state.PendingDelegation.PreviousScopes
	data.HasPreviousScopes = state.PendingDelegation.PreviousGeneration > 0
	for _, scope := range state.PendingDelegation.Scopes {
		data.Scopes = append(data.Scopes, delegationScope{Name: scope, Added: data.HasPreviousScopes && !slices.Contains(data.PreviousScopes, scope)})
	}
	return &data, nil
}

func (s *Service) servePostLoginDelegation(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (bool, error) {
	if state.PendingDelegation == nil {
		return false, nil
	}
	if !state.FirstParty && !endpoint.MetaMcpServerID.Valid {
		clients, err := s.remoteChallengeMgr.ListClients(r.Context(), endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
		if err != nil {
			return true, fmt.Errorf("load connection delegation clients: %w", err)
		}
		// Multi-service flows complete each delegation before another provider
		// callback can replace the grant being reviewed. A single service uses
		// the client's final consent page instead.
		if len(clients) <= 1 {
			return false, nil
		}
	}
	data, err := s.loadPostLoginDelegation(r, endpoint, state)
	if err != nil {
		return true, err
	}
	if data == nil {
		return false, nil
	}
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

func delegationExpiry(deadline *time.Time, now time.Time) (string, string) {
	if deadline == nil {
		return "Never expires", "No fixed expiration"
	}
	exact := deadline.UTC().Format("2 Jan 2006 at 15:04 UTC")
	if !deadline.After(now) {
		return "Expired", exact
	}
	hours := int(deadline.Sub(now).Hours())
	if hours < 1 {
		return "Expires in less than 1 hour", exact
	}
	if hours == 1 {
		return "Expires in 1 hour", exact
	}
	if hours < 24 {
		return fmt.Sprintf("Expires in %d hours", hours), exact
	}
	if hours < 48 {
		return "Expires in 1 day", exact
	}
	return fmt.Sprintf("Expires in %d days", hours/24), exact
}

func describeDelegationPermissions(option *delegationOption, scopes []string) {
	added := 0
	for _, scope := range scopes {
		isAdded := option.ComparisonKnown && !slices.Contains(option.PreviousScopes, scope)
		if isAdded {
			added++
		}
		option.Scopes = append(option.Scopes, delegationScope{Name: scope, Added: isAdded})
	}
	switch {
	case !option.ComparisonKnown:
		option.PermissionSummary = "Review permissions"
	case added == 1:
		option.PermissionSummary = "Grant 1 extra permission"
	case added > 1:
		option.PermissionSummary = fmt.Sprintf("Grant %d extra permissions", added)
	default:
		option.PermissionSummary = "No extra permissions"
	}
}
