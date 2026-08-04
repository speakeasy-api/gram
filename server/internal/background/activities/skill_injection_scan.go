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
		findings, err := s.scanner.Scan(ctx, row.Content, row.OrganizationID, row.ProjectID.String(), "", msg)
		if err != nil {
			// The judge fails open (returns no findings on error), so a non-nil
			// error here is unexpected. Skip this row and let the next sweep retry it.
			s.logger.WarnContext(ctx, "prompt injection scan failed for skill version",
				attr.SlogError(err),
				attr.SlogOrganizationID(row.OrganizationID),
			)
			continue
		}

		flagged := len(findings) > 0
		rationale := ""
		if flagged {
			rationale = findings[0].Description
		}

		if err := queries.MarkSkillVersionInjectionScan(ctx, repo.MarkSkillVersionInjectionScanParams{
			ProjectID:          row.ProjectID,
			SkillVersionID:     row.SkillVersionID,
			InjectionFlagged:   pgtype.Bool{Bool: flagged, Valid: true},
			InjectionRationale: conv.ToPGTextEmpty(rationale),
		}); err != nil {
			return nil, fmt.Errorf("mark skill version injection scan: %w", err)
		}

		result.Processed++
		if flagged {
			result.Flagged++
		}
	}

	return result, nil
}
