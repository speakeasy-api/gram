package plugins

import (
	"fmt"
	"strings"
)

// PublicMarketplaceName is the marketplace identifier users type when
// installing a first-party plugin (`speakeasy@speakeasy`). It is fixed: the
// public repository is a single global artifact, not an org-derived one, and it
// is the umbrella every first-party plugin is published under so registering it
// once carries future plugins too.
const PublicMarketplaceName = "speakeasy"

// PublicMarketplaceRepoURL is the canonical clone URL every client registers.
const PublicMarketplaceRepoURL = "https://github.com/speakeasy-api/marketplace"

const publicMarketplaceDisplayName = "Speakeasy"

const publicMarketplaceOwnerEmail = "support@speakeasy.com"

type platformMCPMarketplaceMode string

const (
	platformMCPMarketplacePublic platformMCPMarketplaceMode = "public"
	platformMCPMarketplaceLocal  platformMCPMarketplaceMode = "local"
)

// PublicPlatformMCPFiles renders the complete file tree of the public
// speakeasy-api/marketplace repository: the five per-client Platform MCP
// package roots plus the Claude, Cursor, and Codex marketplace manifests that
// advertise them. Unlike the per-organization marketplaces, this tree carries
// no organization identity and no credentials — the package authenticates
// through Platform MCP's own OAuth flow — so a single global render serves
// every organization.
//
// serverURL is the Gram deployment the package points at (e.g.
// https://app.getgram.ai). version is the monotonic publish counter mixed into
// every plugin.json so clients treat a re-render as newer and refresh; CI
// supplies the workflow run number. An empty version pins the deterministic
// default used by tests.
func PublicPlatformMCPFiles(serverURL string, version string) (map[string][]byte, error) {
	if strings.TrimSpace(serverURL) == "" {
		return nil, fmt.Errorf("server URL is required")
	}
	// This tree is world-readable and its .mcp.json is what every installed
	// client dials, so an http:// deployment URL would publish a downgrade to
	// everyone at once. The shared generator still allows http for local
	// development; the public render does not.
	// Scheme comparison is case-insensitive: url.Parse normalizes HTTPS:// to
	// https://, so a case-sensitive prefix test would reject a URL the shared
	// generator goes on to accept.
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(serverURL)), "https://") {
		return nil, fmt.Errorf("public marketplace server URL must be https, got %q", serverURL)
	}

	return platformMCPMarketplaceFiles(serverURL, PublicMarketplaceRepoURL, version, platformMCPMarketplacePublic)
}

// LocalPlatformMCPFiles renders the same first-party marketplace against the
// local server. Unlike the public artifact, local development may deliberately
// use HTTP, so callers must keep this output on the local-only Git server.
func LocalPlatformMCPFiles(serverURL, marketplaceRepoURL, version string) (map[string][]byte, error) {
	if strings.TrimSpace(serverURL) == "" {
		return nil, fmt.Errorf("server URL is required")
	}
	if strings.TrimSpace(marketplaceRepoURL) == "" {
		return nil, fmt.Errorf("marketplace repository URL is required")
	}
	if strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("version is required")
	}
	return platformMCPMarketplaceFiles(serverURL, marketplaceRepoURL, version, platformMCPMarketplaceLocal)
}

func platformMCPMarketplaceFiles(serverURL, marketplaceRepoURL, version string, mode platformMCPMarketplaceMode) (map[string][]byte, error) {
	cfg := GenerateConfig{
		OrgName:          publicMarketplaceDisplayName,
		OrgEmail:         publicMarketplaceOwnerEmail,
		OrgID:            "",
		ServerURL:        serverURL,
		APIKey:           "",
		HooksAPIKey:      "",
		ProjectSlug:      "",
		IsDefaultProject: true,
		Version:          version,
		MarketplaceName:  PublicMarketplaceName,
		BrowserLogin:     false,
		HooksOrgName:     "",
		InstallFailOpen:  false,
	}

	files := make(map[string][]byte)
	if err := generatePlatformMCPFilesInto(files, cfg); err != nil {
		return nil, fmt.Errorf("generate Platform MCP packages: %w", err)
	}
	if err := generatePublicMarketplaceManifests(files, cfg); err != nil {
		return nil, err
	}
	files["README.md"] = platformMCPMarketplaceReadme(marketplaceRepoURL, mode)
	files["LICENSE"] = []byte(publicMarketplaceLicense)
	return files, nil
}

// publicMarketplaceLicense is MIT: the tree is plugin manifests and skill
// markdown that people clone and copy into their own agent configuration, so
// the licence has to survive being pasted around in fragments. The year is
// fixed rather than taken from the clock — the render must stay byte-stable
// across publishes, and a moving year would rewrite the file every January.
const publicMarketplaceLicense = `MIT License

Copyright (c) 2026 Speakeasy Development, Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`

