package gram

import "github.com/urfave/cli/v2"

var gitProxyFlags = []cli.Flag{
	&cli.Int64Flag{
		Name:     "git-proxy-github-app-id",
		Usage:    "GitHub App ID for the git smart-HTTP reverse proxy.",
		EnvVars:  []string{"GRAM_GIT_PROXY_GITHUB_APP_ID"},
		Required: false,
	},
	&cli.StringFlag{
		Name:     "git-proxy-github-private-key",
		Usage:    "PEM-encoded private key for the git proxy GitHub App.",
		EnvVars:  []string{"GRAM_GIT_PROXY_GITHUB_PRIVATE_KEY"},
		Required: false,
	},
	&cli.Int64Flag{
		Name:     "git-proxy-github-installation-id",
		Usage:    "GitHub App installation ID whose repositories the proxy can serve.",
		EnvVars:  []string{"GRAM_GIT_PROXY_GITHUB_INSTALLATION_ID"},
		Required: false,
	},
	&cli.BoolFlag{
		Name:     "git-proxy-read-only",
		Usage:    "Disable git push (git-receive-pack) through the proxy.",
		EnvVars:  []string{"GRAM_GIT_PROXY_READ_ONLY"},
		Required: false,
		Value:    true,
	},
}
