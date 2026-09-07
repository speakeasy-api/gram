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
			Name:  "local-kms-signing-algorithm",
			Usage: "Algorithm the in-process KMS stand-in signs with in local development (RS256 or ES256). Keys recorded with a different algorithm report a mismatch, which is how that path stays reachable without a cloud KMS.",
			// Not DefaultText: an empty value is what tells newKMSSigningClients to
			// use its own default, so the default lives there rather than here.
			EnvVars: []string{"GRAM_LOCAL_KMS_SIGNING_ALGORITHM"},
		},
		&cli.StringFlag{
			Name:    "pubsub-emulator-host",
			Usage:   "Host to use for the PubSub emulator",
			EnvVars: []string{"PUBSUB_EMULATOR_HOST"},
		},
	}
}
