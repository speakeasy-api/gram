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
	docs := metaMember{serverID: uuid.New(), projectID: uuid.New(), slug: "docs", name: "Docs"}
	wiki := metaMember{serverID: uuid.New(), projectID: uuid.New(), slug: "wiki", name: "Wiki"}
	added := metaMember{serverID: uuid.New(), projectID: uuid.New(), slug: "added", name: "Added"}
	gate := &metaGateContext{
		catalog: &connectionCatalog{
			Members: map[string]connectionCatalogMember{
				docs.slug: {ServerID: docs.serverID, ProjectID: docs.projectID},
				wiki.slug: {ServerID: wiki.serverID, ProjectID: wiki.projectID},
			},
			Tools: []*toolListEntry{{Name: "docs--search"}},
		},
		catalogRef:    &toolfilter.CatalogReference{ID: uuid.New(), Mode: toolfilter.CatalogModeLive, AllTools: false},
		hiddenMembers: []metaMember{docs, wiki, added},
	}
	listed := listServersForGate(t, gate)
	// Wiki was approved with no tool selected; Added is outside the approval,
	// so no selection narrows it.
	require.Equal(t, []metamcp.HiddenServer{{Slug: "docs", Name: "Docs"}, {Slug: "added", Name: "Added"}}, listed.Hidden)
	// Showing Added again would not approve it, so it is unapproved too.
	require.Equal(t, []metamcp.UnapprovedServer{{Slug: "added", Name: "Added"}}, listed.Unapproved)
}

func TestListServersReportsUnapprovedMembers(t *testing.T) {
	t.Parallel()
	gate := &metaGateContext{
		catalog:           &connectionCatalog{Members: map[string]connectionCatalogMember{}},
		catalogRef:        &toolfilter.CatalogReference{ID: uuid.New(), Mode: toolfilter.CatalogModeLive, AllTools: true},
		unapprovedMembers: []metaMember{{serverID: uuid.New(), slug: "added", name: "Added"}},
	}
	listed := listServersForGate(t, gate)
	require.Equal(t, []metamcp.UnapprovedServer{{Slug: "added", Name: "Added"}}, listed.Unapproved)
	require.Equal(t, metamcp.UnapprovedNote, listed.Note)

	gate.unapprovedMembers = nil
	listed = listServersForGate(t, gate)
	require.Empty(t, listed.Unapproved)
	require.Empty(t, listed.Note)
}

func listServersForGate(t *testing.T, gate *metaGateContext) metamcp.ListServersResult {
	t.Helper()
	body, err := (&Service{}).handleMetaListServersCall(t.Context(), testenv.NewLogger(t), gate, nil, &rawRequest{})
	require.NoError(t, err)

	var rpc struct {
		Result toolCallResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &rpc))
	var listed metamcp.ListServersResult
	require.NoError(t, json.Unmarshal(rpc.Result.StructuredContent, &listed))
	return listed
}

func TestCatalogNotApprovedMessageSeparatesSkippedFromAdded(t *testing.T) {
	t.Parallel()
	skipped := uuid.New()
	require.Equal(t, catalogMemberNotApproved, (&connectionCatalog{}).notApprovedMessage(skipped), "a catalog without the shown list keeps the combined message")
	catalog := &connectionCatalog{ShownServerIDs: []uuid.UUID{skipped}}
	require.Equal(t, catalogMemberSkippedAtConsent, catalog.notApprovedMessage(skipped))
	require.Equal(t, catalogMemberAddedSinceConsent, catalog.notApprovedMessage(uuid.New()))

	moved := uuid.New()
	catalog = &connectionCatalog{
		Members:        map[string]connectionCatalogMember{"moved": {ServerID: moved, ProjectID: uuid.New()}},
		ShownServerIDs: []uuid.UUID{moved},
	}
	require.Equal(t, catalogMemberNotApproved, catalog.notApprovedMessage(moved), "a server approved under another project was neither skipped nor added")
}

func TestConnectionStatusExplainsHiddenMembersOutsideTheApproval(t *testing.T) {
	t.Parallel()
	approved := metaMember{serverID: uuid.New(), projectID: uuid.New(), slug: "docs", name: "Docs"}
	added := metaMember{serverID: uuid.New(), projectID: uuid.New(), slug: "added", name: "Added"}
	gate := &metaGateContext{
		catalog: &connectionCatalog{
			Members:        map[string]connectionCatalogMember{approved.slug: {ServerID: approved.serverID, ProjectID: approved.projectID}},
			ShownServerIDs: []uuid.UUID{approved.serverID},
		},
		catalogRef:    &toolfilter.CatalogReference{ID: uuid.New(), Mode: toolfilter.CatalogModeLive, AllTools: true},
		hiddenMembers: []metaMember{approved, added},
	}
	body, err := (&Service{}).handleMetaConnectionStatusCall(t.Context(), testenv.NewLogger(t), gate, nil, &rawRequest{})
	require.NoError(t, err)

	var rpc struct {
		Result toolCallResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &rpc))
	var status metamcp.ConnectionStatusResult
	require.NoError(t, json.Unmarshal(rpc.Result.StructuredContent, &status))
	require.Equal(t, []metamcp.ConnectionStatusServer{
		{Slug: "docs", Name: "Docs", Status: metamcp.SignInHidden},
		{Slug: "added", Name: "Added", Status: metamcp.SignInHidden, Detail: catalogMemberAddedSinceConsent},
	}, status.Servers)
	require.Contains(t, status.Text(), "- Added: hidden by the user ("+catalogMemberAddedSinceConsent+")")
	require.Contains(t, status.Text(), "- Docs: hidden by the user\n")
}
