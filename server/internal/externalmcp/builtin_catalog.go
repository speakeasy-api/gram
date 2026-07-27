package externalmcp

import (
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
)

// builtInRegistryID identifies catalog entries maintained by Gram rather than
// fetched from an external registry. It is stable so catalog detail links and
// client-side selection keys remain valid across requests.
var builtInRegistryID = uuid.MustParse("7d86f4d1-a510-4b79-bd32-91633968970d")

const (
	googleDriveRegistrySpecifier = "com.google.workspace/drive"
	googleDocsRegistrySpecifier  = "com.google.workspace/docs"
)

type builtInCatalogDefinition struct {
	specifier        string
	title            string
	description      string
	iconURL          string
	remoteURL        string
	setupAPI         string
	setupMCPService  string
	scopes           []string
	migrationSummary string
	tools            []*types.ExternalMCPTool
}

var builtInCatalogDefinitions = []builtInCatalogDefinition{
	{
		specifier:       googleDriveRegistrySpecifier,
		title:           "Google Drive",
		description:     "Google's official remote MCP server for searching, reading, creating, and managing files in Google Drive.",
		iconURL:         "https://www.gstatic.com/images/branding/product/2x/drive_2020q4_48dp.png",
		remoteURL:       "https://drivemcp.googleapis.com/mcp/v1",
		setupAPI:        "drive.googleapis.com",
		setupMCPService: "drivemcp.googleapis.com",
		scopes: []string{
			"https://www.googleapis.com/auth/drive.readonly",
			"https://www.googleapis.com/auth/drive.file",
		},
		migrationSummary: "Use Drive for file discovery and creation. Its create_file tool accepts textContent and contentMimeType and converts supported content, including HTML, to Google-native files.",
		tools: []*types.ExternalMCPTool{
			builtInTool("copy_file", "Copy an existing file in Google Drive.", false),
			builtInTool("create_file", "Create or upload a file, optionally converting supported content to a Google-native file.", false),
			builtInTool("download_file_content", "Download a file's content.", true),
			builtInTool("get_file_metadata", "Get metadata for a Drive file.", true),
			builtInTool("get_file_permissions", "List permissions for a Drive file.", true),
			builtInTool("list_recent_files", "List recently used Drive files.", true),
			builtInTool("read_file_content", "Read a natural-language representation of a Drive file.", true),
			builtInTool("search_files", "Search for files in Google Drive.", true),
		},
	},
	{
		specifier:       googleDocsRegistrySpecifier,
		title:           "Google Docs",
		description:     "Google's official remote MCP server for reading and richly editing native Google Docs.",
		iconURL:         "https://www.gstatic.com/images/branding/product/2x/docs_2020q4_48dp.png",
		remoteURL:       "https://docsmcp.googleapis.com/mcp/v1",
		setupAPI:        "docs.googleapis.com",
		setupMCPService: "docsmcp.googleapis.com",
		scopes: []string{
			"https://www.googleapis.com/auth/drive.readonly",
			"https://www.googleapis.com/auth/drive.file",
			"https://www.googleapis.com/auth/documents.readonly",
			"https://www.googleapis.com/auth/documents",
		},
		migrationSummary: "Use Docs after Drive creates the native document. Its update_doc tool accepts documents.batchUpdate requests for headings, text styles, tables, images, headers, footers, and document styles.",
		tools: []*types.ExternalMCPTool{
			builtInTool("read_doc", "Read a Google Doc's text and structural representation.", true),
			builtInTool("update_doc", "Apply Google Docs documents.batchUpdate requests to a document.", false),
		},
	},
}

func builtInTool(name, description string, readOnly bool) *types.ExternalMCPTool {
	return &types.ExternalMCPTool{
		Name:        &name,
		Description: &description,
		InputSchema: nil,
		Annotations: map[string]any{
			"readOnlyHint": readOnly,
		},
	}
}

