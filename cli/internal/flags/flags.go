// Package flags defines common flags to keep CLI commands consistent.
package flags

import "github.com/urfave/cli/v2"

func APIKey() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "api-key",
		Usage:   "Your Speakeasy API key (must be scoped as a 'Provider')",
		EnvVars: EnvVars("API_KEY"),
	}
}

func APIEndpoint() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "api-url",
		Usage:   "The base URL to use for API calls.",
		EnvVars: EnvVars("API_URL"),
		Value:   "https://app.getgram.ai",
		Hidden:  true,
	}
}

func Org() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "org",
		Usage:   "The target organization (slug)",
		EnvVars: EnvVars("ORG"),
	}
}

func Project() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "project",
		Usage:   "The target project (slug)",
		EnvVars: EnvVars("PROJECT"),
	}
}

func JSON() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  "json",
		Usage: "Output deployment status as JSON",
	}
}
