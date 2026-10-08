package mcp_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// consentInventoryNames lists the selectable tools and the role-hidden names
// a consent tools/list returned.
func consentInventoryNames(t *testing.T, body []byte) ([]string, []string) {
	t.Helper()

	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Meta map[string]json.RawMessage `json:"_meta"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &resp), string(body))

	tools := make([]string, 0, len(resp.Result.Tools))
	for _, tool := range resp.Result.Tools {
		tools = append(tools, tool.Name)
	}
	slices.Sort(tools)

	var hidden struct {
		Names []string `json:"names"`
	}
	if raw, ok := resp.Result.Meta["gram.dev/roleHiddenTools"]; ok {
		require.NoError(t, json.Unmarshal(raw, &hidden))
	}
	slices.Sort(hidden.Names)
	return tools, hidden.Names
}

// The consent picker offers exactly what the minted session can reach: under
// a grant narrowed only by disposition, tools carrying no annotations are
// role-hidden rather than selectable. A grant naming such a tool makes it
// selectable.
func TestServeConsentMCP_PrivateToolsetDispositionGrantHidesUnannotatedTools(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		narrowing map[string]string
		tools     []string
		hidden    []string
	}{
		{
			name:      "read-only rule",
			narrowing: map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly},
			tools:     []string{"reader"},
			hidden:    []string{"eraser", "writer"},
		},
		{
			name:      "rule naming an unannotated tool",
			narrowing: map[string]string{authz.SelectorKeyTool: "writer"},
			tools:     []string{"writer"},
			hidden:    []string{"eraser", "reader"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			toolset, issuer := seedSelectionToolset(t, ctx, ti)
			client, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
				UserSessionIssuerID:     issuer.ID,
				ClientID:                "consent-client-" + uuid.NewString()[:8],
				ClientName:              "consent test client",
				RedirectUris:            []string{"http://localhost:3000/callback"},
				TokenEndpointAuthMethod: "none",
			})
			require.NoError(t, err)

			endpointSlug := "consent-unclassified-" + uuid.NewString()
			mcpServer := createToolsetMcpEndpoint(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, endpointSlug, "private", uuid.NullUUID{}, issuer.ID)
			seedMockUserMCPGrant(t, ctx, ti.conn, toolset.OrganizationID, mcpServer.ID, tc.narrowing)

			stateID, csrfToken := seedModernConsentChallenge(t, ctx, ti, issuer.ID, client, mcpServer.ID, endpointSlug)
			endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, endpointSlug, "x/mcp")
			require.NoError(t, err)

			list := serveConsentMCPRequest(t, context.Background(), ti, endpoint, stateID, csrfToken, uuid.NewString(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
			tools, hidden := consentInventoryNames(t, list.Body.Bytes())
			require.Equal(t, tc.tools, tools)
			require.Equal(t, tc.hidden, hidden)
		})
	}
}