func listBuiltInCatalog(search string) []*types.ExternalMCPServerEntry {
	registryID := builtInRegistryID.String()
	needle := strings.ToLower(strings.TrimSpace(search))
	servers := make([]*types.ExternalMCPServerEntry, 0, len(builtInCatalogDefinitions))

	for _, definition := range builtInCatalogDefinitions {
		if needle != "" &&
			!strings.Contains(strings.ToLower(definition.specifier), needle) &&
			!strings.Contains(strings.ToLower(definition.title), needle) &&
			!strings.Contains(strings.ToLower(definition.description), needle) {
			continue
		}

		title := definition.title
		iconURL := definition.iconURL
		meta := builtInCatalogMeta(definition)
		servers = append(servers, &types.ExternalMCPServerEntry{
			RegistrySpecifier:                   definition.specifier,
			Version:                             "1.0.0",
			Description:                         definition.description,
			ToolsetID:                           nil,
			McpServerID:                         nil,
			RegistryID:                          &registryID,
			OrganizationMcpCollectionRegistryID: nil,
			Title:                               &title,
			IconURL:                             &iconURL,
			Meta:                                meta,
			ToolCount:                           len(definition.tools),
			IsReadOnly:                          false,
			SupportsDcr:                         false,
			Remotes: []*types.ExternalMCPRemote{
				{
					URL:           definition.remoteURL,
					TransportType: "streamable-http",
					Headers:       nil,
					Variables:     nil,
				},
			},
		})
	}

	return servers
}

func getBuiltInCatalogDetails(specifier string) *types.ExternalMCPServer {
	registryID := builtInRegistryID.String()
	for _, definition := range builtInCatalogDefinitions {
		if definition.specifier != specifier {
			continue
		}

		title := definition.title
		iconURL := definition.iconURL
		return &types.ExternalMCPServer{
			RegistrySpecifier:                   definition.specifier,
			Version:                             "1.0.0",
			Description:                         definition.description,
			ToolsetID:                           nil,
			McpServerID:                         nil,
			RegistryID:                          &registryID,
			OrganizationMcpCollectionRegistryID: nil,
			Title:                               &title,
			IconURL:                             &iconURL,
			Meta:                                builtInCatalogMeta(definition),
			Tools:                               definition.tools,
			Remotes: []*types.ExternalMCPRemote{
				{
					URL:           definition.remoteURL,
					TransportType: "streamable-http",
					Headers:       nil,
					Variables:     nil,
				},
			},
		}
	}

	return nil
}

func builtInCatalogMeta(definition builtInCatalogDefinition) map[string]any {
	return map[string]any{
		"com.pulsemcp/server": map[string]any{
			"isOfficial": true,
		},
		"com.pulsemcp/server-version": map[string]any{
			"source":   "https://developers.google.com/workspace/guides/configure-mcp-servers",
			"status":   "active",
			"isLatest": true,
			"remotes[0]": map[string]any{
				"authOptions": []map[string]any{
					{"type": "oauth"},
				},
			},
		},
		"com.getgram/catalog": map[string]any{
			"provider":                   "google-workspace",
			"setupDocumentationUrl":      "https://developers.google.com/workspace/guides/configure-mcp-servers",
			"requiredApis":               []string{definition.setupAPI},
			"requiredMcpServices":        []string{definition.setupMCPService},
			"requiredScopes":             definition.scopes,
			"oauthClientOwnership":       "customer",
			"migrationFromCommunityMcp":  definition.migrationSummary,
			"supportsCombinedAssistants": true,
		},
	}
}

func mergeBuiltInCatalog(registryServers []*types.ExternalMCPServerEntry, search string) []*types.ExternalMCPServerEntry {
	builtIns := listBuiltInCatalog(search)
	seen := make(map[string]struct{}, len(builtIns)+len(registryServers))
	result := make([]*types.ExternalMCPServerEntry, 0, len(builtIns)+len(registryServers))

	for _, server := range append(builtIns, registryServers...) {
		if _, ok := seen[server.RegistrySpecifier]; ok {
			continue
		}
		seen[server.RegistrySpecifier] = struct{}{}
		result = append(result, server)
	}

	return result
}
