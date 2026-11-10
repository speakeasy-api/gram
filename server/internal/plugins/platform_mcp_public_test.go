package plugins

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicPlatformMCPFiles(t *testing.T) {
	t.Parallel()

	files, err := PublicPlatformMCPFiles("https://app.getgram.ai", "17")
	require.NoError(t, err)

	for _, expected := range []string{
		".claude-plugin/marketplace.json",
		".cursor-plugin/marketplace.json",
		".agents/plugins/marketplace.json",
		"README.md",
		"LICENSE",
		"speakeasy/.mcp.json",
		"speakeasy/.claude-plugin/plugin.json",
		"speakeasy/skills/manage-mcp-access/SKILL.md",
		"speakeasy/skills/add-existing-mcp-servers/SKILL.md",
		"agent-plugins/speakeasy/skills/add-existing-mcp-servers/SKILL.md",
		"agent-plugins/speakeasy/skills/manage-mcp-access/SKILL.md",
		"speakeasy/skills/migrate-mcp-between-projects/SKILL.md",
		"agent-plugins/speakeasy/skills/migrate-mcp-between-projects/SKILL.md",
		"speakeasy/skills/expose-tools-on-mcp/SKILL.md",
		"agent-plugins/speakeasy/skills/expose-tools-on-mcp/SKILL.md",
		"cursor-plugins/speakeasy-cursor/.cursor-plugin/plugin.json",
		"speakeasy-codex/.codex-plugin/plugin.json",
	} {
		require.Contains(t, files, expected)
	}

	var claude marketplaceManifest
	require.NoError(t, json.Unmarshal(files[".claude-plugin/marketplace.json"], &claude))
	require.Equal(t, PublicMarketplaceName, claude.Name)
	require.Len(t, claude.Plugins, 1, "the public marketplace advertises only the first-party package")
	require.Equal(t, platformMCPPluginName, claude.Plugins[0].Name)

	var codex codexMarketplaceManifest
	require.NoError(t, json.Unmarshal(files[".agents/plugins/marketplace.json"], &codex))
	require.Equal(t, PublicMarketplaceName, codex.Name)
	require.Len(t, codex.Plugins, 1)

	claudeSkill := files["speakeasy/skills/manage-mcp-access/SKILL.md"]
	require.Equal(t, claudeSkill, files["agent-plugins/speakeasy/skills/manage-mcp-access/SKILL.md"])
	for _, tool := range []string{"get_mcp_access", "list_access_roles", "list_access_members", "create_mcp_access_role", "update_mcp_access_role", "assign_mcp_access_role"} {
		require.Contains(t, string(claudeSkill), tool)
	}
	for _, guardrail := range []string{
		"`list_projects` with `limit: 100`",
		"only `limit` (capped at 100), not cursor or search",
		"If `truncated: true`, stop and hand off project selection to the AICP dashboard",
		"never combine `query` and `cursor`",
		"narrow the query or switch to project-scoped unfiltered pagination",
		"`assignment_eligible: true`",
		"entire role contains only exact `mcp:connect` selectors for the selected project and MCP",
		"non-MCP, broad, other-project, other-MCP, or unknown grants are not eligible",
		"Assigning a role includes all of its grants, not just the user's intended subset",
		"Require explicit confirmation of ALL selected MCP grants",
		"Present EVERY `assignment_rules` entry in product language for final confirmation",
		"`all_tools: true` means unrestricted tool access, including future tools",
		"Do not rely on `allowed_known_tools` to establish the full scope",
		"Missing or incomplete `assignment_rules` is a stop condition",
		"distinguishing unrestricted future-tool access from the known catalogue list",
		"Do not mutate or narrow a shared existing role",
		"create a dedicated narrow role",
		"Stop if incomplete tool catalogue or unevaluated evidence prevents confirmation of exact scope",
		"`tools_truncated: true`",
		"`unevaluated_grants: true`",
		"selected `mcp_id`",
		"role's `version` from the immediately preceding `get_mcp_access` response as `expected_role_version`",
		"member's `version` from the immediately preceding `list_access_members` response as `expected_version`",
		"`snapshot_scope: assignment_commit`",
		"original identity query without a role filter",
		"do not filter by the newly assigned role",
		"Never bypass suppression by changing filters or infer membership from counts",
		"committed local desired state",
		"pending or unverified provider synchronization",
		"Never claim provider synchronization has converged or that the member has effective provider access based on local reads",
	} {
		require.Contains(t, string(claudeSkill), guardrail)
	}
	for _, forbidden := range []string{"API key", "client secret", "password", "Gram", "WorkOS", "with the selected role and the user's exact identity query", "verify the member's effective access"} {
		require.NotContains(t, string(claudeSkill), forbidden)
	}

	require.Contains(t, string(files["LICENSE"]), "MIT License")
	require.Contains(t, string(files["LICENSE"]), "Speakeasy Development, Inc.")

	var meta claudePluginMeta
	require.NoError(t, json.Unmarshal(files["speakeasy/.claude-plugin/plugin.json"], &meta))
	require.Equal(t, "0."+platformMCPGeneratorVersion+".17", meta.Version, "the CI counter must reach the manifest so installs refresh")

	// The Claude plugin directory warns when the listing has no icon. It reads
	// .claude-plugin/icon.png: a square PNG, 512 to 2048 px, under 2 MB.
	icon := files["speakeasy/.claude-plugin/icon.png"]
	require.Less(t, len(icon), 2<<20)
	iconConfig, iconFormat, err := image.DecodeConfig(bytes.NewReader(icon))
	require.NoError(t, err)
	require.Equal(t, "png", iconFormat)
	require.Equal(t, iconConfig.Width, iconConfig.Height)
	require.GreaterOrEqual(t, iconConfig.Width, 512)
	require.LessOrEqual(t, iconConfig.Width, 2048)
}

