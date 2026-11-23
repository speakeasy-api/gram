package gram

import (
	"context"
	"log/slog"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

func functionsFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     "functions-provider",
			Usage:    "Determines what provider to use to deploy and call Speakeasy Functions. Allowed values: local, flyio, disabled. With disabled, no Fly.io or Tigris settings are needed and Functions APIs report that Functions are not available on this deployment.",
			EnvVars:  []string{"GRAM_FUNCTIONS_PROVIDER"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "functions-runner-oci-image",
			Usage:    "The name of the OCI image for the Speakeasy Functions runner. It must not include a tag.",
			EnvVars:  []string{"GRAM_FUNCTIONS_RUNNER_OCI_IMAGE"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "functions-runner-version",
			Usage:    "The version of the Speakeasy Functions runner to use. It must exist in the OCI registry.",
			EnvVars:  []string{"GRAM_FUNCTIONS_RUNNER_VERSION"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "functions-flyio-api-token",
			Usage:    "An organization-scoped API token to use when deploying Speakeasy Functions to fly.io.",
			EnvVars:  []string{"GRAM_FUNCTIONS_FLYIO_API_TOKEN"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "functions-flyio-org",
			Usage:    "The default fly.io organization to deploy Speakeasy Functions runner apps to.",
			EnvVars:  []string{"GRAM_FUNCTIONS_FLYIO_ORG"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "functions-flyio-region",
			Usage:    "The default fly.io region to deploy Speakeasy Functions runner apps to.",
			EnvVars:  []string{"GRAM_FUNCTIONS_FLYIO_REGION"},
			Value:    "us",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "functions-tigris-bucket-uri",
			Usage:    "The URI of the Tigris bucket to use for storing function artifacts.",
			EnvVars:  []string{"GRAM_FUNCTIONS_TIGRIS_BUCKET_URI"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "functions-tigris-key",
			Usage:    "The access key for the Tigris bucket.",
			EnvVars:  []string{"GRAM_FUNCTIONS_TIGRIS_KEY"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "functions-tigris-secret",
			Usage:    "The secret key for the Tigris bucket.",
			EnvVars:  []string{"GRAM_FUNCTIONS_TIGRIS_SECRET"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "functions-local-runner-root",
			Usage:    "Path to the functions package containing the entrypoints for various runtimes.",
			EnvVars:  []string{"GRAM_FUNCTIONS_LOCAL_RUNNER_ROOT"},
			Required: false,
		},
	}
}

// warnIgnoredFunctionsFlags logs, by environment variable name only, each
// provider setting that the disabled Functions provider ignores. Partitions
// can share these settings while one of them runs with Functions off, so they
// are ignored rather than rejected. Values are never logged because some of
// them are secrets. Each name is a flag whose environment variable is the flag
// name upper-cased, with dashes as underscores and a GRAM_ prefix.
func warnIgnoredFunctionsFlags(ctx context.Context, c *cli.Context, logger *slog.Logger, names ...string) {
	for _, name := range names {
		if c.String(name) == "" {
			continue
		}

		envVar := "GRAM_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
		logger.WarnContext(ctx, "ignoring Functions provider setting: the Functions provider is disabled", attr.SlogEnvVarName(envVar))
	}
}
