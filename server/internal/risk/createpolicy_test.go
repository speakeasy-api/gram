package risk_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestCreateRiskPolicy_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	enabled := true
	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Test Policy"),
		Sources: []string{"gitleaks"},
		Enabled: &enabled,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Test Policy", result.Name)
	require.Equal(t, []string{"gitleaks"}, result.Sources)
	require.True(t, result.Enabled)
	require.Equal(t, int64(1), result.Version)
	require.NotEqual(t, uuid.Nil.String(), result.ID)

	// Should have signaled the drain workflow.
	require.Len(t, ti.signaler.calls, 1)
}

func TestCreateRiskPolicy_DefaultSources(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("No Sources"),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"gitleaks"}, result.Sources)
	require.True(t, result.Enabled) // default enabled
}

func TestCreateRiskPolicy_DestructiveToolSource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"destructive_tool"},
		Action:  "flag",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"destructive_tool"}, result.Sources)
	require.Equal(t, "Destructive Tool Scanner", result.Name)
}

func TestCreateRiskPolicy_DestructiveToolRejectsBlock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"destructive_tool"},
		Action:  "block",
	})
	require.Error(t, err)
}

func TestCreateRiskPolicy_CLIDestructiveSource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"cli_destructive"},
		Action:  "flag",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"cli_destructive"}, result.Sources)
	require.Equal(t, "Destructive CLI Command Scanner", result.Name)
}

func TestCreateRiskPolicy_CLIDestructiveRejectsBlock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"cli_destructive"},
		Action:  "block",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cli_destructive")
}

func TestCreateRiskPolicy_EmptyName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new(""),
	})
	require.Error(t, err)
}

func TestCreateRiskPolicy_NameTooLong(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	var longName strings.Builder
	for range 101 {
		longName.WriteString("a")
	}
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new(longName.String()),
	})
	require.Error(t, err)
}

func TestCreateRiskPolicy_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// Set up enterprise account with zero grants — RBAC should deny.
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Should Fail"),
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCreateRiskPolicy_DisabledDoesNotSignal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	enabled := false
	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Disabled Policy"),
		Enabled: &enabled,
	})
	require.NoError(t, err)
	require.False(t, result.Enabled)
	require.Empty(t, ti.signaler.calls) // should not signal when disabled
}

func TestCreateRiskPolicy_ShadowMCPAllowedURLsAreAtomic(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	name := "Shadow MCP Block"
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    &name,
		Sources: []string{"shadow_mcp"},
		Action:  "block",
		ShadowMcpAllowedUrls: []string{
			"HTTPS://GITHUB.EXAMPLE.COM:443/mcp?token=ignored",
			"https://linear.example.com/sse",
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"https://github.example.com/mcp",
		"https://linear.example.com/sse",
	}, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestCreateRiskPolicy_ShadowMCPAllowedURLsRequireCurrentProjectInventory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	observedURL := "https://github.example.com/mcp"
	lookupCalls := 0
	ti.shadowMCPInventoryURLLookup = func(_ context.Context, projectID uuid.UUID, canonicalURL string) (bool, error) {
		lookupCalls++
		require.Equal(t, *authCtx.ProjectID, projectID)
		require.Equal(t, observedURL, canonicalURL)
		return true, nil
	}

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Observed Shadow MCP"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"HTTPS://GITHUB.EXAMPLE.COM:443/mcp?secret=ignored"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, []string{observedURL}, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestCreateRiskPolicy_ShadowMCPUnobservedURLRejectedBeforeMutation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	lookupCalls := 0
	reconcileCalls := 0
	ti.shadowMCPInventoryURLLookup = func(context.Context, uuid.UUID, string) (bool, error) {
		lookupCalls++
		return false, nil
	}
	ti.reconcileShadowMCPPolicyURLs = func(context.Context, riskrepo.DBTX, policybypass.ReconcilePolicyURLsInput) error {
		reconcileCalls++
		return nil
	}
	name := "Unobserved Shadow MCP"

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"https://unobserved.example.com/mcp"},
	})
	require.ErrorContains(t, err, "has not been observed in this project")
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, reconcileCalls)
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
	require.Empty(t, ti.signaler.Calls())
}

