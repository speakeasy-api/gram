package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var errWorkloadReconnectRequired = errors.New("workload upstream connection requires owner action")

// WorkloadReconnectBinding restricts the existing first-party consent flow.
// It grants no workload authority: retries still perform live admission and
// resolve the current owner's tenant-scoped upstream credential.
type WorkloadReconnectBinding struct {
	// ClientID pins the already-selected upstream registration.
	ClientID uuid.UUID `json:"client_id"`
	// OwnerUserID comes from live workload admission, never request parameters.
	OwnerUserID string `json:"owner_user_id"`
	// ExpiresAt survives challenge rotation and bounds the entire handoff.
	ExpiresAt time.Time `json:"expires_at"`
	// Ticket distinguishes the one-use entry ticket from a started consent flow.
	Ticket bool `json:"ticket,omitempty"`
}

func (b *WorkloadReconnectBinding) authorize(userID string, impersonated bool, now time.Time) error {
	if b == nil {
		return nil
	}
	if b.Ticket || b.OwnerUserID == "" || userID != b.OwnerUserID || impersonated || !now.Before(b.ExpiresAt) {
		return oops.E(oops.CodeForbidden, nil, "Only the workload owner can complete this unexpired connection request. Retry the original request to start again.")
	}
	return nil
}

// consumeWorkloadReconnect exchanges the opaque, single-use browser entry
// ticket for the ordinary first-party challenge. No identity comes from URL
// parameters; resource and owner were recorded after workload admission.
func (s *Service) consumeWorkloadReconnect(r *http.Request, endpoint *ResolvedMcpEndpoint) (*WorkloadReconnectBinding, error) {
	id := r.URL.Query().Get("workload_reconnect")
	if id == "" {
		return nil, nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid connection request")
	}
	state, err := s.authnChallengeCache.GetAndDelete(r.Context(), "authnChallenge:"+id)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "connection request expired; retry the original request")
	}
	b := state.WorkloadReconnect
	if b == nil || !b.Ticket || b.ClientID == uuid.Nil || b.OwnerUserID == "" || !time.Now().Before(b.ExpiresAt) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid or expired connection request")
	}
	if err := endpoint.ValidateChallenge(r.Context(), state.Endpoint, state.UserSessionIssuerID); err != nil {
		return nil, oauthAuthorityError(err)
	}
	b.Ticket = false
	return b, nil
}

