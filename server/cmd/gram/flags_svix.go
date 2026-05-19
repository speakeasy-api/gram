package gram

import "github.com/urfave/cli/v2"

var svixFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "svix-api-key",
		Usage:   "API key for Svix used to send webhook events",
		EnvVars: []string{"GRAM_SVIX_API_KEY"},
	},
}
