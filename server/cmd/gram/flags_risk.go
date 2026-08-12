package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/risk"
)

// riskFingerprintPepperFlag carries the keyring every risk host needs: the
// streams writer fingerprints each row it writes, and the server and worker
// recompute those fingerprints to match exact-value exclusions.
func riskFingerprintPepperFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "risk-fingerprint-pepper-keyring",
		Usage:   "JSON payload containing the pepper keyring for fingerprinting risk findings",
		EnvVars: []string{"GRAM_RISK_FINGERPRINT_PEPPER_KEYRING"},
	}
}

// riskIngestFlags are the flags the streams process consumes: it owns the
// ClickHouse risk_findings write path.
func riskIngestFlags() []cli.Flag {
	return []cli.Flag{
		riskFingerprintPepperFlag(),
		&cli.BoolFlag{
			Name:    "disable-clickhouse-risk-writes",
			Usage:   "Disable the ClickHouse risk_findings subscriber (kill switch for the shadow write path)",
			EnvVars: []string{"GRAM_DISABLE_CLICKHOUSE_RISK_WRITES"},
			Value:   false,
		},
	}
}

// riskReconcileFlags are the flags the server and worker consume: both wire
// the retroactive risk-exclusion reconcile.
func riskReconcileFlags() []cli.Flag {
	return []cli.Flag{
		riskFingerprintPepperFlag(),
		&cli.BoolFlag{
			Name:    "disable-clickhouse-risk-retro-reconcile",
			Usage:   "Disable propagating retroactive risk-exclusion changes into ClickHouse (kill switch; the Postgres reconcile still runs)",
			EnvVars: []string{"GRAM_DISABLE_CLICKHOUSE_RISK_RETRO_RECONCILE"},
			Value:   false,
		},
	}
}

// parseOptionalPepperKeyRing builds the fingerprinter for the server and
// worker, where the keyring is optional — it only powers exact-match
// retroactive exclusion propagation to ClickHouse, which degrades with a loud
// log when absent. Streams parses it strictly instead, since every row it
// writes carries a fingerprint.
func parseOptionalPepperKeyRing(ctx context.Context, logger *slog.Logger, raw string) (risk.Fingerprinter, error) {
	if raw == "" {
		logger.WarnContext(ctx, "risk fingerprint pepper keyring not configured; exact-match retroactive exclusion propagation to clickhouse disabled")
		var disabled risk.Fingerprinter
		return disabled, nil
	}

	fingerprinter, err := risk.ParsePepperKeyRing([]byte(raw))
	if err != nil {
		return fingerprinter, fmt.Errorf("parse risk fingerprint pepper keyring: %w", err)
	}
	return fingerprinter, nil
}
