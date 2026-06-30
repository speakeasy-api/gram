package gram

import "github.com/urfave/cli/v2"

var gcpFlags = []cli.Flag{
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

	&cli.BoolFlag{
		Name:    "disable-bigquery-writes",
		Usage:   "Disable writes to BigQuery",
		EnvVars: []string{"DISABLE_BIGQUERY_WRITES"},
	},
	&cli.StringFlag{
		Name:    "bq-risk-findings",
		Usage:   "BigQuery dataset for risk findings",
		Value:   "gram.risk_findings",
		EnvVars: []string{"GRAM_BQ_RISK_FINDINGS"},
	},
}
