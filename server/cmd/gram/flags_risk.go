package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
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

// riskLLMFlags configure the fine-tuned risk model client. Only the streams
// process consumes them: both analyzer lanes run there, while the server and
// worker only publish requests.
func riskLLMFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "risk-llm-url",
			Usage:   "OpenAI-compatible base URL of the fine-tuned risk model, including the /v1 segment. Empty disables the LLM risk analyzer.",
			EnvVars: []string{"GRAM_RISK_LLM_URL"},
		},
		&cli.StringFlag{
			Name:    "risk-llm-api-key",
			Usage:   "Bearer token for the fine-tuned risk model endpoint",
			EnvVars: []string{"GRAM_RISK_LLM_API_KEY"},
		},
		&cli.StringFlag{
			Name:    "risk-llm-model",
			Usage:   "Served model name of the fine-tuned risk model; must equal the deployment's --served-model-name",
			EnvVars: []string{"GRAM_RISK_LLM_MODEL"},
			Value:   llmanalyzer.DefaultModel,
		},
	}
}

// llmAnalyzerConfigFromCLI reads the risk LLM flags into a client config.
// Timeout and max tokens are code constants, not flags.
func llmAnalyzerConfigFromCLI(c *cli.Context) llmanalyzer.Config {
	return llmanalyzer.Config{
		BaseURL:   c.String("risk-llm-url"),
		APIKey:    c.String("risk-llm-api-key"),
		Model:     c.String("risk-llm-model"),
		Timeout:   llmanalyzer.DefaultTimeout,
		MaxTokens: llmanalyzer.DefaultMaxTokens,
	}
}
