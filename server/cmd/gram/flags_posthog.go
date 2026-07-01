package gram

import "github.com/urfave/cli/v2"

var posthogFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "posthog-endpoint",
		Usage:    "The endpoint to proxy product metrics to",
		EnvVars:  []string{"POSTHOG_ENDPOINT"},
		Required: false,
	},
	&cli.StringFlag{
		Name:     "posthog-api-key",
		Usage:    "The posthog public API key",
		EnvVars:  []string{"POSTHOG_API_KEY"},
		Required: false,
	},
	&cli.StringFlag{
		Name:     "posthog-personal-api-key",
		Usage:    "The posthog personal API key for local feature flag evaluation",
		EnvVars:  []string{"POSTHOG_PERSONAL_API_KEY"},
		Required: false,
	},

	&cli.StringFlag{
		Name:     "local-feature-flags-csv",
		Usage:    "Path to a CSV file containing local feature flags. Format: distinct_id,flag,enabled (with header row). The path must be under the server working directory.",
		EnvVars:  []string{"GRAM_LOCAL_FEATURE_FLAGS_CSV"},
		Required: false,
	},
}
