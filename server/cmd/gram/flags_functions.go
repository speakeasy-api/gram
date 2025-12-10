package gram

import "github.com/urfave/cli/v2"

var functionsFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "functions-provider",
		Usage:    "Determines what provider to use to deploy and call Gram Functions. Allowed values: local, flyio.",
		EnvVars:  []string{"GRAM_FUNCTIONS_PROVIDER"},
		Required: false,
	},
	&cli.StringFlag{
		Name:     "functions-runner-oci-image",
		Usage:    "The name of the OCI image for the Gram Functions runner. It must not include a tag.",
		EnvVars:  []string{"GRAM_FUNCTIONS_RUNNER_OCI_IMAGE"},
		Required: false,
	},
	&cli.StringFlag{
		Name:     "functions-runner-version",
		Usage:    "The version of the Gram Functions runner to use. It must exist in the OCI registry.",
		EnvVars:  []string{"GRAM_FUNCTIONS_RUNNER_VERSION"},
		Required: false,
	},
	&cli.StringFlag{
		Name:     "functions-flyio-api-token",
		Usage:    "An organization-scoped API token to use when deploying Gram Functions to fly.io.",
		EnvVars:  []string{"GRAM_FUNCTIONS_FLYIO_API_TOKEN"},
		Required: false,
	},
	&cli.StringFlag{
		Name:     "functions-flyio-org",
		Usage:    "The default fly.io organization to deploy Gram Functions runner apps to.",
		EnvVars:  []string{"GRAM_FUNCTIONS_FLYIO_ORG"},
		Required: false,
	},
	&cli.StringFlag{
		Name:     "functions-flyio-region",
		Usage:    "The default fly.io region to deploy Gram Functions runner apps to.",
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
