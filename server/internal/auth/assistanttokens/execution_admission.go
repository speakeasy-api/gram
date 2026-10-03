package assistanttokens

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// IsExecutionToken selects a validator, not authority. Once selected, an
// invalid or denied credential cannot retry through legacy owner admission.
func IsExecutionToken(raw string) bool {
	token, _, err := jwt.NewParser().ParseUnverified(executionBearer(raw), new(mcpauthz.AssistantExecutionClaims))
	return err == nil && token.Header["typ"] == mcpauthz.AssistantExecutionType
}

func executionBearer(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	return raw
}

func (m *Manager) executionForUse(ctx context.Context, raw string) (*assistantidentity.Execution, error) {
	if m.executionIssuer == nil || m.executionIdentities == nil || m.executionDB == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	claims, err := m.executionIssuer.ValidateAssistantExecution(executionBearer(raw))
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "invalid execution credential")
	}
	e := claims.Execution
	if err := m.validateExecutionAuthority(ctx, e); err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "execution authority unavailable")
	}
	return &e, nil
}

// AuthorizeRuntime is used only for bootstrap, compaction persistence and
// model inference. Workload execution has no fabricated human UserID.
func (m *Manager) AuthorizeRuntime(ctx context.Context, raw string) (context.Context, *Claims, error) {
	if !IsExecutionToken(raw) {
		return m.Authorize(ctx, raw)
	}
	e, err := m.executionForUse(ctx, raw)
	if err != nil {
		return ctx, nil, fmt.Errorf("authorize assistant execution: %w", err)
	}
	if err := m.executionIdentities.AdmitModel(ctx, m.executionDB, *e); err != nil {
		return ctx, nil, oops.E(oops.CodeForbidden, err, "assistant model execution denied")
	}
	return m.executionContext(ctx, *e)
}

func (m *Manager) executionContext(ctx context.Context, e assistantidentity.Execution) (context.Context, *Claims, error) {
	project, err := m.projects.GetProjectByID(ctx, e.Identity.ProjectID)
	if err != nil || project.OrganizationID != e.Identity.OrganizationID {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}
	thread, err := assistantsrepo.New(m.executionDB).LoadAssistantThreadForBootstrap(ctx, assistantsrepo.LoadAssistantThreadForBootstrapParams{ThreadID: e.ThreadID, ProjectID: e.Identity.ProjectID})
	if err != nil || thread.AssistantID != e.Identity.AssistantID || thread.OrganizationID != e.Identity.OrganizationID {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}
	ctx = context.WithValue(ctx, executionChatKey{}, thread.ChatID)
	org, err := m.orgs.GetOrganizationMetadata(ctx, e.Identity.OrganizationID)
	if err != nil {
		return ctx, nil, fmt.Errorf("authorize assistant execution: %w", err)
	}
	ac := &contextvalues.AuthContext{UserID: "", ExternalUserID: "", APIKeyID: "", APIKeyName: "", OrgWidePluginHooksKey: false, SessionID: nil, Email: nil, HasActiveSubscription: false, APIKeyScopes: nil, IsAdmin: false, SupportOrganizationID: "", ActiveOrganizationID: e.Identity.OrganizationID, ProjectID: &project.ID, ProjectSlug: &project.Slug, OrganizationSlug: org.Slug, AccountType: org.GramAccountType, Whitelisted: org.Whitelisted}
	ctx = contextvalues.WithPrincipalCredentialAuthorization(ctx, ac, urn.NewWorkloadPrincipal(e.Identity.IssuerID, e.Identity.Subject), contextvalues.PrincipalCredential{AuthorizerUserID: "", DelegatedGrants: e.Ceiling.Policy, DelegatedGrantsVersion: int32(e.Ceiling.EncodingVersion)})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: e.Identity.AssistantID, ThreadID: e.ThreadID})
	return ctx, executionRuntimeClaims(e, ""), nil
}

// AuthorizeBusiness is selected only by the server's resource MCP route. The
// runtime credential is not an MCP-resource access token and never enters that
// token validator. Instead, the concrete server resource is authorized using
// the existing workload ceiling/live-agent admission and subsequent tool checks.
func (m *Manager) AuthorizeBusiness(ctx context.Context, raw string, resource uuid.UUID, restriction BusinessPolicyRestriction) (context.Context, error) {
	if resource == uuid.Nil || m.authz == nil {
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
	ctx, err = m.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, fmt.Errorf("authorize assistant business execution: %w", err)
	}
	if err := m.authz.Require(ctx, authz.AssistantExecuteCheck(e.Identity.AssistantID.String(), e.Identity.ProjectID.String())); err != nil {
		return ctx, fmt.Errorf("authorize business execution capability: %w", err)
	}
	// A trusted server hook may only intersect an additional policy. It cannot
	// choose another principal, credential, resource or platform route.
	if restriction != nil {
		grants, err := restriction(ctx, *e)
		if err != nil {
			return ctx, fmt.Errorf("authorize assistant business execution: %w", err)
		}
		ctx = authz.RestrictContext(ctx, grants)
	}
	if err := m.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, resource.String(), e.Identity.ProjectID.String())); err != nil {
		return ctx, fmt.Errorf("authorize assistant business execution: %w", err)
	}
	return ctx, nil
}

// AuthorizePlatform is called exclusively by the registered platform route.
// It preserves assistant-owned credentials/permissions independently of the
// chosen invoker and of business denial. Tool-level managed/project/thread
// restrictions still run in the platform service. External MCPs cannot select it.
func (m *Manager) AuthorizePlatform(ctx context.Context, raw string) (context.Context, *Claims, error) {
	if !IsExecutionToken(raw) {
		return m.Authorize(ctx, raw)
	}
	if m.executionIssuer == nil || m.executionIdentities == nil || m.executionDB == nil {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}
	claims, err := m.executionIssuer.ValidateAssistantExecution(executionBearer(raw))
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "invalid platform execution credential")
	}
	e := &claims.Execution
	// Platform capabilities belong to the assistant, not the selected human.
	// Live workload/thread authority remains mandatory; invoker grant loss must
	// not suppress an otherwise authorized assistant-owned error reply.
	if err := m.validateExecutionWorkloadAuthority(ctx, *e); err != nil {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "invalid platform authority")
	}

	// Keep the workload and AssistantPrincipal; platform tools authorize their
	// own assistant-owned capabilities. Never install creator user grants.
	return m.executionContext(ctx, *e)
}

type executionChatKey struct{}

// ExecutionChatID is trusted persisted routing metadata, not an HTTP header.
func ExecutionChatID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(executionChatKey{}).(uuid.UUID)
	return id, ok
}

// BusinessPolicyRestriction is the invocation-scoped integration boundary for
// AIM-412's human policy. Nil means AIM-411's ceiling intersect agent policy;
// a configured hook returning no grants denies, and errors never select owner.
type BusinessPolicyRestriction func(context.Context, assistantidentity.Execution) ([]authz.Grant, error)

func executionRuntimeClaims(e assistantidentity.Execution, userID string) *Claims {
	var claims Claims
	claims.OrgID = e.Identity.OrganizationID
	claims.ProjectID = e.Identity.ProjectID.String()
	claims.AssistantID = e.Identity.AssistantID.String()
	claims.ThreadID = e.ThreadID.String()
	claims.UserID = userID
	return &claims
}
