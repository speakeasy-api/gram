package gram

import "github.com/urfave/cli/v2"

var pluginsFlags = []cli.Flag{
	&cli.Int64Flag{
		Name:     "plugins-github-app-id",
		Usage:    "GitHub App ID for plugin publishing.",
		EnvVars:  []string{"GRAM_PLUGINS_GITHUB_APP_ID"},
		Required: false,
	},
	&cli.StringFlag{
		Name:     "plugins-github-private-key",
		Usage:    "PEM-encoded private key for the GitHub App.",
		EnvVars:  []string{"GRAM_PLUGINS_GITHUB_PRIVATE_KEY"},
		Required: false,
	},
	&cli.StringFlag{
		Name:     "plugins-github-org",
		Usage:    "GitHub organization to create plugin repos in.",
		EnvVars:  []string{"GRAM_PLUGINS_GITHUB_ORG"},
		Required: false,
	},
	&cli.Int64Flag{
		Name:     "plugins-github-installation-id",
		Usage:    "GitHub App installation ID on the target org.",
		EnvVars:  []string{"GRAM_PLUGINS_GITHUB_INSTALLATION_ID"},
		Required: false,
	},
}
