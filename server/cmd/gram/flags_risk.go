package gram

import "github.com/urfave/cli/v2"

var riskFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "risk-fingerprint-pepper-keyring",
		Usage:   "JSON payload containing the pepper keyring for fingerprinting risk findings",
		EnvVars: []string{"GRAM_RISK_FINGERPRINT_PEPPER_KEYRING"},
	},
	&cli.BoolFlag{
		Name:    "disable-clickhouse-risk-writes",
		Usage:   "Disable the ClickHouse risk_findings subscriber (kill switch for the shadow write path)",
		EnvVars: []string{"GRAM_DISABLE_CLICKHOUSE_RISK_WRITES"},
		Value:   false,
	},
}
