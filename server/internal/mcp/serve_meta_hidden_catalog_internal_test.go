package mcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestListServersReportsOnlyHiddenMembersTheConsentSelected(t *testing.T) {
	t.Parallel()
	gate := &metaGateContext{
		catalog:    &connectionCatalog{Tools: []*toolListEntry{{Name: "docs--search"}}},
		catalogRef: &toolfilter.CatalogReference{ID: uuid.New(), Mode: toolfilter.CatalogModeLive, AllTools: false},
		hiddenMembers: []metaMember{
			{serverID: uuid.New(), slug: "docs", name: "Docs"},
			{serverID: uuid.New(), slug: "wiki", name: "Wiki"},
		},
	}
	body, err := (&Service{}).handleMetaListServersCall(t.Context(), testenv.NewLogger(t), gate, nil, &rawRequest{})
	require.NoError(t, err)

	var rpc struct {
		Result toolCallResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &rpc))
	var listed metamcp.ListServersResult
	require.NoError(t, json.Unmarshal(rpc.Result.StructuredContent, &listed))
	require.Equal(t, []metamcp.HiddenServer{{Slug: "docs", Name: "Docs"}}, listed.Hidden)
}