// generatePublicMarketplaceManifests emits the three client manifests for the
// public repository. They mirror generateSharedFilesWithPlatformClients but
// list only the first-party package: there are no observability hooks and no
// customer plugins in this tree.
func generatePublicMarketplaceManifests(files map[string][]byte, cfg GenerateConfig) error {
	owner := marketplaceOwner{Name: cfg.OrgName, Email: cfg.OrgEmail}

	claudeManifest, err := marshalJSON(marketplaceManifest{
		Name:     PublicMarketplaceName,
		Owner:    owner,
		Metadata: nil,
		Plugins: []marketplaceEntry{{
			Name:        platformMCPPluginName,
			DisplayName: platformMCPDisplayName,
			Source:      "./" + platformMCPPluginRoot,
			Description: platformMCPDescription,
		}},
	})
	if err != nil {
		return fmt.Errorf("marshal public claude marketplace.json: %w", err)
	}
	files[".claude-plugin/marketplace.json"] = claudeManifest

	cursorManifest, err := marshalJSON(marketplaceManifest{
		Name:     PublicMarketplaceName,
		Owner:    owner,
		Metadata: &marketplaceMetadata{PluginRoot: cursorPluginRoot},
		Plugins: []marketplaceEntry{{
			Name:        platformMCPCursorPluginName,
			DisplayName: "", // Cursor carries the display name in its own plugin.json.
			Source:      platformMCPCursorPluginName,
			Description: platformMCPDescription,
		}},
	})
	if err != nil {
		return fmt.Errorf("marshal public cursor marketplace.json: %w", err)
	}
	files[".cursor-plugin/marketplace.json"] = cursorManifest

	codexManifest, err := marshalJSON(codexMarketplaceManifest{
		Name:      PublicMarketplaceName,
		Interface: codexInterface{DisplayName: platformMCPDisplayName, ShortDescription: platformMCPDescription},
		Plugins: []codexMarketplaceEntry{{
			Name: platformMCPCodexPluginName,
			Source: codexMarketplaceSource{
				Source: "local",
				Path:   "./" + platformMCPCodexPluginRoot,
			},
			Policy: codexMarketplacePolicy{
				Installation:   "AVAILABLE",
				Authentication: "ON_USE",
			},
		}},
	})
	if err != nil {
		return fmt.Errorf("marshal public codex marketplace.json: %w", err)
	}
	files[".agents/plugins/marketplace.json"] = codexManifest

	return nil
}

func platformMCPMarketplaceReadme(marketplaceRepoURL string, mode platformMCPMarketplaceMode) []byte {
	var b strings.Builder
	b.WriteString("# " + platformMCPDisplayName + "\n\n")
	b.WriteString(platformMCPDescription + "\n\n")
	if mode == platformMCPMarketplacePublic {
		b.WriteString("This repository is Speakeasy's public plugin marketplace. Registering it once makes every ")
		b.WriteString("plugin Speakeasy publishes available in your agent, including ones added later. It contains no ")
		b.WriteString("credentials: plugins authenticate against your organization through OAuth on first use, and ")
		b.WriteString("installing one grants no access on its own.\n\n")
		b.WriteString("It currently ships [Platform MCP](https://www.speakeasy.com/product/gram).\n\n")
		b.WriteString("> **Auto-generated.** Every file here is rendered from the Speakeasy control plane and replaced on each release. Manual edits are discarded.\n\n")
	} else {
		b.WriteString("This repository is generated by the local Gram server for development. It contains no credentials; ")
		b.WriteString("plugins authenticate through local Platform MCP OAuth on first use, and installing one grants no access on its own.\n\n")
		b.WriteString("> **Local development only.** Restarting Gram regenerates these files. Manual edits are discarded.\n\n")
	}

	b.WriteString("## Claude Code\n\n")
	b.WriteString("```\n")
	b.WriteString("/plugin marketplace add " + marketplaceRepoURL + "\n")
	b.WriteString("/plugin install " + platformMCPPluginName + "@" + PublicMarketplaceName + "\n")
	b.WriteString("```\n\n")

	b.WriteString("## Codex\n\n")
	b.WriteString("```\n")
	b.WriteString("codex plugin marketplace add " + marketplaceRepoURL + "\n")
	b.WriteString("```\n\n")
	b.WriteString("Then open `/plugins` and install `" + platformMCPCodexPluginName + "`.\n\n")

	b.WriteString("## Cursor\n\n")
	b.WriteString("In the Cursor dashboard for a team you administer, go to Settings → Plugins → Import and paste:\n\n")
	b.WriteString("```\n")
	b.WriteString(marketplaceRepoURL + "\n")
	b.WriteString("```\n\n")

	b.WriteString("## Other agents\n\n")
	b.WriteString("`opencode-plugins/` and `agent-plugins/` carry the same server in the OpenCode and portable ")
	if mode == platformMCPMarketplacePublic {
		b.WriteString("Agent Plugins formats. Clone this repository or download an archive from GitHub and point your ")
	} else {
		b.WriteString("Agent Plugins formats. Clone this repository and point your ")
	}
	b.WriteString("agent at the directory for your format.\n")
	return []byte(b.String())
}
