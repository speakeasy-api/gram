package mcp

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReconnectCatalogChanges(t *testing.T) {
	t.Parallel()
	old := &connectionCatalog{Tools: []*toolListEntry{{Name: "source--removed"}, {Name: "source--changed", Description: "old"}, {Name: "source--same"}}}
	var inventory consentToolInventory
	for _, tool := range []*toolListEntry{{Name: "source--added"}, {Name: "source--changed", Description: "new"}, {Name: "source--same"}} {
		inventory.Tools = append(inventory.Tools, consentInventoryTool{Name: tool.Name, Definition: tool, DefinitionHash: consentEvidenceHash(tool)})
	}
	changes := compareCatalogInventory(old, inventory)
	require.Equal(t, []string{"source--added"}, changes.Added)
	require.Equal(t, []string{"source--removed"}, changes.Removed)
	require.Equal(t, []string{"source--changed"}, changes.Changed)
}
