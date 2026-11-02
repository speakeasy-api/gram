package mcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReconnectCatalogPreservesOtherSelections(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		for _, allTools := range []bool{false, true} {
			t.Run(map[bool]string{true: "existing", false: "new"}[existing]+map[bool]string{true: "-all", false: "-selected"}[allTools], func(t *testing.T) {
				t.Parallel()
				other := &toolListEntry{Name: "other--selected"}
				catalog := &connectionCatalog{ID: uuid.New(), Members: map[string]connectionCatalogMember{"other": {ServerID: uuid.New()}}, Tools: []*toolListEntry{other}, ExpiresAt: time.Now().Add(time.Hour)}
				beforeExpiry, beforeID := catalog.ExpiresAt, catalog.ID
				member := connectionCatalogMember{ServerID: uuid.New()}
				if existing {
					catalog.Members["target"] = member
					catalog.Tools = append(catalog.Tools, &toolListEntry{Name: "target--kept"})
				}
				var tools []*toolListEntry
				for _, name := range []string{"target--kept", "target--added"} {
					definition := &toolListEntry{Name: name}
					tools = append(tools, definition)
				}
				catalog.replaceMember("target", &connectionCatalog{Members: map[string]connectionCatalogMember{"target": member}, Tools: tools}, allTools)
				var names []string
				for _, tool := range catalog.Tools {
					names = append(names, tool.Name)
				}
				require.Contains(t, names, "other--selected")
				require.Contains(t, names, "target--kept")
				require.Equal(t, beforeExpiry, catalog.ExpiresAt)
				require.Equal(t, beforeID, catalog.ID)
				if !existing || allTools {
					require.Contains(t, names, "target--added")
				} else {
					require.NotContains(t, names, "target--added")
				}
				require.Same(t, other, catalog.Tools[0])
			})
		}
	}
}

func TestReconnectCatalogAddsPreviouslyUnselectedMember(t *testing.T) {
	t.Parallel()
	member := connectionCatalogMember{ServerID: uuid.New()}
	catalog := &connectionCatalog{Members: map[string]connectionCatalogMember{"target": member}}
	definition := &toolListEntry{Name: "target--new"}
	catalog.replaceMember("target", &connectionCatalog{Members: map[string]connectionCatalogMember{"target": member}, Tools: []*toolListEntry{definition}}, false)
	require.Equal(t, []*toolListEntry{definition}, catalog.Tools)
}
