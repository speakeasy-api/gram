package gram

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

const (
	// encryptRiskResultsDefaultBatchSize is the rows sealed per statement. Each
	// row costs two local AES-GCM operations and its share of one UPDATE, so
	// 500 rows keep a statement well under a second without a long lock.
	encryptRiskResultsDefaultBatchSize = 500

	// encryptRiskResultsMaxBatchSize bounds one statement's row locks and WAL.
	encryptRiskResultsMaxBatchSize = 10_000
)

// newEncryptRiskResultsCommand cuts one organization over to encrypted risk
// result evidence. It runs where the server runs, because it needs the same
// organization encryption settings and KMS access.
func newEncryptRiskResultsCommand() *cli.Command {
	return &cli.Command{
		Name:  "encrypt-risk-results",
		Usage: "Encrypt an organization's stored risk finding matches under its own key",
		Description: "Provisions the organization's risk-evidence data encryption key, after which every process seals new " +
			"risk_results matches and spans with it, then encrypts the rows already stored in plaintext. " +
			"By default it only counts those rows; pass --dry-run=false to encrypt them. Safe to re-run.",
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "environment", EnvVars: []string{"GRAM_ENVIRONMENT"}, Required: true},
			&cli.StringFlag{Name: "database-url", EnvVars: []string{"GRAM_DATABASE_URL"}, Required: true},
			&cli.StringFlag{Name: "organization-id", Usage: "Organization to cut over", Required: true},
			&cli.IntFlag{Name: "batch-size", Usage: "Rows encrypted per statement", Value: encryptRiskResultsDefaultBatchSize},
			&cli.BoolFlag{Name: "dry-run", Usage: "Only count the rows still in plaintext", Value: true},
		}, orgEncryptionFlags()...),
		Action: func(c *cli.Context) error {
			ctx := c.Context
			logger := PullLogger(ctx)

			organizationID := strings.TrimSpace(c.String("organization-id"))
			if organizationID == "" {
				return errors.New("organization id is required")
			}
			batchSize := c.Int("batch-size")
			if batchSize < 1 || batchSize > encryptRiskResultsMaxBatchSize {
				return fmt.Errorf("batch size must be between 1 and %d", encryptRiskResultsMaxBatchSize)
			}

			db, err := newDBClient(ctx, logger, otel.GetMeterProvider(), c.String("database-url"), dbClientOptions{enableUnsafeLogging: false})
			if err != nil {
				return fmt.Errorf("connect to postgres: %w", err)
			}
			defer db.Close()

			plaintext, err := risk.CountPlaintextResultEvidence(ctx, db, organizationID)
			if err != nil {
				return fmt.Errorf("count plaintext risk results: %w", err)
			}
			if _, err := fmt.Fprintf(c.App.Writer, "%d risk results hold plaintext evidence.\n", plaintext); err != nil {
				return fmt.Errorf("write plaintext count: %w", err)
			}
			if c.Bool("dry-run") {
				if _, err := fmt.Fprintln(c.App.Writer, "Dry run: nothing changed. Pass --dry-run=false to provision the key and encrypt them."); err != nil {
					return fmt.Errorf("write dry run notice: %w", err)
				}
				return nil
			}

			service, provisioner, err := newOrgEncryption(ctx, logger, c, db, newGCPIdentity(ctx, logger, c), newAuditLogger())
			if err != nil {
				return err
			}
			if service == nil || provisioner == nil {
				return errors.New("organization encryption is not configured: set GRAM_ORG_ENCRYPTION_CREDENTIAL_ID")
			}

			if err := provisioner.EnsureUseCase(ctx, organizationID, orgencryption.UseCaseRiskEvidence, orgencryption.SystemActor()); err != nil {
				return fmt.Errorf("provision risk evidence key: %w", err)
			}

			// Other processes can keep writing plaintext until their cached view
			// of the organization's keys expires. Waiting it out means the pass
			// below also catches their last plaintext rows.
			if _, err := fmt.Fprintf(c.App.Writer, "Key provisioned. Waiting %s for every process to start encrypting.\n", orgencryption.ConfigCacheTTL); err != nil {
				return fmt.Errorf("write provisioning notice: %w", err)
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("wait for organization encryption caches: %w", ctx.Err())
			case <-time.After(orgencryption.ConfigCacheTTL):
			}

			report, err := risk.BackfillResultEvidence(ctx, logger, db, risk.NewResultEvidenceCipher(service), organizationID, conv.SafeInt32(batchSize))
			if _, werr := fmt.Fprintf(c.App.Writer, "Encrypted %d risk results across %d projects; %d were already encrypted.\n", report.Sealed, report.Projects, report.Skipped); werr != nil {
				return fmt.Errorf("write backfill report: %w", werr)
			}
			if err != nil {
				return fmt.Errorf("encrypt risk results (safe to re-run): %w", err)
			}

			remaining, err := risk.CountPlaintextResultEvidence(ctx, db, organizationID)
			if err != nil {
				return fmt.Errorf("count remaining plaintext risk results: %w", err)
			}
			if remaining > 0 {
				return fmt.Errorf("%d risk results still hold plaintext evidence; re-run the command", remaining)
			}

			return nil
		},
	}
}
