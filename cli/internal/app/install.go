package app

import (
	"fmt"
	"net/url"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/mcp"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/urfave/cli/v2"
)

var installFlags = []cli.Flag{
	flags.APIKey(),
	flags.Project(),
	&cli.StringFlag{
		Name:  "toolset",
		Usage: "The slug of the Gram toolset to install (e.g., speakeasy-admin). Will automatically look up MCP configuration.",
	},
	&cli.StringFlag{
		Name:  "mcp-url",
		Usage: "The MCP server URL (e.g., https://mcp.getgram.ai/org/project/environment). Use this for manual configuration.",
	},
	&cli.StringFlag{
		Name:  "name",
		Usage: "The name to use for this MCP server in Cursor (defaults to toolset name or derived from URL)",
	},
	&cli.StringFlag{
		Name:  "header",
		Usage: "The HTTP header name for the API key (defaults to Authorization)",
		Value: "Authorization",
	},
	&cli.StringFlag{
		Name:  "env-var",
		Usage: "Environment variable name to use for API key substitution (e.g., MCP_API_KEY). If provided, uses ${VAR} syntax instead of hardcoding the key",
	},
}

func newInstallCommand() *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "Install Gram toolsets as MCP servers in various clients",
		Subcommands: []*cli.Command{
			newInstallClaudeCodeCommand(),
			newInstallClaudeDesktopCommand(),
			newInstallCursorCommand(),
			newInstallGeminiCLICommand(),
		},
	}
}

func resolveToolsetInfo(c *cli.Context) (*mcp.ToolsetInfo, error) {
	ctx := c.Context
	logger := logging.PullLogger(ctx)
	prof := profile.FromContext(ctx)

	toolsetSlug := c.String("toolset")
	mcpURL := c.String("mcp-url")

	// Validate that either toolset or mcp-url is provided
	if toolsetSlug == "" && mcpURL == "" {
		return nil, fmt.Errorf("either --toolset or --mcp-url must be provided")
	}
	if toolsetSlug != "" && mcpURL != "" {
		return nil, fmt.Errorf("cannot provide both --toolset and --mcp-url")
	}

	// Get API URL if needed
	var apiURL *url.URL
	if toolsetSlug != "" {
		var err error
		apiURL, err = workflow.ResolveURL(c, prof)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve API URL: %w", err)
		}
	}

	projectSlug := workflow.ResolveProject(c, prof)
	apiKey := workflow.ResolveKey(c, prof)

	// Resolve toolset information using shared logic
	info, err := mcp.ResolveToolsetInfo(ctx, &mcp.ResolverOptions{
		ProjectSlug:     projectSlug,
		ToolsetSlug:     toolsetSlug,
		MCPURL:          mcpURL,
		ServerName:      c.String("name"),
		APIKey:          apiKey,
		HeaderName:      c.String("header"),
		EnvVar:          c.String("env-var"),
		APIURL:          apiURL,
		Logger:          logger,
		IsHeaderNameSet: c.IsSet("header"),
		IsEnvVarSet:     c.IsSet("env-var"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve toolset info: %w", err)
	}
	return info, nil
}
