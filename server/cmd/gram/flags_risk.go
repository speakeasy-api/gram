package gram

import "github.com/urfave/cli/v2"

var riskFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "risk-fingerprint-pepper-keyring",
		Usage:   "JSON payload containing the pepper keyring for fingerprinting risk findings",
		EnvVars: []string{"GRAM_RISK_FINGERPRINT_PEPPER_KEYRING"},
	},
}
