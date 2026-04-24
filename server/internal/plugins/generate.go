package plugins

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// ServerEnvConfig represents a user-facing environment variable required by a server.
type ServerEnvConfig struct {
	VariableName string
	DisplayName  string // Shown to the user in Claude's userConfig prompt
}

// PluginServerInfo contains the resolved information for a single MCP server.
type PluginServerInfo struct {
	DisplayName string
	Policy      string
	// Resolved MCP URL (e.g. https://app.getgram.ai/mcp/{slug}).
	MCPURL string
	// IsPublic indicates whether the toolset is publicly accessible (no Gram API key needed).
	IsPublic bool
	// EnvConfigs are user-facing environment variables for public servers.
	EnvConfigs []ServerEnvConfig
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
	// APIKey is the plaintext Gram API key to inject into MCP server configs.
	// If empty, configs will use placeholder variables instead.
	APIKey string
}

// GeneratePluginPackages produces the complete file map for a plugin distribution
// repository containing both Claude Code and Cursor plugins. Used for GitHub push.
func GeneratePluginPackages(plugins []PluginInfo, cfg GenerateConfig) (map[string][]byte, error) {
	files := make(map[string][]byte)

	var claudePlugins []marketplaceEntry
	var cursorPlugins []marketplaceEntry
	var codexPlugins []codexMarketplaceEntry
	for _, p := range plugins {
		if err := generateClaudePlugin(files, p, cfg); err != nil {
			return nil, fmt.Errorf("generate claude plugin %s: %w", p.Slug, err)
		}
		if err := generateCursorPlugin(files, p, cfg); err != nil {
			return nil, fmt.Errorf("generate cursor plugin %s: %w", p.Slug, err)
		}
		if err := generateCodexPlugin(files, p, cfg); err != nil {
			return nil, fmt.Errorf("generate codex plugin %s: %w", p.Slug, err)
		}
		claudePlugins = append(claudePlugins, marketplaceEntry{
			Name:        p.Slug,
			Source:      "./" + p.Slug,
			Description: p.Description,
		})
		cursorPlugins = append(cursorPlugins, marketplaceEntry{
			Name:        p.Slug + "-cursor",
			Source:      "./" + p.Slug + "-cursor",
			Description: p.Description,
		})
		codexPlugins = append(codexPlugins, codexMarketplaceEntry{
			Name: p.Slug + "-codex",
			Source: codexMarketplaceSource{
				Source: "local",
				Path:   "./" + p.Slug + "-codex",
			},
			Policy: codexMarketplacePolicy{
				Installation:   "AVAILABLE",
				Authentication: codexAuthPolicy(p, cfg),
			},
		})
	}

	owner := marketplaceOwner{Name: cfg.OrgName, Email: cfg.OrgEmail}
	marketplaceName := conv.ToSlug(cfg.OrgName) + "-gram"

	claudeManifest, err := marshalJSON(marketplaceManifest{
		Name:    marketplaceName,
		Owner:   owner,
		Plugins: claudePlugins,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal claude marketplace.json: %w", err)
	}
	files[".claude-plugin/marketplace.json"] = claudeManifest

	cursorManifest, err := marshalJSON(marketplaceManifest{
		Name:    marketplaceName,
		Owner:   owner,
		Plugins: cursorPlugins,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal cursor marketplace.json: %w", err)
	}
	files[".cursor-plugin/marketplace.json"] = cursorManifest

	codexManifest, err := marshalJSON(codexMarketplaceManifest{
		Name:      marketplaceName,
		Interface: codexInterface{DisplayName: cfg.OrgName + " Plugins"},
		Plugins:   codexPlugins,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal codex marketplace.json: %w", err)
	}
	files[".agents/plugins/marketplace.json"] = codexManifest

	files["README.md"] = generateReadme(plugins, cfg)

	return files, nil
}

// escapeMarkdownCell sanitizes user-controlled text for inclusion in a single
// Markdown table cell: collapses line breaks, escapes pipes that would otherwise
// split the row, and caps excessively long values.
func escapeMarkdownCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "|", `\|`)

	const maxRunes = 200
	if utf8.RuneCountInString(s) > maxRunes {
		runes := []rune(s)
		s = string(runes[:maxRunes]) + "…"
	}
	return s
}

func generateReadme(plugins []PluginInfo, cfg GenerateConfig) []byte {
	var b strings.Builder

	b.WriteString("# " + cfg.OrgName + " Plugins\n\n")
	b.WriteString("This repository contains plugin packages managed by [Gram](https://getgram.ai). ")
	b.WriteString("Each plugin bundles MCP servers for distribution via Claude Code, Cursor, and Codex marketplaces.\n\n")
	b.WriteString("## How this repo works\n\n")
	b.WriteString("- **Read-only access.** Collaborators are granted pull permission only. You can clone and inspect the repository, but you cannot push to it.\n")
	b.WriteString("- **Auto-managed by Gram.** Each publish from the Gram dashboard overwrites this repository's contents. Any manual edits, new branches, or local commits will be discarded on the next publish — make changes in Gram instead.\n\n")

	if len(plugins) > 0 {
		b.WriteString("## Plugins\n\n")
		b.WriteString("| Plugin | Description | Servers |\n")
		b.WriteString("|--------|-------------|--------:|\n")
		for _, p := range plugins {
			desc := strings.TrimSpace(p.Description)
			if desc == "" {
				desc = "—"
			}
			fmt.Fprintf(&b, "| %s | %s | %d |\n", escapeMarkdownCell(p.Name), escapeMarkdownCell(desc), len(p.Servers))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Installation\n\n")
	b.WriteString("### Claude Code\n\n")
	b.WriteString("1. Go to your organization's [Claude admin console](https://claude.ai)\n")
	b.WriteString("2. Navigate to **Settings → Plugin Marketplaces**\n")
	b.WriteString("3. Click **Add Marketplace** and paste this repository's URL\n")
	b.WriteString("4. Plugins will be automatically available to members of your organization\n\n")
	b.WriteString("### Cursor\n\n")
	b.WriteString("1. Open your team's [Cursor dashboard](https://cursor.com/dashboard)\n")
	b.WriteString("2. Navigate to **Settings → Plugins → Import**\n")
	b.WriteString("3. Paste this repository's URL to import the marketplace\n")
	b.WriteString("4. Plugins will be available to team members\n\n")
	b.WriteString("### Codex\n\n")
	b.WriteString("Add this repository as a plugin marketplace from the Codex CLI:\n\n")
	b.WriteString("```\ncodex plugin marketplace add <this-repo-url>\n```\n\n")
	b.WriteString("Then list available plugins with `codex /plugins` and install the ones you want.\n")
	b.WriteString("Plugins that need authentication will prompt for any required environment variables on install.\n")

	return []byte(b.String())
}

// GenerateSinglePluginPackage produces files for a single plugin with files at
// the root level (no subdirectory prefix). Used for per-plugin ZIP downloads
// that can be installed directly via `claude --plugin-dir`.
func GenerateSinglePluginPackage(plugin PluginInfo, cfg GenerateConfig, platform string) (map[string][]byte, error) {
	files := make(map[string][]byte)

	switch platform {
	case "claude":
		if err := generateClaudePluginFlat(files, plugin, cfg); err != nil {
			return nil, fmt.Errorf("generate claude plugin: %w", err)
		}
	case "cursor":
		if err := generateCursorPluginFlat(files, plugin, cfg); err != nil {
			return nil, fmt.Errorf("generate cursor plugin: %w", err)
		}
	case "codex":
		if err := generateCodexPluginFlat(files, plugin, cfg); err != nil {
			return nil, fmt.Errorf("generate codex plugin: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}

	return files, nil
}

func generateClaudePluginFlat(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateClaudePluginInDir(files, "", p, cfg)
}

func generateCursorPluginFlat(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateCursorPluginInDir(files, "", p.Slug, p, cfg)
}

func generateCodexPluginFlat(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateCodexPluginInDir(files, "", p.Slug, p, cfg)
}

func generateClaudePlugin(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateClaudePluginInDir(files, p.Slug, p, cfg)
}

func generateCursorPlugin(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateCursorPluginInDir(files, p.Slug+"-cursor", p.Slug+"-cursor", p, cfg)
}

func generateCodexPlugin(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	name := p.Slug + "-codex"
	return generateCodexPluginInDir(files, name, name, p, cfg)
}

// codexAuthPolicy picks ON_INSTALL when the user will be prompted for a
// secret (public server env vars or a Gram API key the published config
// can't bake in) and ON_USE when nothing needs to be collected. A baked-in
// APIKey plus all-public-no-env servers means the plugin is install-silent.
func codexAuthPolicy(p PluginInfo, cfg GenerateConfig) string {
	for _, s := range p.Servers {
		if s.IsPublic {
			if len(s.EnvConfigs) > 0 {
				return "ON_INSTALL"
			}
			continue
		}
		if cfg.APIKey == "" {
			return "ON_INSTALL"
		}
	}
	return "ON_USE"
}

func generateCodexPluginInDir(files map[string][]byte, subdir, name string, p PluginInfo, cfg GenerateConfig) error {
	pluginJSON, err := marshalJSON(codexPluginMeta{
		Name:        name,
		Version:     "0.1.0",
		Description: p.Description,
		MCPServers:  "./.mcp.json",
		Interface: &codexInterface{
			DisplayName:      p.Name,
			ShortDescription: p.Description,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".codex-plugin/plugin.json")] = pluginJSON

	mcpServers := make(map[string]codexMCPServer)
	for _, s := range p.Servers {
		entry := codexMCPServer{URL: s.MCPURL}

		if s.IsPublic {
			// User provides each variable in their shell env; Codex
			// substitutes the value into the named header at runtime.
			if len(s.EnvConfigs) > 0 {
				entry.EnvHTTPHeaders = make(map[string]string, len(s.EnvConfigs))
				for _, ec := range s.EnvConfigs {
					entry.EnvHTTPHeaders[ec.DisplayName] = ec.VariableName
				}
			}
		} else if cfg.APIKey != "" {
			// Private server: bake the Gram-issued key directly into the
			// published config. Repo is private, so this matches the Cursor/Claude pattern.
			entry.HTTPHeaders = map[string]string{"Authorization": "Bearer " + cfg.APIKey}
		} else {
			// Private server, no key available: ask Codex to read GRAM_API_KEY
			// from the user's environment at startup.
			entry.BearerTokenEnvVar = "GRAM_API_KEY"
		}

		mcpServers[s.DisplayName] = entry
	}
	mcpJSON, err := marshalJSON(codexMCPConfig{MCPServers: mcpServers})
	if err != nil {
		return fmt.Errorf("marshal .mcp.json: %w", err)
	}
	files[path.Join(subdir, ".mcp.json")] = mcpJSON

	return nil
}

func generateClaudePluginInDir(files map[string][]byte, subdir string, p PluginInfo, cfg GenerateConfig) error {
	// Collect userConfig entries across all servers that need user-provided values.
	userConfig := make(map[string]userConfigEntry)

	// Determine if any private server needs a Gram API key prompt.
	needsGramKeyPrompt := false
	for _, s := range p.Servers {
		if !s.IsPublic && cfg.APIKey == "" {
			needsGramKeyPrompt = true
		}
		// Public servers may need user-provided env vars.
		for _, ec := range s.EnvConfigs {
			userConfig[ec.VariableName] = userConfigEntry{
				Description: ec.DisplayName,
				Sensitive:   true,
			}
		}
	}

	if needsGramKeyPrompt {
		userConfig["GRAM_API_KEY"] = userConfigEntry{
			Description: "Your Gram API key for authenticating MCP server connections",
			Sensitive:   true,
		}
	}

	var userConfigPtr map[string]userConfigEntry
	if len(userConfig) > 0 {
		userConfigPtr = userConfig
	}

	pluginJSON, err := marshalJSON(claudePluginMeta{
		Name:        p.Slug,
		Description: p.Description,
		Version:     "0.1.0",
		Author:      pluginAuthor{Name: cfg.OrgName, URL: "https://getgram.ai"},
		Homepage:    "https://getgram.ai",
		UserConfig:  userConfigPtr,
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".claude-plugin/plugin.json")] = pluginJSON

	mcpServers := make(map[string]claudeMCPServer)
	for _, s := range p.Servers {
		headers := make(map[string]string)

		if s.IsPublic {
			// Public servers use env config variables for auth.
			for _, ec := range s.EnvConfigs {
				headers[ec.DisplayName] = "${user_config." + ec.VariableName + "}"
			}
		} else if cfg.APIKey != "" {
			// Private server with injected key.
			headers["Authorization"] = "Bearer " + cfg.APIKey
		} else {
			// Private server without key — prompt user.
			headers["Authorization"] = "Bearer ${user_config.GRAM_API_KEY}"
		}

		mcpServers[s.DisplayName] = claudeMCPServer{
			Type:    "http",
			URL:     s.MCPURL,
			Headers: headers,
		}
	}
	mcpJSON, err := marshalJSON(claudeMCPConfig{MCPServers: mcpServers})
	if err != nil {
		return fmt.Errorf("marshal .mcp.json: %w", err)
	}
	files[path.Join(subdir, ".mcp.json")] = mcpJSON

	return nil
}

func generateCursorPluginInDir(files map[string][]byte, subdir, name string, p PluginInfo, cfg GenerateConfig) error {
	displayName := p.Name
	if subdir != "" {
		displayName = p.Name + " (Cursor)"
	}

	pluginJSON, err := marshalJSON(cursorPluginMeta{
		Name:        name,
		DisplayName: displayName,
		Description: p.Description,
		Version:     "0.1.0",
		Author:      pluginAuthor{Name: cfg.OrgName, URL: "https://getgram.ai"},
		Homepage:    "https://getgram.ai",
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".cursor-plugin/plugin.json")] = pluginJSON

	mcpServers := make(map[string]cursorMCPServer)
	for _, s := range p.Servers {
		headers := make(map[string]string)

		if s.IsPublic {
			for _, ec := range s.EnvConfigs {
				headers[ec.DisplayName] = "${env:" + ec.VariableName + "}"
			}
		} else if cfg.APIKey != "" {
			headers["Authorization"] = "Bearer " + cfg.APIKey
		} else {
			headers["Authorization"] = "Bearer ${env:GRAM_API_KEY}"
		}

		mcpServers[s.DisplayName] = cursorMCPServer{
			URL:     s.MCPURL,
			Headers: headers,
		}
	}
	mcpJSON, err := marshalJSON(cursorMCPConfig{MCPServers: mcpServers})
	if err != nil {
		return fmt.Errorf("marshal mcp.json: %w", err)
	}
	files[path.Join(subdir, "mcp.json")] = mcpJSON

	return nil
}

// --- JSON types ---

type marketplaceManifest struct {
	Name    string             `json:"name"`
	Owner   marketplaceOwner   `json:"owner"`
	Plugins []marketplaceEntry `json:"plugins"`
}

type marketplaceOwner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
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
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Version     string                     `json:"version"`
	Author      pluginAuthor               `json:"author"`
	Homepage    string                     `json:"homepage"`
	UserConfig  map[string]userConfigEntry `json:"userConfig,omitempty"`
}

type userConfigEntry struct {
	Description string `json:"description"`
	Sensitive   bool   `json:"sensitive"`
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

// Codex types — schema verified against openai/codex @
// f802f0a3911655ac0e2876fceedf8ad833431df3 (2026-04-24):
//   codex-rs/core-plugins/src/manifest.rs  — plugin manifest (rename_all = "camelCase")
//   codex-rs/core-plugins/src/loader.rs    — .mcp.json wrapper format
//   codex-rs/config/src/mcp_types.rs       — server transport (untagged; url selects streamable_http)
// Note: MCP server entry fields are snake_case; plugin manifest fields are camelCase.
// Refresh this pin when Codex's plugin support moves out of preview.

type codexPluginMeta struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description,omitempty"`
	MCPServers  string          `json:"mcpServers"`
	Interface   *codexInterface `json:"interface,omitempty"`
}

type codexInterface struct {
	DisplayName      string `json:"displayName"`
	ShortDescription string `json:"shortDescription,omitempty"`
}

type codexMCPConfig struct {
	MCPServers map[string]codexMCPServer `json:"mcpServers"`
}

type codexMCPServer struct {
	URL               string            `json:"url"`
	BearerTokenEnvVar string            `json:"bearer_token_env_var,omitempty"`
	HTTPHeaders       map[string]string `json:"http_headers,omitempty"`
	EnvHTTPHeaders    map[string]string `json:"env_http_headers,omitempty"`
}

type codexMarketplaceManifest struct {
	Name      string                  `json:"name"`
	Interface codexInterface          `json:"interface"`
	Plugins   []codexMarketplaceEntry `json:"plugins"`
}

type codexMarketplaceEntry struct {
	Name   string                 `json:"name"`
	Source codexMarketplaceSource `json:"source"`
	Policy codexMarketplacePolicy `json:"policy"`
}

type codexMarketplaceSource struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

type codexMarketplacePolicy struct {
	Installation   string `json:"installation"`
	Authentication string `json:"authentication"`
}

func marshalJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}
	return b, nil
}
