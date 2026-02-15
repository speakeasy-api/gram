package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"text/template"

	"github.com/speakeasy-api/gram/cli/internal/api"
	//	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/cli/internal/skills"
	//	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/urfave/cli/v2"
)

type Params struct {
	APIKey secret.Secret
}

// PluginMetadata represents metadata for a Claude plugin
type PluginMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

func newSkillsCommand() *cli.Command {
	return &cli.Command{
		Name:        "skills",
		Usage:       "lol",
		Description: "foo",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "server-name",
				Usage: "lipsum",
			},
			&cli.StringSliceFlag{
				Name:  "tool-name",
				Usage: "stuff",
			},
			&cli.StringFlag{
				Name:  "api-key",
				Usage: "foo",
			},
			&cli.StringFlag{
				Name:  "project-slug",
				Usage: "foo",
			},
			&cli.StringFlag{
				Name:    "url",
				Usage:   "Gram server URL",
				EnvVars: []string{"GRAM_SITE_URL"},
				Value:   "https://localhost:8080",
			},
		},
		Action: doSkills,
	}
}

func doSkills(c *cli.Context) error {
	// Print all flag values
	fmt.Printf("interactive: %v\n", c.Bool("interactive"))
	fmt.Printf("server-name: %v\n", c.Bool("server-name"))
	fmt.Printf("tool-name: %v\n", c.StringSlice("tool-name"))
	fmt.Printf("api-key: %v\n", c.String("api-key"))

	// Construct secret.Secret from string
	apiKey := secret.Secret(c.String("api-key"))
	projectSlug := c.String("project-slug")

	//prof := profile.FromContext(c.Context)
	parsedURL, err := url.Parse(c.String("url"))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	ctx := c.Context
	tsc := api.NewToolsetsClient(&api.ToolsetsClientOptions{
		Scheme: parsedURL.Scheme,
		Host:   parsedURL.Host,
	})

	result, err := tsc.InferSkillsFromToolset(ctx, apiKey, projectSlug)
	if err != nil {
		return fmt.Errorf("could not infer skills from toolset: %w", err)
	}

	fmt.Printf("Tools count: %d\n", len(result.Tools))
	fmt.Printf("Skills count: %d\n", len(result.Skills))

	// Create temporary directory for the plugin
	tmpDir, err := os.MkdirTemp("", "claude-plugin-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	fmt.Printf("Created temporary directory: %s\n", tmpDir)

	// Materialize plugin filesystem structure
	metadata := PluginMetadata{
		Name:        projectSlug,
		Description: "Auto-generated skills from toolsets",
		Version:     "1.0.0",
	}
	if err := materializePluginFS(tmpDir, metadata); err != nil {
		return fmt.Errorf("failed to create plugin filesystem: %w", err)
	}

	// Render templates into the filesystem
	skillsDir := filepath.Join(tmpDir, "skills")
	for i, skill := range result.Skills {
		var toolName string
		if i < len(result.Tools) {
			toolName = result.Tools[i].Name
		} else {
			toolName = fmt.Sprintf("tool_%d", i)
		}

		templateInfo := skills.SkillsTemplateInfo{
			Name:         toolName,
			Description:  skill,
			Instructions: fmt.Sprintf("Use the %s tool to %s", toolName, skill),
			Examples:     []string{fmt.Sprintf("Can you help me with %s?", toolName)},
		}

		rendered, err := renderTemplate(templateInfo)
		if err != nil {
			return fmt.Errorf("failed to render template for %s: %w", toolName, err)
		}

		// Write skill to the skills directory
		skillPath := filepath.Join(skillsDir, toolName+".md")
		if err := os.WriteFile(skillPath, []byte(rendered), 0644); err != nil {
			return fmt.Errorf("failed to write skill file for %s: %w", toolName, err)
		}

		fmt.Printf("Created skill: %s\n", toolName)
	}

	// Return the path to the temporary directory
	fmt.Printf("\n✓ Plugin created successfully!\n")
	fmt.Printf("\nTo use this plugin, copy the directory to your desired location.\n")
	fmt.Printf("%s", tmpDir)

	return nil
}

// renderTemplate renders a Claude skill file using the provided template info
func renderTemplate(info skills.SkillsTemplateInfo) (string, error) {
	const skillTemplate = `# {{ .Name }}

{{ .Description }}

## Instructions

{{ .Instructions }}

## Examples

{{- range .Examples }}
- {{ . }}
{{- end }}
`

	tmpl, err := template.New("skill").Parse(skillTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, info); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// materializePluginFS creates the .claude-plugin directory and plugin.json file
func materializePluginFS(pluginDir string, metadata PluginMetadata) error {
	// Create the .claude-plugin directory structure
	for _, dir := range []string{
		filepath.Join(pluginDir, ".claude-plugin"),
		filepath.Join(pluginDir, "commands"),
		filepath.Join(pluginDir, "agents"),
		filepath.Join(pluginDir, "skills"),
		filepath.Join(pluginDir, "hooks"),
	} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("failed to create claude directory structure: %w", err)
		}
	}

	// Create MCP configuration file with toolset information
	mcpConfig := generateMCPConfig(metadata)
	mcpData, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}
	mcpPath := filepath.Join(pluginDir, ".mcp.json")
	if err := os.WriteFile(mcpPath, mcpData, 0644); err != nil {
		return fmt.Errorf("failed to write .mcp.json: %w", err)
	}

	// Create LSP configuration file with defaults
	lspConfig := generateLSPConfig()
	lspData, err := json.MarshalIndent(lspConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal LSP config: %w", err)
	}
	lspPath := filepath.Join(pluginDir, ".lsp.json")
	if err := os.WriteFile(lspPath, lspData, 0640); err != nil {
		return fmt.Errorf("failed to write .lsp.json: %w", err)
	}

	// Marshal the plugin metadata to JSON with indentation
	jsonData, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plugin metadata: %w", err)
	}

	// Write the plugin.json file
	pluginJsonPath := filepath.Join(pluginDir, ".claude-plugin", "plugin.json")
	if err := os.WriteFile(pluginJsonPath, jsonData, 0640); err != nil {
		return fmt.Errorf("failed to write plugin.json: %w", err)
	}

	return nil
}

// generateMCPConfig creates an MCP configuration with the toolset information
func generateMCPConfig(metadata PluginMetadata) map[string]interface{} {
	// Create a server entry for this toolset
	serverName := metadata.Name

	return map[string]interface{}{
		"mcpServers": map[string]interface{}{
			serverName: map[string]interface{}{
				"command": "npx",
				"args": []string{
					"-y",
					"@speakeasy-api/mcp-server-gram",
					"--project",
					metadata.Name,
				},
				"description": metadata.Description,
			},
		},
	}
}

// generateLSPConfig creates an LSP configuration
// Currently returns an empty configuration as LSP servers are language-specific
func generateLSPConfig() map[string]interface{} {
	return map[string]interface{}{
		"languageServers": map[string]interface{}{},
	}
}
