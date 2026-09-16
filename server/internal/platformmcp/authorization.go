package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type externalAuthorizationRefusal struct {
	Code             string `json:"code"`
	RequiredScope    string `json:"required_scope"`
	RequestAccessURL string `json:"request_access_url,omitempty"`
	Message          string `json:"message"`
}

func externalAuthorizationToolResult(err error) (*mcp.CallToolResult, bool) {
	var denied *ExternalAuthorizationError
	if !errors.As(err, &denied) {
		return nil, false
	}
	payload, marshalErr := json.Marshal(externalAuthorizationRefusal{
		Code:             "permission_denied",
		RequiredScope:    denied.RequiredScope,
		RequestAccessURL: denied.RequestAccessURL,
		Message:          denied.Error(),
	})
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{ //nolint:exhaustruct // An authorization refusal has no MCP metadata, requests, or task state.
		Content: []mcp.Content{&mcp.TextContent{ //nolint:exhaustruct // Plain refusal text needs no annotations or metadata.
			Text: string(payload),
		}},
		IsError: true,
	}, true
}

// ExternalAuthorizationError is safe to return to an MCP client. It names the
// actual failed check and a dashboard request path, but never includes the
// caller's grants, tool arguments, credentials, or undisclosed resource names.
type ExternalAuthorizationError struct {
	RequiredScope    string
	RequestAccessURL string
	cause            error
}

func (e *ExternalAuthorizationError) Error() string {
	requiredScope := strings.TrimSpace(e.RequiredScope)
	if requiredScope == "" {
		return "You do not have permission to use this Platform MCP tool. Ask an organization administrator to review your access."
	}
	message := fmt.Sprintf("You do not have permission to use this Platform MCP tool. This action requires %s.", requiredScope)
	if e.RequestAccessURL != "" {
		message += " Request access: " + e.RequestAccessURL
	} else {
		message += " Ask an organization administrator to review your access."
	}
	return message
}

func (e *ExternalAuthorizationError) Unwrap() error { return e.cause }

// PrepareExternalContext verifies live membership, binds a trusted user
// context, and resolves current grants once for this external request.
func (a *LiveOrgAdminAuthorizer) PrepareExternalContext(ctx context.Context, principal Principal) (context.Context, error) {
	if a == nil || a.db == nil || a.engine == nil || principal.UserID == "" || principal.OrganizationID == "" {
		return ctx, ErrUnavailable
	}
	if err := a.RequireLiveMembership(ctx, principal); err != nil {
		return ctx, err
	}
	organization, err := organizationsrepo.New(a.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return ctx, fmt.Errorf("load Platform MCP organization: %w", err)
	}

	ctx = contextWithPrincipal(ctx, principal)
	// Platform MCP deliberately has no dashboard session or selected project.
	// Organization metadata is copied only for downstream entitlement context;
	// authorization comes exclusively from the freshly loaded grants below.
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  principal.OrganizationID,
		UserID:                principal.UserID,
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             nil,
		ProjectID:             nil,
		OrganizationSlug:      organization.Slug,
		Email:                 nil,
		AccountType:           organization.GramAccountType,
		HasActiveSubscription: false,
		Whitelisted:           organization.Whitelisted,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
		SupportOrganizationID: "",
	}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	// Resolve from the verified principal and overwrite any grants inherited
	// from a parent request. PrepareContext intentionally trusts an existing
	// grant value, which is not appropriate at this transport boundary.
	principals, err := authz.ResolveUserPrincipals(ctx, a.db, principal.OrganizationID, principal.UserID)
	if err != nil {
		return ctx, fmt.Errorf("resolve Platform MCP member principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, a.db, principal.OrganizationID, principals)
	if err != nil {
		return ctx, fmt.Errorf("load Platform MCP member grants: %w", err)
	}
	ctx = authz.GrantsToContext(ctx, grants)
	// Platform MCP is a trusted session-less acting surface, so ShouldEnforce must
	// be true. Treat any future regression as unavailable rather than allowing
	// Require to become a no-op.
	enforce, err := a.engine.ShouldEnforce(ctx)
	if err != nil || !enforce {
		if err == nil {
			err = errors.New("Platform MCP RBAC enforcement is inactive") //nolint:staticcheck // Platform MCP is a product name.
		}
		return ctx, fmt.Errorf("verify Platform MCP RBAC enforcement: %w", err)
	}
	return ctx, nil
}

func (a *LiveOrgAdminAuthorizer) AuthorizeExternalCall(ctx context.Context, principal Principal, policy ExternalAuthorization) error {
	if a == nil || a.engine == nil || principal.UserID == "" || principal.OrganizationID == "" {
		return ErrUnavailable
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID != principal.UserID || authCtx.ActiveOrganizationID != principal.OrganizationID {
		return ErrUnavailable
	}
	var check authz.Check
	switch policy {
	case ExternalAuthorizationOrgAdmin:
		check = authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: principal.OrganizationID, Dimensions: nil}
	default:
		return fmt.Errorf("%w: unsupported external authorization policy %q", ErrUnavailable, policy)
	}
	if err := a.engine.Require(ctx, check); err != nil {
		var shareable *oops.ShareableError
		if !errors.As(err, &shareable) || shareable.Code != oops.CodeForbidden {
			return fmt.Errorf("authorize Platform MCP call: %w", err)
		}
		return &ExternalAuthorizationError{
			RequiredScope:    string(check.Scope),
			RequestAccessURL: a.requestAccessURL(ctx, check),
			cause:            err,
		}
	}
	return nil
}

func (a *LiveOrgAdminAuthorizer) requestAccessURL(ctx context.Context, check authz.Check) string {
	// The current policy is one exact, user-visible organization scope. Future
	// compound or undisclosable checks must withhold a prefilled request rather
	// than suggest an inaccurate grant.
	if check.Scope != authz.ScopeOrgAdmin || check.ResourceID == "" || check.ResourceID == authz.WildcardResource {
		return ""
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return ""
	}
	organizationSlug := strings.TrimSpace(authCtx.OrganizationSlug)
	if organizationSlug == "" {
		return ""
	}
	return mcpaccess.RequestAccessURL(a.dashboardURL, organizationSlug, mcpaccess.RequestAccessURLParams{
		Scope:        string(check.Scope),
		ResourceID:   check.ResourceID,
		ResourceName: "",
	})
}
