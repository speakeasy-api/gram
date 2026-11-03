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

// lockCuration preserves source-before-candidate lock ordering and canonical tenant ownership.
func lockCuration(ctx context.Context, tx pgx.Tx, organizationID string, id uuid.UUID) (repo.RegistryPromotionCandidate, error) {
	q := repo.New(tx)
	target, err := q.GetCurationTarget(ctx, repo.GetCurationTargetParams{OrganizationID: organizationID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return target, ErrNotFound
	}
	if err != nil {
		return target, fmt.Errorf("read curation target: %w", err)
	}
	if !target.ProjectID.Valid || !target.McpServerID.Valid {
		return target, ErrNotFound
	}
	_, err = q.GetSource(ctx, repo.GetSourceParams{OrganizationID: organizationID, ProjectID: target.ProjectID.UUID, McpServerID: target.McpServerID.UUID})
	if errors.Is(err, pgx.ErrNoRows) {
		return target, ErrNotFound
	}
	if err != nil {
		return target, fmt.Errorf("lock curation source: %w", err)
	}
	row, err := q.GetPromotion(ctx, repo.GetPromotionParams{OrganizationID: organizationID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrNotFound
	}
	if err != nil {
		return row, fmt.Errorf("lock curation candidate: %w", err)
	}
	if row.ProjectID != target.ProjectID || row.McpServerID != target.McpServerID {
		return row, ErrStale
	}
	return row, nil
}

func validateCuration(row repo.RegistryPromotionCandidate, revision, action string) error {
	if row.PromotedAt.Valid {
		return ErrConflict
	}
	if row.CurationRevision.String() != revision {
		return ErrStale
	}
	switch action {
	case "reject", "archive":
		if row.CurationStatus != "active" {
			return ErrConflict
		}
	case "restore":
		if row.CurationStatus != "rejected" && row.CurationStatus != "archived" {
			return ErrConflict
		}
	default:
		return ErrInvalid
	}
	return nil
}

// ValidateCurationTx locks the canonical source and exact review disposition.
func (s *Service) ValidateCurationTx(ctx context.Context, tx pgx.Tx, organizationID string, id uuid.UUID, revision, action string) (Candidate, error) {
	row, err := lockCuration(ctx, tx, organizationID, id)
	if err != nil {
		return Candidate{}, err
	}
	if err := validateCuration(row, revision, action); err != nil {
		return Candidate{}, err
	}
	return s.candidate(row), nil
}

// CurateTx changes one unpublished candidate and its audit record atomically.
func (s *Service) CurateTx(ctx context.Context, tx pgx.Tx, request CurationRequest, actor string) (Candidate, error) {
	if request.OrganizationID == "" || request.PromotionID == uuid.Nil || strings.TrimSpace(request.RetryKey) == "" || len(request.RetryKey) > 128 {
		return Candidate{}, ErrInvalid
	}
	principal, err := urn.ParsePrincipal(actor)
	if err != nil {
		return Candidate{}, ErrInvalid
	}
	prior, err := lockCuration(ctx, tx, request.OrganizationID, request.PromotionID)
	if err != nil {
		return Candidate{}, err
	}
	if prior.CurationRetryKey.Valid && prior.CurationRetryKey.String == request.RetryKey {
		if prior.CurationRetryRevision.String != request.CurationRevision || prior.CurationRetryAction.String != request.Action || prior.CurationUpdatedBy.String != actor {
			return Candidate{}, ErrConflict
		}
		return s.candidate(prior), nil
	}
	if err := validateCuration(prior, request.CurationRevision, request.Action); err != nil {
		return Candidate{}, err
	}
	status := "active"
	switch request.Action {
	case "reject":
		status = "rejected"
	case "archive":
		status = "archived"
	}
	row, err := repo.New(tx).CurateCandidate(ctx, repo.CurateCandidateParams{ID: prior.ID, OrganizationID: prior.OrganizationID, ProjectID: prior.ProjectID, CurationRevision: prior.CurationRevision, CurationStatus: status, Actor: conv.ToPGText(actor), RetryKey: conv.ToPGText(request.RetryKey), RetryRevision: conv.ToPGText(request.CurationRevision), Action: conv.ToPGText(request.Action)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrStale
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("update candidate curation: %w", err)
	}
	logger := audit.NewLogger()
	switch request.Action {
	case "reject":
		err = logger.LogCatalogPromotionReject(ctx, tx, audit.LogCatalogPromotionRejectEvent{OrganizationID: row.OrganizationID, ProjectID: row.ProjectID, Actor: principal, CatalogPromotionURN: urn.NewCatalogPromotion(row.ID)})
	case "archive":
		err = logger.LogCatalogPromotionArchive(ctx, tx, audit.LogCatalogPromotionArchiveEvent{OrganizationID: row.OrganizationID, ProjectID: row.ProjectID, Actor: principal, CatalogPromotionURN: urn.NewCatalogPromotion(row.ID)})
	case "restore":
		err = logger.LogCatalogPromotionRestore(ctx, tx, audit.LogCatalogPromotionRestoreEvent{OrganizationID: row.OrganizationID, ProjectID: row.ProjectID, Actor: principal, CatalogPromotionURN: urn.NewCatalogPromotion(row.ID)})
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("audit candidate curation: %w", err)
	}
	return s.candidate(row), nil
}