func TestCreateRiskPolicy_ShadowMCPURLObservedOnlyByAnotherProjectRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	otherProjectID := uuid.New()
	ti.shadowMCPInventoryURLLookup = func(_ context.Context, projectID uuid.UUID, _ string) (bool, error) {
		return projectID == otherProjectID, nil
	}

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Cross Project Shadow MCP"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"https://other-project.example.com/mcp"},
	})
	require.ErrorContains(t, err, "has not been observed in this project")
	require.NotEqual(t, otherProjectID, *authCtx.ProjectID)
}

func TestCreateRiskPolicy_ShadowMCPInventoryLookupFailureIsUnexpected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	ti.shadowMCPInventoryURLLookup = func(context.Context, uuid.UUID, string) (bool, error) {
		return false, errors.New("clickhouse unavailable")
	}
	name := "Inventory Failure Shadow MCP"

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"https://unavailable.example.com/mcp"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_ShadowMCPEmptyAllowedURLsSkipsInventoryLookup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	ti.shadowMCPInventoryURLLookup = func(context.Context, uuid.UUID, string) (bool, error) {
		return false, errors.New("inventory lookup must not run")
	}

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Empty Shadow MCP"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{},
	})
	require.NoError(t, err)
}

func TestCreateRiskPolicy_ShadowMCPReconcileFailureRollsBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	ti.reconcileShadowMCPPolicyURLs = func(context.Context, riskrepo.DBTX, policybypass.ReconcilePolicyURLsInput) error {
		return errors.New("injected grant failure")
	}
	name := "Rolled Back Shadow MCP"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"https://github.example.com/mcp"},
	})
	require.ErrorContains(t, errors.Unwrap(err), "injected grant failure")
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_ShadowMCPAllowedURLsRejectInvalidState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	name := "Shadow MCP Flag"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "flag",
		ShadowMcpAllowedUrls: []string{"https://github.example.com/mcp"},
	})
	require.ErrorContains(t, err, "shadow mcp allowed urls require an enabled blocking shadow mcp policy")
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_ShadowMCPAllowedURLsRejectInvalidURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	name := "Shadow MCP Invalid URL"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"not a shadow mcp url"},
	})
	require.ErrorContains(t, err, "invalid shadow mcp allowed urls")
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func requireRiskPolicyCreateAuditCount(t *testing.T, ctx context.Context, ti *testInstance, policyID string, want int) {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	logs, err := auditrepo.New(ti.conn).ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		CursorSeq:      pgtype.Int8{},
		ActorID:        pgtype.Text{},
		Action:         pgtype.Text{String: "risk_policy:create", Valid: true},
		SubjectType:    pgtype.Text{String: "risk_policy", Valid: true},
		SubjectID:      pgtype.Text{String: policyID, Valid: true},
	})
	require.NoError(t, err)
	require.Len(t, logs, want)
}

type policyNameCompletionClient struct {
	mock.Mock
}

func newPolicyNameCompletionClient(name string) *policyNameCompletionClient {
	content := or.CreateChatAssistantMessageContentStr(name)
	message := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&content),
		Name:             nil,
		ToolCalls:        nil,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
	client := new(policyNameCompletionClient)
	client.On("GetCompletion", mock.Anything, mock.Anything).Return(&openrouter.CompletionResponse{
		StartTime:    time.Time{},
		Message:      &message,
		MessageID:    "",
		Model:        "test-model",
		Usage:        openrouter.Usage{},
		FinishReason: nil,
		ToolCalls:    nil,
		Content:      name,
	}, nil)
	return client
}

func (c *policyNameCompletionClient) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	args := c.Called(ctx, request)
	response, _ := args.Get(0).(*openrouter.CompletionResponse)
	return response, args.Error(1)
}

func (c *policyNameCompletionClient) GetCompletionStream(context.Context, openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, errors.New("not implemented")
}

func (c *policyNameCompletionClient) GetObjectCompletion(context.Context, openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *policyNameCompletionClient) CreateEmbeddings(context.Context, string, string, []string, ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, errors.New("not implemented")
}
