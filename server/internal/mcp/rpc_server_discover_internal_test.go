package mcp

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	metadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServerDiscoverNotificationHasNoResponse(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{"hosted", "platform", "meta"} {
		t.Run(surface, func(t *testing.T) {
			t.Parallel()
			logger := testenv.NewLogger(t)
			service := &Service{logger: logger,
				toolsetsRepo: toolsetsrepo.New(failingDBTX{}), mcpMetadataRepo: metadatarepo.New(failingDBTX{})}
			resolution := mcpversions.Resolution{Declared: mcpversions.Version20260728, InEffect: mcpversions.Version20260728}
			req := &rawRequest{JSONRPC: "2.0", Method: mcpversions.MethodServerDiscover,
				Params: json.RawMessage(`{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`)}
			var body json.RawMessage
			var err error
			switch surface {
			case "hosted":
				body, err = service.handleRequest(t.Context(), &mcpInputs{protocolVersion: resolution}, req)
			case "platform":
				body, err = service.handlePlatformToolsetRequest(t.Context(), nil, platformtools.Toolset{}, req, "", &resolution)
			case "meta":
				body, err = service.handleMetaMCPRequest(t.Context(), logger, nil, &metamcprepo.MetaMcpServer{},
					&metaGateContext{protocolVersion: resolution}, req, resolution.Declared)
			}
			require.NoError(t, err)
			require.Nil(t, body, "accepted notifications must leave the transport a bodyless acknowledgement")
		})
	}
}

func TestServerDiscoverPreservesZeroAndEmptyStringIDs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		id   mcpjsonrpc.ID
		want string
	}{
		{name: "zero", id: mcpjsonrpc.NumberID(0), want: "0"},
		{name: "empty string", id: mcpjsonrpc.StringID(""), want: `""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body, err := handleServerDiscover(t.Context(), testenv.NewLogger(t), tc.id,
				describePlatformServer(), mcpversions.SupportedPlatformToolset())
			require.NoError(t, err)
			var response struct {
				ID json.RawMessage `json:"id"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			require.JSONEq(t, tc.want, string(response.ID))
		})
	}
}

func TestPlatformServerDiscoverIsCallerVarying(t *testing.T) {
	t.Parallel()
	resolution := mcpversions.Resolution{Declared: mcpversions.Version20260728, InEffect: mcpversions.Version20260728}
	body, err := (&Service{logger: testenv.NewLogger(t)}).handlePlatformToolsetRequest(t.Context(), nil, platformtools.Toolset{},
		&rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.NumberID(1), Method: mcpversions.MethodServerDiscover}, "", &resolution)
	require.NoError(t, err)
	var response struct {
		Result struct {
			CacheScope string `json:"cacheScope"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	// Every platform caller presents an assistant token, so discovery must
	// never license a shared cache to serve it across callers.
	require.Equal(t, "private", response.Result.CacheScope)
}

func TestHostedServerDiscoverStoredInstructionsAndCachePrivacy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                  string
		public, authenticated bool
		wrapperPublic         *bool
		scope                 string
	}{
		{name: "anonymous public", public: true, scope: "public"},
		{name: "authenticated public", public: true, authenticated: true, scope: "private"},
		{name: "authenticated private", authenticated: true, scope: "private"},
		{name: "private stays private", scope: "private"},
		{name: "private wrapper over public toolset", public: true, wrapperPublic: new(false), scope: "private"},
		{name: "public wrapper over private toolset", wrapperPublic: new(true), scope: "public"},
		{name: "authenticated public wrapper", authenticated: true, wrapperPublic: new(true), scope: "private"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			conn, err := TestInfra.CloneTestDatabase(t, "mcp_discovery_description")
			require.NoError(t, err)
			require.NoError(t, orgsrepo.New(conn).CreateOrganizationMetadata(ctx, orgsrepo.CreateOrganizationMetadataParams{
				ID: "org-discovery-test", Name: "Discovery Test", Slug: "discovery-test",
			}))
			project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
				Name: "Discovery", Slug: "discovery", OrganizationID: "org-discovery-test",
			})
			require.NoError(t, err)
			toolsets := toolsetsrepo.New(conn)
			toolset, err := toolsets.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
				Name: "Discovery", Slug: "discovery", OrganizationID: project.OrganizationID, ProjectID: project.ID,
				McpSlug: conv.ToPGText("discovery"), McpEnabled: true,
			})
			require.NoError(t, err)
			_, err = toolsets.UpdateToolset(ctx, toolsetsrepo.UpdateToolsetParams{
				Name: toolset.Name, Slug: toolset.Slug, ProjectID: project.ID,
				McpSlug: toolset.McpSlug, McpEnabled: true, McpIsPublic: tc.public,
			})
			require.NoError(t, err)
			metadata := metadatarepo.New(conn)
			const instructions = "Search the example catalog before choosing a tool."
			_, err = metadata.UpsertMetadata(ctx, metadatarepo.UpsertMetadataParams{
				ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ProjectID: project.ID,
				Instructions: conv.ToPGText(instructions),
			})
			require.NoError(t, err)
			// The request builder already loaded the toolset, so describing the
			// server must not look it up again by slug.
			service := &Service{logger: testenv.NewLogger(t), toolsetsRepo: toolsetsrepo.New(failingDBTX{}), mcpMetadataRepo: metadata}
			body, err := service.handleRequest(ctx, &mcpInputs{
				projectID: project.ID, toolset: toolset.Slug,
				toolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, toolsetIsPublic: new(tc.public),
				authenticated: tc.authenticated, wrapperIsPublic: tc.wrapperPublic,
				protocolVersion: mcpversions.Resolution{Declared: mcpversions.Version20260728, InEffect: mcpversions.Version20260728},
			}, &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.NumberID(1), Method: mcpversions.MethodServerDiscover})
			require.NoError(t, err)
			var response struct {
				Result struct {
					Instructions      string                     `json:"instructions"`
					CacheScope        string                     `json:"cacheScope"`
					TTLMs             *int                       `json:"ttlMs"`
					Capabilities      map[string]json.RawMessage `json:"capabilities"`
					SupportedVersions []string                   `json:"supportedVersions"`
					Meta              map[string]serverInfo      `json:"_meta"`
				} `json:"result"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			require.Equal(t, instructions, response.Result.Instructions)
			require.Equal(t, tc.scope, response.Result.CacheScope)
			require.NotNil(t, response.Result.TTLMs)
			require.Zero(t, *response.Result.TTLMs)
			require.ElementsMatch(t, []string{"tools", "prompts", "resources"}, slices.Collect(maps.Keys(response.Result.Capabilities)))
			require.Equal(t, mcpversions.SupportedHostedToolset(), response.Result.SupportedVersions)
			require.Equal(t, serverInfoHostedToolset, response.Result.Meta[metaKeyServerInfo])
		})
	}
}