// The public repository is world-readable, so nothing org-derived or secret may
// reach it. Rendering with credentials set on the config must not change the
// output, and no rendered byte may carry a key or an organization identity.
func TestPublicPlatformMCPFilesCarriesNoSecrets(t *testing.T) {
	t.Parallel()

	files, err := PublicPlatformMCPFiles("https://app.getgram.ai", "17")
	require.NoError(t, err)

	for path, content := range files {
		body := string(content)
		for _, forbidden := range []string{"gram_", "GRAM_API_KEY", "SPEAKEASY_AI_API_KEY", "Gram-Project", "Speakeasy-AI-Project", "Authorization"} {
			require.NotContains(t, body, forbidden, "%s must not carry credentials", path)
		}
	}
}

// The published .mcp.json is what every installed client dials, so a plaintext
// deployment URL would reach every install at once.
func TestPublicPlatformMCPFilesRejectsInsecureServerURL(t *testing.T) {
	t.Parallel()

	_, err := PublicPlatformMCPFiles("http://app.getgram.ai", "17")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be https")

	// url.Parse normalizes the scheme, so an uppercase one is still secure.
	files, err := PublicPlatformMCPFiles("HTTPS://app.getgram.ai", "17")
	require.NoError(t, err)
	require.Contains(t, string(files["speakeasy/.mcp.json"]), "app.getgram.ai/platform-mcp")
}

func TestPublicPlatformMCPFilesRequiresServerURL(t *testing.T) {
	t.Parallel()

	_, err := PublicPlatformMCPFiles("  ", "17")
	require.Error(t, err)

	// https, so it clears the scheme guard above and reaches the shape
	// validation the shared generator applies.
	_, err = PublicPlatformMCPFiles("https:///missing-host", "17")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid Platform MCP server URL")
}

