package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

// destructiveHookFixture wires up a Claude PreToolUse test that exercises the
// destructive-scope check: it seeds a Gram user keyed by email, optionally
// grants the destructive scope, and primes Redis with session metadata that
// resolves to that user. handlePreToolUse can then run end-to-end against the
// real authz engine.
type destructiveHookFixture struct {
	ti        *testInstance
	sessionID string
	userEmail string
	gramUser  string
	orgID     string
	projectID uuid.UUID
}

func newDestructiveHookFixture(t *testing.T, grantDestructive bool) (context.Context, destructiveHookFixture) {
	t.Helper()

	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	orgID := authCtx.ActiveOrganizationID
	projectID := *authCtx.ProjectID

	userEmail := "destructive-tester+" + uuid.NewString()[:8] + "@example.com"
	gramUserID := uuid.NewString()
	seedHookGramUser(t, ctx, ti.conn, gramUserID, userEmail)
	seedHookOrgConnection(t, ctx, ti.conn, orgID, gramUserID)

	if grantDestructive {
		seedHookDestructiveGrant(t, ctx, ti.conn, orgID, gramUserID)
	}

	sessionID := uuid.NewString()
	seedHookSessionMetadata(t, ctx, ti, sessionID, userEmail, orgID, projectID.String())

	return ctx, destructiveHookFixture{
		ti:        ti,
		sessionID: sessionID,
		userEmail: userEmail,
		gramUser:  gramUserID,
		orgID:     orgID,
		projectID: projectID,
	}
}

func seedHookGramUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, userID, email string) {
	t.Helper()
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: "Hook Tester",
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)
}

func seedHookOrgConnection(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, userID string) {
	t.Helper()
	err := orgrepo.New(conn).AttachWorkOSUserToOrg(ctx, orgrepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     orgID,
		UserID:             userID,
		WorkosMembershipID: conv.PtrToPGText(conv.PtrEmpty("membership-" + userID)),
	})
	require.NoError(t, err)
}

func seedHookDestructiveGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, userID string) {
	t.Helper()
	selector := authz.NewSelector(authz.ScopeToolsExecuteDestructive, orgID)
	selectorBytes, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		Scope:          string(authz.ScopeToolsExecuteDestructive),
		Selectors:      selectorBytes,
	})
	require.NoError(t, err)
}

func seedHookSessionMetadata(t *testing.T, ctx context.Context, ti *testInstance, sessionID, userEmail, orgID, projectID string) {
	t.Helper()
	metadata := SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   userEmail,
		ClaudeOrgID: "claude-org",
		GramOrgID:   orgID,
		ProjectID:   projectID,
	}
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), metadata, 24*time.Hour))
}

// Bash `rm -rf *` is a content-trigger match. Without the destructive scope
// the call must be denied and the deny reason must reference the matched
// pattern so the user knows why.
func TestClaude_PreToolUse_DeniesDestructiveBashWithoutScope(t *testing.T) {
	t.Parallel()

	ctx, f := newDestructiveHookFixture(t, false)

	toolName := "Bash"
	toolUseID := "toolu_destructive_no_grant"
	result, err := f.ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &f.sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "rm -rf *"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Contains(t, *output.PermissionDecisionReason, "tools:execute_destructive")
	assert.Contains(t, *output.PermissionDecisionReason, "shell/rm-rf")
}

func TestClaude_PreToolUse_AllowsDestructiveBashWithScope(t *testing.T) {
	t.Parallel()

	ctx, f := newDestructiveHookFixture(t, true)

	toolName := "Bash"
	toolUseID := "toolu_destructive_with_grant"
	result, err := f.ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &f.sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "rm -rf /tmp/work"},
	})
	require.NoError(t, err)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "allow", *output.PermissionDecision)
}

// When metadata.UserEmail is empty (Redis not seeded yet, plugin auth path),
// destructive enforcement must fail closed: no identity = no scope check
// possible.
func TestClaude_PreToolUse_DeniesDestructiveBashWithEmptyEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	sessionID := uuid.NewString()
	toolName := "Bash"
	toolUseID := "toolu_destructive_no_email"
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "rm -rf *"},
	})
	require.NoError(t, err)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision,
		"empty UserEmail should fail closed for destructive content")
}

// When the resolved email has no Gram user record, the lookup returns "" and
// the destructive check fails closed.
func TestClaude_PreToolUse_DeniesDestructiveBashWithUnknownEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	sessionID := uuid.NewString()
	seedHookSessionMetadata(t, ctx, ti, sessionID, "ghost@example.com", authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	toolName := "Bash"
	toolUseID := "toolu_destructive_unknown_email"
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "rm -rf /etc"},
	})
	require.NoError(t, err)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
}

// A benign Bash call must still allow — the content trigger only fires on
// curated patterns. Without a grant on a benign call, the engine never
// runs (no destructive content matched).
func TestClaude_PreToolUse_AllowsBenignBash(t *testing.T) {
	t.Parallel()

	ctx, f := newDestructiveHookFixture(t, false)

	toolName := "Bash"
	toolUseID := "toolu_benign_bash"
	result, err := f.ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &f.sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "ls -la"},
	})
	require.NoError(t, err)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "allow", *output.PermissionDecision)
}

// MCP tool input that contains a destructive SQL fragment ("DROP TABLE")
// should trip the content trigger even though the tool name itself isn't
// flagged. This proves the trigger is tool-name-agnostic.
func TestClaude_PreToolUse_DeniesMCPToolWithDestructiveContent(t *testing.T) {
	t.Parallel()

	ctx, f := newDestructiveHookFixture(t, false)

	toolName := "mcp__db__run_query"
	toolUseID := "toolu_mcp_drop_table"
	result, err := f.ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &f.sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"query": "DROP TABLE users"},
	})
	require.NoError(t, err)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Contains(t, *output.PermissionDecisionReason, "database/drop")
}

// Non-enterprise orgs must remain unaffected — the destructive content
// trigger short-circuits in RequireForHookPrincipal when the account isn't
// enterprise, so a Bash `rm -rf` must still allow.
func TestClaude_PreToolUse_AllowsDestructiveBashOnNonEnterprise(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "pro"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	sessionID := uuid.NewString()
	seedHookSessionMetadata(t, ctx, ti, sessionID, "anyone@example.com", authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	toolName := "Bash"
	toolUseID := "toolu_destructive_non_enterprise"
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "rm -rf *"},
	})
	require.NoError(t, err)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "allow", *output.PermissionDecision)
}
