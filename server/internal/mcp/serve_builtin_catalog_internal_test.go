package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestBuiltinMemberCannotBypassConnectionCatalog(t *testing.T) {
	t.Parallel()
	// Synthetic members are machine-only today. If a catalog reaches this path,
	// it must still reject an unapproved member before built-in dispatch.
	service := &Service{}
	member := metaMember{backend: metaMemberBackendBuiltin, builtinKey: "slack", serverID: uuid.New(), projectID: uuid.New(), slug: "builtin_slack"}
	gate := &metaGateContext{catalog: &connectionCatalog{}, catalogMode: toolfilter.CatalogModeLive}
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	_, err := service.describeMetaMember(ctx, logger, gate, member)
	require.ErrorContains(t, err, catalogMemberNotApproved)
	result, err := service.executeMetaMemberTool(ctx, logger, gate, []metaMember{member}, &rawRequest{}, "builtin_slack--test", nil, nil)
	require.NoError(t, err)
	require.Contains(t, string(result), catalogMemberNotApproved)
	gate.catalogMode = toolfilter.CatalogModeCached
	status, message := service.memberSignInStatus(ctx, logger, gate, member)
	require.Equal(t, metamcp.SignInNeedsConsent, status)
	require.Equal(t, catalogMemberNotApproved, message)
	gate.catalog = nil
	status, message = service.memberSignInStatus(ctx, logger, gate, member)
	require.Equal(t, metamcp.SignInNotRequired, status)
	require.Empty(t, message)
}
