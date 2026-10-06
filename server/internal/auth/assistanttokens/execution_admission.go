package assistanttokens

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// IsExecutionToken selects a validator, not authority. Once selected, an
// invalid or denied credential never falls back to user-scoped runtime-token
// authorization. A malformed typ header also selects it, so it cannot reach
// the legacy validator either.
func IsExecutionToken(raw string) bool {
	token, _, err := jwt.NewParser().ParseUnverified(executionBearer(raw), new(mcpauthz.AssistantExecutionClaims))
	if err != nil {
		return false
	}
	typ, present := token.Header["typ"]
	_, isString := typ.(string)
	return (present && !isString) || typ == mcpauthz.AssistantExecutionType
}

func executionBearer(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	return raw
}

func (m *Manager) executionForUse(ctx context.Context, raw string) (*assistantidentity.Execution, error) {
	claims, err := m.executionIssuer.ValidateAssistantExecution(executionBearer(raw))
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "invalid execution credential")
	}
	e := claims.Execution
	if err := m.validateExecutionAuthority(ctx, e); err != nil {
		if errors.Is(err, assistantidentity.ErrInvalidIdentity) || errors.Is(err, assistantidentity.ErrActorIneligible) {
			return nil, oops.E(oops.CodeUnauthorized, err, "execution authority unavailable")
		}
		return nil, fmt.Errorf("validate execution authority: %w", err)
	}
	return &e, nil
}

// AuthorizeRuntime authorizes runner calls that act for the assistant itself:
// bootstrap, compaction persistence, MCP OAuth flow creation, model
// inference and the platform toolset, whose tools enforce their own
// assistant-scoped restrictions. Other tokens use Authorize.
func (m *Manager) AuthorizeRuntime(ctx context.Context, raw string) (context.Context, *Claims, error) {
	if !IsExecutionToken(raw) {
		return m.Authorize(ctx, raw)
	}
	e, err := m.executionForUse(ctx, raw)
	if err != nil {
		return ctx, nil, fmt.Errorf("authorize assistant execution: %w", err)
	}
	return m.executionContext(ctx, *e)
}

// executionContext acts as the trigger's workload principal. Authorization
// admits it through the ordinary workload path: the token's ceiling
// intersected with the agent's live policy. No user grants are installed.
func (m *Manager) executionContext(ctx context.Context, e assistantidentity.Execution) (context.Context, *Claims, error) {
	project, err := m.projects.GetProjectByID(ctx, e.Identity.ProjectID)
	if err != nil || project.OrganizationID != e.Identity.OrganizationID {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}
	thread, err := assistantsrepo.New(m.db).LoadAssistantThreadForBootstrap(ctx, assistantsrepo.LoadAssistantThreadForBootstrapParams{ThreadID: e.ThreadID, ProjectID: e.Identity.ProjectID})
	if err != nil || thread.AssistantID != e.Identity.AssistantID || thread.OrganizationID != e.Identity.OrganizationID {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}
	ctx = context.WithValue(ctx, executionChatKey{}, thread.ChatID)
	ctx = context.WithValue(ctx, executionKey{}, e)
	org, err := m.orgs.GetOrganizationMetadata(ctx, e.Identity.OrganizationID)
	if err != nil {
		return ctx, nil, fmt.Errorf("load execution organization: %w", err)
	}
	ac := &contextvalues.AuthContext{UserID: "", ExternalUserID: "", APIKeyID: "", APIKeyName: "", OrgWidePluginHooksKey: false, SessionID: nil, Email: nil, HasActiveSubscription: false, APIKeyScopes: nil, IsAdmin: false, SupportOrganizationID: "", ActiveOrganizationID: e.Identity.OrganizationID, ProjectID: &project.ID, ProjectSlug: &project.Slug, OrganizationSlug: org.Slug, AccountType: org.GramAccountType, Whitelisted: org.Whitelisted}
	ctx = contextvalues.WithPrincipalCredentialAuthorization(ctx, ac, urn.NewWorkloadPrincipal(e.Identity.IssuerID, e.Identity.Subject), contextvalues.PrincipalCredential{AuthorizerUserID: "", DelegatedGrants: e.Ceiling.Policy, DelegatedGrantsVersion: int32(e.Ceiling.EncodingVersion)})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: e.Identity.AssistantID, ThreadID: e.ThreadID})
	ctx = contextvalues.WithAssistantInvoker(ctx, e.HumanUserID)
	var claims Claims
	claims.OrgID = e.Identity.OrganizationID
	claims.ProjectID = e.Identity.ProjectID.String()
	claims.AssistantID = e.Identity.AssistantID.String()
	claims.ThreadID = e.ThreadID.String()
	claims.UserID = e.HumanUserID
	return ctx, &claims, nil
}

// AuthorizeBusiness authorizes an execution token for one MCP server or
// toolset in the token's project. The resource's project is read from storage
// so a global resource ID cannot lend cross-project authority.
func (m *Manager) AuthorizeBusiness(ctx context.Context, raw string, resource uuid.UUID) (context.Context, error) {
	if resource == uuid.Nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	e, err := m.executionForUse(ctx, raw)
	if err != nil {
		return ctx, fmt.Errorf("authorize assistant business execution: %w", err)
	}
	ctx, _, err = m.executionContext(ctx, *e)
	if err != nil {
		return ctx, fmt.Errorf("authorize assistant business execution: %w", err)
	}
	_, err = mcpserversrepo.New(m.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: resource, ProjectID: e.Identity.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = toolsetsrepo.New(m.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: resource, ProjectID: e.Identity.ProjectID})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return ctx, fmt.Errorf("resolve execution resource ownership: %w", err)
	}
	ctx, err = m.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, fmt.Errorf("authorize assistant business execution: %w", err)
	}
	if err := m.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, resource.String(), e.Identity.ProjectID.String())); err != nil {
		return ctx, fmt.Errorf("authorize assistant business execution: %w", err)
	}
	return ctx, nil
}

type executionKey struct{}

// ExecutionFromContext returns the execution a request was authorized with.
func ExecutionFromContext(ctx context.Context) (assistantidentity.Execution, bool) {
	e, ok := ctx.Value(executionKey{}).(assistantidentity.Execution)
	return e, ok
}

type executionChatKey struct{}

// ExecutionChatID is the chat of the thread an execution token is pinned to.
func ExecutionChatID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(executionChatKey{}).(uuid.UUID)
	return id, ok
}