func TestLocalPlatformMCPFilesTargetsLocalServer(t *testing.T) {
	t.Parallel()

	const marketplaceRepoURL = "http://localhost:8080/marketplace/local-platform-mcp-marketplace-000000000000.git"
	files, err := LocalPlatformMCPFiles("http://localhost:8080", marketplaceRepoURL, "42")
	require.NoError(t, err)
	require.Contains(t, string(files["speakeasy/.mcp.json"]), "http://localhost:8080/platform-mcp")
	readme := string(files["README.md"])
	// Claude Code installs with the quick commands first, then settings turn
	// auto-update on.
	require.Contains(t, readme, "/plugin marketplace add "+marketplaceRepoURL)
	require.Contains(t, readme, "/plugin install "+platformMCPPluginName+"@"+PublicMarketplaceName)
	require.Contains(t, readme, `"url": "`+marketplaceRepoURL+`"`)
	require.Contains(t, readme, `"`+PublicMarketplaceName+`": {`+"\n      \"autoUpdate\": true,")
	require.Contains(t, readme, `"`+platformMCPPluginName+"@"+PublicMarketplaceName+`": true`)
	require.Less(t, strings.Index(readme, "/plugin marketplace add"), strings.Index(readme, "/plugin install"))
	require.Less(t, strings.Index(readme, "/plugin install"), strings.Index(readme, `"autoUpdate": true`))
	require.Contains(t, readme, "choose **Enable auto-update**")
	require.Contains(t, readme, "merge this into managed settings")
	require.Contains(t, readme, "generated by the local Speakeasy server")
	require.NotContains(t, readme, "public plugin marketplace")

	var meta claudePluginMeta
	require.NoError(t, json.Unmarshal(files["speakeasy/.claude-plugin/plugin.json"], &meta))
	require.Equal(t, "0."+platformMCPGeneratorVersion+".42", meta.Version)

	var claude marketplaceManifest
	require.NoError(t, json.Unmarshal(files[".claude-plugin/marketplace.json"], &claude))
	require.Equal(t, PublicMarketplaceName, claude.Name)
	require.Len(t, claude.Plugins, 1)
	require.Equal(t, platformMCPPluginName, claude.Plugins[0].Name)
}

// Codex does not open the sign-in page for a plugin's MCP server on its own,
// so the model sees the skills but no Platform tools and no reason why. The
// sign-in skill gives it the per-client sign-in steps, and the workflows that
// start with list_projects point at it instead of a vague "repair OAuth".
func TestPublicPlatformMCPFilesShipSignInWorkflow(t *testing.T) {
	t.Parallel()

	files, err := PublicPlatformMCPFiles("https://app.getgram.ai", "17")
	require.NoError(t, err)

	const path = "skills/platform-sign-in/SKILL.md"
	content := files["speakeasy/"+path]
	require.NotEmpty(t, content)
	require.Equal(t, content, files["agent-plugins/speakeasy/"+path])
	require.Equal(t, content, files["speakeasy-codex/"+path], "the Codex package is the one that needs the sign-in steps")
	workflow := string(content)
	for _, required := range []string{
		"name: platform-sign-in",
		"codex mcp login platform",
		"/Applications/Codex.app/Contents/Resources/codex mcp login platform",
		"codex mcp list",
		"Never run it without asking",
		"`/mcp`",
		"call `list_projects` again",
		"start a new conversation or restart the client",
		"is not a sign-in problem",
	} {
		require.Contains(t, workflow, required)
	}
	for _, forbidden := range []string{"Gram", "paste the token", "bearer_token"} {
		require.NotContains(t, workflow, forbidden)
	}

	for _, skill := range []string{
		"add-mcp-from-catalog",
		"add-mcp-from-remote-url",
		"expose-tools-on-mcp",
		"migrate-mcp-between-projects",
		"create-guardrail-from-description",
		"summarize-critical-watchdog-findings",
		"share-coding-session",
	} {
		body := string(files["speakeasy/skills/"+skill+"/SKILL.md"])
		require.Contains(t, body, "follow the `platform-sign-in` workflow", skill)
		require.NotContains(t, body, "repair AICP OAuth", skill)
	}

	readme := string(files["README.md"])
	codexAt := strings.Index(readme, "## Codex")
	cursorAt := strings.Index(readme, "## Cursor")
	require.Positive(t, codexAt)
	require.Greater(t, cursorAt, codexAt)
	require.Contains(t, readme[codexAt:cursorAt], "codex mcp login "+platformMCPServerName)
}
