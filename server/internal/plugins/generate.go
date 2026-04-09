package plugins

import (
	"encoding/json"
	"fmt"
)

// PluginServerInfo contains the resolved information for a single MCP server.
type PluginServerInfo struct {
	DisplayName string
	Policy      string
	// Resolved MCP URL (e.g. https://app.getgram.ai/mcp/{slug}).
	MCPURL string
	// Whether auth header should use Gram API key.
	UseGramAuth bool
}

// PluginInfo contains the data needed to generate packages for a single plugin.
type PluginInfo struct {
	Name        string
	Slug        string
	Description string
	Servers     []PluginServerInfo
}

// GenerateConfig holds org-level configuration for package generation.
type GenerateConfig struct {
	OrgName  string
	OrgEmail string
	// Base server URL (e.g. https://app.getgram.ai).
	ServerURL string
}

// GeneratePluginPackages produces the complete file map for a plugin distribution
// repository containing both Claude Code and Cursor plugins. Used for GitHub push.
func GeneratePluginPackages(plugins []PluginInfo, cfg GenerateConfig) (map[string][]byte, error) {
	files := make(map[string][]byte)

	var marketplacePlugins []marketplaceEntry
	for _, p := range plugins {
		if err := generateClaudePlugin(files, p, cfg); err != nil {
			return nil, fmt.Errorf("generate claude plugin %s: %w", p.Slug, err)
		}
		if err := generateCursorPlugin(files, p, cfg); err != nil {
			return nil, fmt.Errorf("generate cursor plugin %s: %w", p.Slug, err)
		}
		marketplacePlugins = append(marketplacePlugins, marketplaceEntry{
			Name:        p.Slug,
			Source:      "./" + p.Slug,
			Description: p.Description,
		})
	}

	manifest, err := marshalJSON(marketplaceManifest{
		Name:    cfg.OrgName + "-gram",
		Owner:   marketplaceOwner{Name: cfg.OrgName, Email: cfg.OrgEmail},
		Plugins: marketplacePlugins,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal marketplace.json: %w", err)
	}
	files["marketplace.json"] = manifest

	return files, nil
}

// GeneratePluginPackagesForPlatform produces files for a single platform.
// Used for per-platform ZIP downloads.
func GeneratePluginPackagesForPlatform(plugins []PluginInfo, cfg GenerateConfig, platform string) (map[string][]byte, error) {
	files := make(map[string][]byte)

	for _, p := range plugins {
		switch platform {
		case "claude":
			if err := generateClaudePlugin(files, p, cfg); err != nil {
				return nil, fmt.Errorf("generate claude plugin %s: %w", p.Slug, err)
			}
		case "cursor":
			if err := generateCursorPlugin(files, p, cfg); err != nil {
				return nil, fmt.Errorf("generate cursor plugin %s: %w", p.Slug, err)
			}
		}
	}

	if platform == "claude" {
		var marketplacePlugins []marketplaceEntry
		for _, p := range plugins {
			marketplacePlugins = append(marketplacePlugins, marketplaceEntry{
				Name:        p.Slug,
				Source:      "./" + p.Slug,
				Description: p.Description,
			})
		}
		manifest, err := marshalJSON(marketplaceManifest{
			Name:    cfg.OrgName + "-gram",
			Owner:   marketplaceOwner{Name: cfg.OrgName, Email: cfg.OrgEmail},
			Plugins: marketplacePlugins,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal marketplace.json: %w", err)
		}
		files["marketplace.json"] = manifest
	}

	return files, nil
}

func generateClaudePlugin(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	prefix := p.Slug + "/"

	// .claude-plugin/plugin.json
	pluginJSON, err := marshalJSON(claudePluginMeta{
		Name:        p.Slug,
		Description: p.Description,
		Version:     "0.1.0",
		Author:      pluginAuthor{Name: cfg.OrgName, URL: "https://getgram.ai"},
		Homepage:    "https://getgram.ai",
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[prefix+".claude-plugin/plugin.json"] = pluginJSON

	// .mcp.json
	mcpServers := make(map[string]claudeMCPServer)
	for _, s := range p.Servers {
		server := claudeMCPServer{
			Type:    "http",
			URL:     s.MCPURL,
			Headers: nil,
		}
		if s.UseGramAuth {
			server.Headers = map[string]string{
				"Authorization": "Bearer ${GRAM_API_KEY}",
			}
		}
		mcpServers[s.DisplayName] = server
	}
	mcpJSON, err := marshalJSON(claudeMCPConfig{MCPServers: mcpServers})
	if err != nil {
		return fmt.Errorf("marshal .mcp.json: %w", err)
	}
	files[prefix+".mcp.json"] = mcpJSON

	// hooks/hooks.json — same structure as the existing gram-hooks plugin.
	files[prefix+"hooks/hooks.json"] = []byte(claudeHooksJSON)

	// hooks/send_hook.sh
	files[prefix+"hooks/send_hook.sh"] = []byte(claudeSendHookSH(cfg.ServerURL))

	return nil
}

func generateCursorPlugin(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	prefix := p.Slug + "-cursor/"

	// .cursor-plugin/plugin.json
	pluginJSON, err := marshalJSON(cursorPluginMeta{
		Name:        p.Slug + "-cursor",
		DisplayName: p.Name + " (Cursor)",
		Description: p.Description,
		Version:     "0.1.0",
		Author:      pluginAuthor{Name: cfg.OrgName, URL: "https://getgram.ai"},
		Homepage:    "https://getgram.ai",
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[prefix+".cursor-plugin/plugin.json"] = pluginJSON

	// mcp.json
	mcpServers := make(map[string]cursorMCPServer)
	for _, s := range p.Servers {
		server := cursorMCPServer{
			URL:     s.MCPURL,
			Headers: nil,
		}
		if s.UseGramAuth {
			server.Headers = map[string]string{
				"Authorization": "Bearer ${env:GRAM_API_KEY}",
			}
		}
		mcpServers[s.DisplayName] = server
	}
	mcpJSON, err := marshalJSON(cursorMCPConfig{MCPServers: mcpServers})
	if err != nil {
		return fmt.Errorf("marshal mcp.json: %w", err)
	}
	files[prefix+"mcp.json"] = mcpJSON

	// hooks/hooks.json
	files[prefix+"hooks/hooks.json"] = []byte(cursorHooksJSON)

	// hooks/send_hook.sh
	files[prefix+"hooks/send_hook.sh"] = []byte(cursorSendHookSH(cfg.ServerURL))

	return nil
}

// ResolveServerMCPURL builds the MCP URL for a plugin server based on its source type.
func ResolveServerMCPURL(serverURL string, toolsetMCPSlug *string, registryServerSpecifier *string, externalURL *string) (url string, useGramAuth bool) {
	switch {
	case toolsetMCPSlug != nil && *toolsetMCPSlug != "":
		return fmt.Sprintf("%s/mcp/%s", serverURL, *toolsetMCPSlug), true
	case externalURL != nil && *externalURL != "":
		return *externalURL, false
	case registryServerSpecifier != nil && *registryServerSpecifier != "":
		// Registry servers are proxied through Gram for now.
		return *registryServerSpecifier, false
	default:
		return "", false
	}
}

// --- JSON types ---

type marketplaceManifest struct {
	Name    string             `json:"name"`
	Owner   marketplaceOwner   `json:"owner"`
	Plugins []marketplaceEntry `json:"plugins"`
}

type marketplaceOwner struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type marketplaceEntry struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Description string `json:"description"`
}

type pluginAuthor struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type claudePluginMeta struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Version     string       `json:"version"`
	Author      pluginAuthor `json:"author"`
	Homepage    string       `json:"homepage"`
}

type claudeMCPConfig struct {
	MCPServers map[string]claudeMCPServer `json:"mcpServers"`
}

type claudeMCPServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type cursorPluginMeta struct {
	Name        string       `json:"name"`
	DisplayName string       `json:"displayName"`
	Description string       `json:"description"`
	Version     string       `json:"version"`
	Author      pluginAuthor `json:"author"`
	Homepage    string       `json:"homepage"`
}

type cursorMCPConfig struct {
	MCPServers map[string]cursorMCPServer `json:"mcpServers"`
}

type cursorMCPServer struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func marshalJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}
	return b, nil
}

// --- Static hook templates ---

const claudeHooksJSON = `{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PLUGIN_ROOT/hooks/send_hook.sh\"",
            "async": true
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PLUGIN_ROOT/hooks/send_hook.sh\"",
            "async": true
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PLUGIN_ROOT/hooks/send_hook.sh\"",
            "async": true
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PLUGIN_ROOT/hooks/send_hook.sh\"",
            "async": true
          }
        ]
      }
    ],
    "PostToolUseFailure": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PLUGIN_ROOT/hooks/send_hook.sh\"",
            "async": true
          }
        ]
      }
    ]
  }
}
`

const cursorHooksJSON = `{
  "hooks": {
    "preToolUse": [
      {
        "command": "./hooks/send_hook.sh",
        "timeout": 10
      }
    ],
    "postToolUse": [
      {
        "command": "./hooks/send_hook.sh",
        "timeout": 10
      }
    ],
    "postToolUseFailure": [
      {
        "command": "./hooks/send_hook.sh",
        "timeout": 10
      }
    ]
  }
}
`

func claudeSendHookSH(serverURL string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
# Shared script to send hook events to Gram

server_url="${GRAM_HOOKS_SERVER_URL:-%s}"

curl -X POST \
  -H "Content-Type: application/json" \
  -d @- \
  --max-time 30 \
  "${server_url}/rpc/hooks.claude"
`, serverURL)
}

func cursorSendHookSH(serverURL string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
# Shared script to send Cursor hook events to Gram

server_url="${GRAM_HOOKS_SERVER_URL:-%s}"
api_key="${GRAM_API_KEY:-}"
project_slug="${GRAM_PROJECT_SLUG:-}"

# Fail silently if credentials are not configured
if [ -z "$api_key" ] || [ -z "$project_slug" ]; then
  echo '{}'
  exit 0
fi

curl -s -X POST \
  -H "Content-Type: application/json" \
  -H "Gram-Key: ${api_key}" \
  -H "Gram-Project: ${project_slug}" \
  -d @- \
  --max-time 10 \
  "${server_url}/rpc/hooks.cursor" 2>/dev/null || echo '{}'
`, serverURL)
}
