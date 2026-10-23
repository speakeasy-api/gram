package admin

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SetCatalogPromotionService installs the shared promotion domain for HTTP and Admin MCP.
func (s *Service) SetCatalogPromotionService(service *catalogpromotion.Service) {
	s.catalogPromotion = service
}

// CatalogPromotionService exposes the same transaction-aware domain to approved Admin MCP writes.
func (s *Service) CatalogPromotionService() *catalogpromotion.Service { return s.catalogPromotion }

func promotionCandidateView(c catalogpromotion.Candidate) *gen.AdminRegistryPromotionCandidate {
	return &gen.AdminRegistryPromotionCandidate{ID: c.ID, OrganizationID: c.OrganizationID, ProjectID: c.ProjectID, McpServerID: c.MCPServerID, RemoteMcpServerID: c.RemoteMCPServerID,
		SourceName: conv.PtrEmpty(c.SourceName), SourceRevision: c.SourceRevision, Eligible: c.Eligible, Reason: c.Reason, Status: c.Status, ObservedSuccesses: c.ObservedSuccesses, ObservedFailures: c.ObservedFailures, ObservedDays: c.ObservedDays,
		CheckedAt: conv.PtrEmpty(c.CheckedAt), RegistryEntryID: conv.PtrEmpty(c.RegistryEntryID), DocsStatus: conv.PtrEmpty(c.DocsStatus), DocsURL: conv.PtrEmpty(c.DocsURL), DeliveryError: conv.PtrEmpty(c.DeliveryError), DocsRevision: c.DocsRevision, DocsRetryAllowed: c.DocsRetryAllowed, DocsAttempts: c.DocsAttempts,
		DocsLastAttemptAt: conv.PtrEmpty(c.DocsLastAttemptAt), DocsNextAttemptAt: conv.PtrEmpty(c.DocsNextAttemptAt), DocsQueuedAt: conv.PtrEmpty(c.DocsQueuedAt), PromotedAt: conv.PtrEmpty(c.PromotedAt), PublicTitle: conv.PtrEmpty(c.PublicTitle)}
}

func promotionTarget(organizationID, projectID, serverID string) (catalogpromotion.Target, error) {
	project, err := uuid.Parse(projectID)
	if err != nil {
		return catalogpromotion.Target{}, oops.C(oops.CodeBadRequest)
	}
	server, err := uuid.Parse(serverID)
	if err != nil {
		return catalogpromotion.Target{}, oops.C(oops.CodeBadRequest)
	}
	if organizationID == "" {
		return catalogpromotion.Target{}, oops.C(oops.CodeBadRequest)
	}
	return catalogpromotion.Target{OrganizationID: organizationID, ProjectID: project, MCPServerID: server}, nil
}

func promotionError(err error) error {
	switch {
	case errors.Is(err, catalogpromotion.ErrUnavailable):
		return oops.E(oops.CodeUnavailable, nil, "catalogue promotion validation is temporarily unavailable; retry")
	case errors.Is(err, catalogpromotion.ErrNotFound):
		return oops.C(oops.CodeNotFound)
	case errors.Is(err, catalogpromotion.ErrStale), errors.Is(err, catalogpromotion.ErrConflict):
		return oops.E(oops.CodeConflict, nil, "promotion source changed or was already promoted; refresh before retrying")
	case errors.Is(err, catalogpromotion.ErrInvalid):
		return oops.E(oops.CodeBadRequest, nil, "invalid catalogue promotion input; check target identifiers, pagination cursor, retry key, and reviewed public metadata")
	case errors.Is(err, catalogpromotion.ErrIneligible):
		return oops.E(oops.CodeBadRequest, nil, "remote server does not meet the configuration and recent usage requirements for promotion")
	default:
		return oops.E(oops.CodeUnexpected, err, "catalogue promotion is unavailable")
	}
}

func (s *Service) ListRegistryPromotionCandidates(ctx context.Context, p *gen.ListRegistryPromotionCandidatesPayload) (*gen.AdminRegistryPromotionCandidates, error) {
	if s.catalogPromotion == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	rows, next, err := s.catalogPromotion.ListCandidates(ctx, conv.PtrValOr(p.Cursor, ""), p.Limit)
	if err != nil {
		return nil, promotionError(err)
	}
	candidates := make([]*gen.AdminRegistryPromotionCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, promotionCandidateView(row))
	}
	return &gen.AdminRegistryPromotionCandidates{Candidates: candidates, NextCursor: conv.PtrEmpty(next)}, nil
}

func (s *Service) GetRegistryPromotionCandidate(ctx context.Context, p *gen.GetRegistryPromotionCandidatePayload) (*gen.AdminRegistryPromotionCandidate, error) {
	if s.catalogPromotion == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	target, err := promotionTarget(p.OrganizationID, p.ProjectID, p.McpServerID)
	if err != nil {
		return nil, err
	}
	result, err := s.catalogPromotion.GetCandidate(ctx, target)
	if err != nil {
		return nil, promotionError(err)
	}
	return promotionCandidateView(result), nil
}

func (s *Service) PromoteRegistryCandidate(ctx context.Context, p *gen.PromoteRegistryCandidatePayload) (*gen.AdminRegistryPromotionCandidate, error) {
	if s.catalogPromotion == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	target, err := promotionTarget(p.OrganizationID, p.ProjectID, p.McpServerID)
	if err != nil {
		return nil, err
	}
	actor, _, _ := adminActor(ctx)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, promotionError(err)
	}
	defer o11y.NoLogDefer(func() error {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return tx.Rollback(cleanupCtx)
	})
	result, err := s.catalogPromotion.PromoteTx(ctx, tx, catalogpromotion.Request{Target: target, SourceRevision: p.SourceRevision,
		Public:                  catalogpromotion.PublicMetadata{Name: p.PublicName, Title: p.PublicTitle, Description: p.PublicDescription, URL: p.PublicURL, DocumentationURL: p.DocumentationURL, DocsSlug: p.DocsSlug, AuthMode: p.AuthMode},
		PublicMetadataConfirmed: p.PublicMetadataConfirmed, RetryKey: p.RetryKey}, actor.String())
	if err != nil {
		return nil, promotionError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, promotionError(err)
	}
	return promotionCandidateView(result), nil
}

func (s *Service) RetryRegistryPromotionDocs(ctx context.Context, p *gen.RetryRegistryPromotionDocsPayload) (*gen.AdminRegistryPromotionCandidate, error) {
	if s.catalogPromotion == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	id, err := uuid.Parse(p.PromotionID)
	if err != nil || p.OrganizationID == "" {
		return nil, oops.C(oops.CodeBadRequest)
	}
	actor, _, _ := adminActor(ctx)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, promotionError(err)
	}
	defer o11y.NoLogDefer(func() error {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return tx.Rollback(cleanupCtx)
	})
	result, err := s.catalogPromotion.RetryDocsTx(ctx, tx, catalogpromotion.DocsRetryRequest{OrganizationID: p.OrganizationID, PromotionID: id, DocsRevision: p.DocsRevision, RetryKey: p.RetryKey}, actor.String())
	if err != nil {
		return nil, promotionError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, promotionError(err)
	}
	return promotionCandidateView(result), nil
}
