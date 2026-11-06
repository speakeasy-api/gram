package mcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// filterFixture returns a request context granted mcp:connect on one tool of
// toolsetID only, and an engine that counts how often it would record a check.
func filterFixture(t *testing.T, toolsetID string) (context.Context, *authz.Engine, *int) {
	t.Helper()
	sessionID := "session_filter"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_" + uuid.NewString(),
		UserID:               "user_filter",
		SessionID:            &sessionID,
		AccountType:          "enterprise",
	})
	ctx = authz.GrantsToContext(ctx, []authz.Grant{{
		PrincipalUrn: "role:admin",
		Scope:        authz.ScopeMCPConnect,
		Selector:     authz.Selector{"resource_kind": "mcp", "resource_id": toolsetID, "tool": "tool_1"},
	}})

	recorded := 0
	engine := authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) {
		recorded++
		return false, nil
	}, workos.NewStubClient())
	return ctx, engine, &recorded
}

func filterTools(n int) []*toolListEntry {
	tools := make([]*toolListEntry, n)
	for i := range tools {
		tools[i] = &toolListEntry{Name: fmt.Sprintf("tool_%d", i)}
	}
	return tools
}

// Listing a server's tools asks about none of them, so filtering records no
// authorization check: every tool the caller cannot use would otherwise show
// as a denial on each tools/list.
func TestFilterToolsByMCPConnect_RecordsNoChecks(t *testing.T) {
	t.Parallel()

	toolsetID := uuid.NewString()
	ctx, engine, recorded := filterFixture(t, toolsetID)

	allowed, err := filterToolsByMCPConnect(ctx, testenv.NewLogger(t), engine, toolsetID, uuid.NewString(), filterTools(50))

	require.NoError(t, err)
	require.Len(t, allowed, 1)
	require.Equal(t, "tool_1", allowed[0].Name)
	require.Zero(t, *recorded)
}

func TestFilterToolsByMCPConnect_StopsAfterCancel(t *testing.T) {
	t.Parallel()

	toolsetID := uuid.NewString()
	ctx, engine, _ := filterFixture(t, toolsetID)
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	_, err := filterToolsByMCPConnect(ctx, testenv.NewLogger(t), engine, toolsetID, uuid.NewString(), filterTools(50))

	require.ErrorIs(t, err, context.Canceled)
}
