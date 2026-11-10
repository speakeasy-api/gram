package risk

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// ResultEvidenceBackfillReport counts what one backfill run did.
type ResultEvidenceBackfillReport struct {
	// Projects is how many of the organization's projects were walked.
	Projects int

	// Sealed is how many rows moved from plaintext to the encrypted columns.
	Sealed int64

	// Skipped is how many rows were left alone because they gained sealed
	// evidence between being read and being written.
	Skipped int64
}

// BackfillResultEvidence seals the plaintext match and spans of every
// risk_results row the organization owns, one project and one page of
// batchSize rows at a time. The organization must already have its
// risk-evidence DEK. Each page is sealed, opened again to prove the
// ciphertexts read back, and written in one statement that also clears the
// plaintext. A rerun only reads rows still in plaintext, so the backfill can be
// repeated or resumed.
func BackfillResultEvidence(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, cipher *ResultEvidenceCipher, organizationID string, batchSize int32) (ResultEvidenceBackfillReport, error) {
	report := ResultEvidenceBackfillReport{Projects: 0, Sealed: 0, Skipped: 0}
	if batchSize <= 0 {
		return report, fmt.Errorf("batch size must be positive, got %d", batchSize)
	}

	q := repo.New(db)
	projectIDs, err := q.ListOrganizationProjectIDsForEvidenceBackfill(ctx, organizationID)
	if err != nil {
		return report, fmt.Errorf("list organization projects: %w", err)
	}

	for _, projectID := range projectIDs {
		sealed, skipped, err := backfillProjectResultEvidence(ctx, q, cipher, organizationID, projectID, batchSize)
		report.Projects++
		report.Sealed += sealed
		report.Skipped += skipped
		if err != nil {
			return report, fmt.Errorf("project %s: %w", projectID, err)
		}

		if sealed > 0 || skipped > 0 {
			logger.InfoContext(ctx, "sealed project risk result evidence",
				attr.SlogOrganizationID(organizationID),
				attr.SlogProjectID(projectID.String()),
				attr.SlogRiskEvidenceSealedCount(sealed),
			)
		}
	}

	return report, nil
}

func backfillProjectResultEvidence(ctx context.Context, q *repo.Queries, cipher *ResultEvidenceCipher, organizationID string, projectID uuid.UUID, batchSize int32) (sealed int64, skipped int64, err error) {
	afterCreatedAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.NegativeInfinity, Valid: true}
	afterID := uuid.Nil

	for {
		rows, err := q.ListPlaintextRiskResultEvidenceBatch(ctx, repo.ListPlaintextRiskResultEvidenceBatchParams{
			ProjectID:      projectID,
			OrganizationID: organizationID,
			AfterCreatedAt: afterCreatedAt,
			AfterID:        afterID,
			BatchLimit:     batchSize,
		})
		if err != nil {
			return sealed, skipped, fmt.Errorf("list plaintext evidence: %w", err)
		}
		if len(rows) == 0 {
			return sealed, skipped, nil
		}

		params := repo.SealRiskResultEvidenceBatchParams{
			ProjectID:      projectID,
			OrganizationID: organizationID,
			Ids:            make([]uuid.UUID, 0, len(rows)),
			MatchEncrypted: make([]string, 0, len(rows)),
			SpansEncrypted: make([]string, 0, len(rows)),
		}
		for _, row := range rows {
			evidence, err := sealAndVerify(ctx, cipher, row)
			if err != nil {
				return sealed, skipped, fmt.Errorf("risk result %s: %w", row.ID, err)
			}
			params.Ids = append(params.Ids, row.ID)
			params.MatchEncrypted = append(params.MatchEncrypted, evidence.MatchEncrypted.String)
			params.SpansEncrypted = append(params.SpansEncrypted, evidence.SpansEncrypted.String)
		}

		written, err := q.SealRiskResultEvidenceBatch(ctx, params)
		if err != nil {
			return sealed, skipped, fmt.Errorf("store sealed evidence: %w", err)
		}
		sealed += written
		skipped += int64(len(rows)) - written

		last := rows[len(rows)-1]
		afterCreatedAt, afterID = last.CreatedAt, last.ID
		if len(rows) < int(batchSize) {
			return sealed, skipped, nil
		}
	}
}

// sealAndVerify seals one row's evidence and opens it again, so a ciphertext
// that would not read back never replaces the plaintext.
func sealAndVerify(ctx context.Context, cipher *ResultEvidenceCipher, row repo.ListPlaintextRiskResultEvidenceBatchRow) (ResultEvidence, error) {
	evidence, err := cipher.SealEncrypted(ctx, row.OrganizationID, row.Match, row.Spans)
	if err != nil {
		return ResultEvidence{}, err
	}

	match, err := cipher.OpenMatch(ctx, row.OrganizationID, evidence.Match, evidence.MatchEncrypted)
	if err != nil {
		return ResultEvidence{}, err
	}
	if match != row.Match {
		return ResultEvidence{}, fmt.Errorf("sealed match does not read back")
	}

	spans, err := cipher.OpenSpans(ctx, row.OrganizationID, evidence.Spans, evidence.SpansEncrypted)
	if err != nil {
		return ResultEvidence{}, err
	}
	if !bytes.Equal(spans, row.Spans) {
		return ResultEvidence{}, fmt.Errorf("sealed spans do not read back")
	}

	return evidence, nil
}

// CountPlaintextResultEvidence counts the organization's risk_results rows
// that still hold match or spans in plaintext.
func CountPlaintextResultEvidence(ctx context.Context, db *pgxpool.Pool, organizationID string) (int64, error) {
	q := repo.New(db)
	projectIDs, err := q.ListOrganizationProjectIDsForEvidenceBackfill(ctx, organizationID)
	if err != nil {
		return 0, fmt.Errorf("list organization projects: %w", err)
	}

	var total int64
	for _, projectID := range projectIDs {
		count, err := q.CountPlaintextRiskResultEvidence(ctx, repo.CountPlaintextRiskResultEvidenceParams{
			ProjectID:      projectID,
			OrganizationID: organizationID,
		})
		if err != nil {
			return total, fmt.Errorf("count project %s: %w", projectID, err)
		}
		total += count
	}

	return total, nil
}
