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

// cancelOnFirstCheck returns a request context and an engine whose first
// challenge-log flag check cancels that context, plus a counter of checks.
func cancelOnFirstCheck(t *testing.T, toolsetID string) (context.Context, *authz.Engine, *int) {
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
		Selector:     authz.Selector{"resource_kind": "mcp", "resource_id": toolsetID},
	}})
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)

	checks := 0
	engine := authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) {
		checks++
		cancel()
		return false, nil
	}, workos.NewStubClient())
	return ctx, engine, &checks
}

func TestFilterToolsByMCPConnect_StopsAfterCancel(t *testing.T) {
	t.Parallel()

	toolsetID := uuid.NewString()
	ctx, engine, checks := cancelOnFirstCheck(t, toolsetID)
	tools := make([]*toolListEntry, 50)
	for i := range tools {
		tools[i] = &toolListEntry{Name: fmt.Sprintf("tool_%d", i)}
	}

	_, err := filterToolsByMCPConnect(ctx, testenv.NewLogger(t), engine, toolsetID, uuid.NewString(), tools)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, *checks)
}

func TestFilterCatalogByMCPConnect_StopsAfterCancel(t *testing.T) {
	t.Parallel()

	toolsetID := uuid.NewString()
	ctx, engine, checks := cancelOnFirstCheck(t, toolsetID)
	catalog := &memberCatalog{byName: map[string]*toolListEntry{}}
	for i := range 50 {
		name := fmt.Sprintf("tool_%d", i)
		catalog.byName[name] = &toolListEntry{Name: name}
	}

	err := filterCatalogByMCPConnect(ctx, testenv.NewLogger(t), engine, toolsetID, uuid.NewString(), "member", catalog)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, *checks)
}
