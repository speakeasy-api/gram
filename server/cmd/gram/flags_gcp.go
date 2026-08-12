package gram

import "github.com/urfave/cli/v2"

func gcpFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "gcp-project-id",
			Usage:   "Google Cloud project ID",
			EnvVars: []string{"GRAM_GCP_PROJECT_ID"},
		},
		&cli.StringFlag{
			Name:    "pubsub-emulator-host",
			Usage:   "Host to use for the PubSub emulator",
			EnvVars: []string{"PUBSUB_EMULATOR_HOST"},
		},
	}
}
