package catalogpromotion

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ValidateDocsRetryTx locks and validates the exact delivery state approved by staff.
func (s *Service) ValidateDocsRetryTx(ctx context.Context, tx pgx.Tx, organizationID string, id uuid.UUID, revision string) (Candidate, error) {
	row, err := repo.New(tx).GetPromotion(ctx, repo.GetPromotionParams{OrganizationID: organizationID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("read documentation retry state: %w", err)
	}
	result := s.candidate(row)
	if row.DocsRevision.String() != revision {
		return Candidate{}, ErrStale
	}
	if !result.DocsRetryAllowed {
		return Candidate{}, ErrConflict
	}
	return result, nil
}

// RetryDocsTx schedules one exact retry and audit event atomically, including orphaned publications.
func (s *Service) RetryDocsTx(ctx context.Context, tx pgx.Tx, request DocsRetryRequest, actor string) (Candidate, error) {
	if request.OrganizationID == "" || request.PromotionID == uuid.Nil || strings.TrimSpace(request.RetryKey) == "" || len(request.RetryKey) > 128 {
		return Candidate{}, ErrInvalid
	}
	principal, err := urn.ParsePrincipal(actor)
	if err != nil {
		return Candidate{}, ErrInvalid
	}
	q := repo.New(tx)
	prior, err := q.GetPromotion(ctx, repo.GetPromotionParams{OrganizationID: request.OrganizationID, ID: request.PromotionID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("read documentation retry receipt: %w", err)
	}
	if prior.DocsRetryKey.Valid && prior.DocsRetryKey.String == request.RetryKey {
		if prior.DocsRetryRevision.String != request.DocsRevision || prior.DocsRetryBy.String != actor {
			return Candidate{}, ErrConflict
		}
		return s.candidate(prior), nil
	}
	if _, err := s.ValidateDocsRetryTx(ctx, tx, request.OrganizationID, request.PromotionID, request.DocsRevision); err != nil {
		return Candidate{}, err
	}
	row, err := q.RetryDocs(ctx, repo.RetryDocsParams{OrganizationID: request.OrganizationID, ID: request.PromotionID, RetryKey: conv.ToPGText(request.RetryKey), RetryRevision: conv.ToPGText(request.DocsRevision), Actor: conv.ToPGText(actor), ProjectID: prior.ProjectID})
	if err != nil {
		return Candidate{}, fmt.Errorf("schedule documentation retry: %w", err)
	}
	if err := audit.NewLogger().LogCatalogPromotionRetryDocs(ctx, tx, audit.LogCatalogPromotionRetryDocsEvent{OrganizationID: row.OrganizationID, ProjectID: row.ProjectID, Actor: principal, CatalogPromotionURN: urn.NewCatalogPromotion(row.ID)}); err != nil {
		return Candidate{}, fmt.Errorf("audit documentation retry: %w", err)
	}
	return s.candidate(row), nil
}