func (s *Service) serveWorkloadReconnect(w http.ResponseWriter, r *http.Request, authentication *issuerGateAuthentication, selection *toolfilter.SessionSelection) error {
	if r.Method != http.MethodPost {
		return oops.E(oops.CodeFailedPrecondition, nil, "The workload owner's upstream connection requires sign-in. Retry an MCP request to obtain a connection link.")
	}
	prepared := prepareMCPRequest(w, r, 1<<20, mcpversions.SupportedHostedToolset())
	req := prepared.request
	if prepared.bodyReadErr != nil || prepared.bodyDecodeErr != nil || req.JSONRPC != "2.0" || req.Method == "" {
		return oops.E(oops.CodeBadRequest, nil, "invalid MCP request")
	}
	if req.ID.IsNull() {
		return respondWithNoContent(true, w)
	}
	endpoint := authentication.endpoint
	if !authentication.chosenClient.Valid {
		return oops.E(oops.CodeFailedPrecondition, nil, "The workload upstream client must be configured before its owner can connect it.")
	}
	ctx := r.Context()
	// Credential resolution precedes the proxy's call interceptors. Perform
	// their per-tool permission check before disclosing a reconnect link.
	if req.Method == "tools/call" {
		var params sdk.CallToolParamsRaw
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
			return oops.E(oops.CodeBadRequest, err, "invalid tool call")
		}
		if selection != nil && !selection.AllowsName(params.Name) {
			return oops.E(oops.CodeForbidden, nil, "tool is not allowed by this session")
		}
		if s.remoteProxyManager == nil || s.authz == nil {
			return oops.E(oops.CodeForbidden, nil, "tool authorization is unavailable")
		}
		interceptor := remotemcp.NewToolsCallAuthzInterceptor(s.authz, s.remoteProxyManager, endpoint.McpServerID.UUID.String(), endpoint.ProjectID.String(), s.logger)
		if err := interceptor.InterceptToolsCallRequest(ctx, &proxy.ToolsCallRequest{Params: &params, UserRequest: nil}); err != nil {
			return fmt.Errorf("authorize workload reconnect tool: %w", err)
		}
	}
	admission, err := runtimepolicy.AdmitWorkloadSession(ctx, s.db)
	if err != nil {
		return fmt.Errorf("admit workload reconnect: %w", err)
	}
	allowed, err := authz.GrantsAuthorize(admission.Owner, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: endpoint.ProjectID.String(), ResourceKind: "", Dimensions: nil})
	if err != nil {
		return fmt.Errorf("authorize workload owner project access: %w", err)
	}
	if !allowed || admission.OwnerUserID == "" {
		return oops.E(oops.CodeForbidden, nil, "The workload owner must have access to this project before connecting its upstream.")
	}
	ref, err := endpoint.EndpointRef(ctx, s.db, s.BaseURLForRequest(r))
	if err != nil {
		return err
	}
	root, err := endpoint.RootURL(s.BaseURLForRequest(r))
	if err != nil {
		return err
	}
	now := time.Now()
	ticket := AuthnChallengeState{
		ConsentBrowserHash:       "",
		FederatedBinding:         nil,
		DelegationRetryUsed:      false,
		Browser:                  nil,
		Federation:               nil,
		FlowID:                   "",
		AuthorizerUserID:         "",
		AuthorizerImpersonated:   nil,
		SignInID:                 nil,
		FreshLoginForced:         false,
		AgentAuthorizationTarget: nil,
		ClientID:                 "",
		RedirectURI:              "",
		State:                    "",
		CodeChallenge:            "",
		CodeChallengeMethod:      "",
		CSRFToken:                "",
		Subject:                  nil,
		AutoConnectDone:          false,
		FocusServer:              "",
		ID:                       uuid.NewString(),
		UserSessionIssuerID:      endpoint.UserSessionIssuerID,
		Endpoint:                 ref,
		CreatedAt:                now,
		FirstParty:               true,
		WorkloadReconnect:        &WorkloadReconnectBinding{ClientID: authentication.chosenClient.UUID, OwnerUserID: admission.OwnerUserID, ExpiresAt: now.Add(10 * time.Minute), Ticket: true},
	}
	if err := s.authnChallengeCache.Store(ctx, ticket); err != nil {
		return fmt.Errorf("store workload reconnect ticket: %w", err)
	}
	link := root + "/connect/first-party?workload_reconnect=" + url.QueryEscape(ticket.ID)
	return writeWorkloadReconnectResponse(w, req.ID, link, ticket.ID)
}

// URL-required is a JSON-RPC error, not an elicitation/create request. The
// readable message carries the same link for clients without an elicitation
// UI. Completing sign-in does not retry or execute the original operation.
func writeWorkloadReconnectResponse(w http.ResponseWriter, id mcpjsonrpc.ID, link, elicitationID string) error {
	message := fmt.Sprintf("The workload is authenticated, but its owner must connect the upstream: %s\nOnly the owner can complete this link. After connecting, explicitly retry the original request; it has not been executed.", link)
	source := sdk.URLElicitationRequiredError([]*sdk.ElicitParams{{Meta: nil, RequestedSchema: nil, Mode: "url", Message: message, URL: link, ElicitationID: elicitationID}})
	var rpcErr *jsonrpc.Error
	if !errors.As(source, &rpcErr) {
		return fmt.Errorf("build workload reconnect response: %w", source)
	}
	rpcErr.Message = message
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := json.NewEncoder(w).Encode(struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      mcpjsonrpc.ID  `json:"id"`
		Error   *jsonrpc.Error `json:"error"`
	}{JSONRPC: "2.0", ID: id, Error: rpcErr}); err != nil {
		return fmt.Errorf("encode workload reconnect response: %w", err)
	}
	return nil
}

func validateWorkloadReconnectBinding(state AuthnChallengeState) error {
	if b := state.WorkloadReconnect; b != nil {
		if b.Ticket || !time.Now().Before(b.ExpiresAt) {
			return errors.New("workload connection request expired or not started")
		}
		if state.Subject != nil {
			if state.Subject.Kind != urn.SessionSubjectKindUser || state.Subject.ID != b.OwnerUserID {
				return errors.New("workload connection owner mismatch")
			}
			impersonated := state.AuthorizerImpersonated == nil || *state.AuthorizerImpersonated
			if err := b.authorize(state.AuthorizerUserID, impersonated, time.Now()); err != nil {
				return err
			}
		}
	}

	return nil
}
