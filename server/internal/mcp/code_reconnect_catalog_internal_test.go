package mcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestReconnectCatalogChanges(t *testing.T) {
	t.Parallel()
	old := &connectionCatalog{Tools: []*toolListEntry{{Name: "source--removed"}, {Name: "source--changed", Description: "old"}, {Name: "source--same"}}}
	var inventory consentToolInventory
	for _, tool := range []*toolListEntry{{Name: "source--added"}, {Name: "source--changed", Description: "new"}, {Name: "source--same"}} {
		inventory.Tools = append(inventory.Tools, consentInventoryTool{Name: tool.Name, Definition: tool, DefinitionHash: consentEvidenceHash(tool)})
	}
	changes, err := compareCatalogInventory(old, inventory)
	require.NoError(t, err)
	require.Len(t, changes.Added, 1)
	require.Equal(t, "source--added", changes.Added[0].Name)
	require.Len(t, changes.Removed, 1)
	require.Equal(t, "source--removed", changes.Removed[0].Name)
	require.Len(t, changes.Changed, 1)
	require.Equal(t, "source--changed", changes.Changed[0].Name)
	require.Equal(t, []catalogFieldChange{{Name: "description", Before: "old", After: "new", HasBefore: true, HasAfter: true}}, changes.Changed[0].Fields)
}

func TestFreezeConnectionCatalogRejectsInvalidDefinitions(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "freeze_invalid_definitions")
	require.NoError(t, err)
	service := &Service{db: pool, logger: testenv.NewLogger(t)}
	endpoint := &ResolvedMcpEndpoint{MetaMcpServerID: validUUID(uuid.New())}
	subject := urn.NewAnonymousSubject(uuid.NewString())
	state := AuthnChallengeState{ID: uuid.NewString(), Subject: &subject}
	for _, tc := range []struct{ name, tool, hash, want string }{
		{name: "unapproved source", tool: "other--injected", want: "no verified source"},
		{name: "changed definition", tool: "target--changed", hash: "mismatch", want: "catalog is incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			definition := &toolListEntry{Name: tc.tool}
			hash := tc.hash
			if hash == "" {
				hash = consentEvidenceHash(definition)
			}
			inventory := &consentToolInventory{StateID: state.ID, Resource: endpointToolSelectionResource(endpoint), Complete: true, Tools: []consentInventoryTool{{Name: tc.tool, Definition: definition, DefinitionHash: hash}}}
			_, err := service.freezeConnectionCatalog(t.Context(), endpoint, state, state.ClientID, inventory, nil, toolfilter.CatalogModeCached, nil, time.Now().Add(time.Hour))
			require.ErrorContains(t, err, tc.want)
		})
	}
}
