package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

type ScanSkillVersionsForInjectionParams struct {
	BatchSize int32 `json:"batch_size"`
}

type ScanSkillVersionsForInjectionResult struct {
	Processed int  `json:"processed"`
	Flagged   int  `json:"flagged"`
	HasMore   bool `json:"has_more"`
}

// SkillInjectionScanner classifies captured/authored skill manifests for prompt
// injection content and records the verdict on the version. It reuses the
// realtime prompt-injection judge, so no new model or pub/sub topology is added.
type SkillInjectionScanner struct {
	logger  *slog.Logger
	db      *pgxpool.Pool
	scanner *promptinjection.Scanner
}

func NewSkillInjectionScanner(logger *slog.Logger, db *pgxpool.Pool, scanner *promptinjection.Scanner) *SkillInjectionScanner {
	return &SkillInjectionScanner{logger: logger, db: db, scanner: scanner}
}

func (s *SkillInjectionScanner) Scan(ctx context.Context, params ScanSkillVersionsForInjectionParams) (*ScanSkillVersionsForInjectionResult, error) {
	if params.BatchSize <= 0 {
		// A non-positive batch would either error every attempt (negative LIMIT) or
		// wedge the drain in a perpetual continue-as-new (zero rows, HasMore false is
		// never reached because nothing is scanned). Reject malformed inputs early.
		return nil, fmt.Errorf("batch size must be positive, got %d", params.BatchSize)
	}
	queries := repo.New(s.db)
	rows, err := queries.ListUnscannedSkillVersions(ctx, params.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("list unscanned skill versions: %w", err)
	}

	result := &ScanSkillVersionsForInjectionResult{Processed: 0, Flagged: 0, HasMore: len(rows) == int(params.BatchSize)}
	for _, row := range rows {
		// The manifest is untrusted content the agent will load as instructions,
		// so frame it as an attachment; the judge treats the body as data either way.
		msg := judgemessage.New(message.PromptAttachment, "", row.Content)
		// ScanStrict (not Scan) so a judge that failed to reach a verdict returns
		// an error instead of a false SAFE: we skip the row and leave it in the
		// queue for the next sweep rather than permanently marking it scanned.
		findings, err := s.scanner.ScanStrict(ctx, row.Content, row.OrganizationID, row.ProjectID.String(), "", msg)
		if err != nil {
			s.logger.WarnContext(ctx, "prompt injection scan failed for skill version",
				attr.SlogError(err),
				attr.SlogOrganizationID(row.OrganizationID),
			)
			// ponytail: a row whose judge call always fails rides the head of the
			// queue and burns one call per sweep forever. Bounded (other rows in the
			// batch still drain), so no dead-letter yet; add a failure-count column and
			// a permanent-failure verdict if a stuck manifest shows up in practice.
			continue
		}

		flagged := len(findings) > 0
		rationale := ""
		if flagged {
			rationale = findings[0].Description
		}

		marked, err := queries.MarkSkillVersionInjectionScan(ctx, repo.MarkSkillVersionInjectionScanParams{
			ProjectID:          row.ProjectID,
			SkillVersionID:     row.SkillVersionID,
			InjectionFlagged:   pgtype.Bool{Bool: flagged, Valid: true},
			InjectionRationale: conv.ToPGTextEmpty(rationale),
		})
		if err != nil {
			return nil, fmt.Errorf("mark skill version injection scan: %w", err)
		}
		if marked == 0 {
			// The version was deleted or moved projects between the list and this
			// update, so the verdict wasn't persisted. Don't count it as scanned;
			// if it still exists it stays queued for the next sweep.
			continue
		}

		result.Processed++
		if flagged {
			result.Flagged++
		}
	}

	return result, nil
}
