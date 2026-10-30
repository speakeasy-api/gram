package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
)

func TestSkillMemberServerIDsFollowsTheConsentSelection(t *testing.T) {
	t.Parallel()

	docs := metaMember{serverID: uuid.New(), slug: "docs", name: "Docs"}
	wiki := metaMember{serverID: uuid.New(), slug: "wiki", name: "Wiki"}
	members := []metaMember{docs, wiki}

	selected := &metaGateContext{
		catalog:    &connectionCatalog{Tools: []*toolListEntry{{Name: "docs--search"}}},
		catalogRef: &toolfilter.CatalogReference{ID: uuid.New(), Mode: toolfilter.CatalogModeLive, AllTools: false},
	}
	require.Equal(t, []uuid.UUID{docs.serverID}, skillMemberServerIDs(selected, members),
		"a member with no selected tool contributes no skills")

	allTools := &metaGateContext{
		catalog:    &connectionCatalog{Tools: []*toolListEntry{{Name: "docs--search"}}},
		catalogRef: &toolfilter.CatalogReference{ID: uuid.New(), Mode: toolfilter.CatalogModeLive, AllTools: true},
	}
	require.Equal(t, []uuid.UUID{docs.serverID, wiki.serverID}, skillMemberServerIDs(allTools, members))

	unconsented := &metaGateContext{}
	require.Equal(t, []uuid.UUID{docs.serverID, wiki.serverID}, skillMemberServerIDs(unconsented, members))
}
