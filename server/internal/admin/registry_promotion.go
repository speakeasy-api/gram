package admin

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/catalogauth"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
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
	var suggestions *gen.AdminRegistryPromotionSuggestions
	if v := c.SuggestedMetadata; v != nil {
		suggestions = &gen.AdminRegistryPromotionSuggestions{PublicName: conv.PtrEmpty(v.Name), PublicTitle: conv.PtrEmpty(v.Title), PublicDescription: conv.PtrEmpty(v.Description), PublicURL: conv.PtrEmpty(v.URL), DocumentationURL: conv.PtrEmpty(v.DocumentationURL), DocsSlug: conv.PtrEmpty(v.DocsSlug), AuthMode: conv.PtrEmpty(v.AuthMode)}
	}
	var setupGuides []*gen.AdminRegistryPromotionSetupGuide
	for _, guide := range c.SetupGuides {
		setupGuides = append(setupGuides, &gen.AdminRegistryPromotionSetupGuide{Slug: guide.Slug, Title: guide.Title})
	}
	var discovery *gen.AdminRegistryPromotionAuthDiscovery
	if c.AuthDiscovery != nil {
		discovery = &gen.AdminRegistryPromotionAuthDiscovery{SupportsDcr: c.AuthDiscovery.SupportsDCR, SupportsCimd: c.AuthDiscovery.SupportsCIMD, AuthSetup: c.AuthDiscovery.AuthSetup, Status: c.AuthDiscovery.Status}
	}
	var usage *gen.AdminRegistryPromotionAccountUsage
	if v := c.AccountUsage; v != nil {
		tools := make([]*gen.AdminRegistryPromotionToolUsage, 0, len(v.Tools))
		for _, tool := range v.Tools {
			tools = append(tools, &gen.AdminRegistryPromotionToolUsage{Name: tool.Name, Calls: tool.Calls, Successes: tool.Successes})
		}
		usage = &gen.AdminRegistryPromotionAccountUsage{WindowDays: v.WindowDays, DistinctUsers: v.DistinctUsers, Clients: v.Clients, Tools: tools, Transport: v.Transport, ResourceName: conv.PtrEmpty(v.ResourceName), DocumentationURL: conv.PtrEmpty(v.DocumentationURL)}
	}
	return &gen.AdminRegistryPromotionCandidate{AccountUsage: usage, Review: promotionReviewView(c.Review), ReviewAllowed: c.ReviewAllowed, AuthDiscovery: discovery, CreatedAt: c.CreatedAt, CurationRevision: c.CurationRevision, SuggestedMetadata: suggestions,
		SetupGuides: setupGuides, SetupGuidesVersion: conv.PtrEmpty(c.SetupGuidesVersion), EndpointGuideSlug: conv.PtrEmpty(c.EndpointGuideSlug), ID: c.ID, OrganizationID: c.OrganizationID, ProjectID: c.ProjectID, McpServerID: c.MCPServerID, RemoteMcpServerID: c.RemoteMCPServerID,
		SourceName: conv.PtrEmpty(c.SourceName), SourceRevision: c.SourceRevision, Eligible: c.Eligible, CanOverrideEligibility: c.CanOverrideEligibility(), Reason: c.Reason, Status: c.Status, ObservedSuccesses: c.ObservedSuccesses, ObservedFailures: c.ObservedFailures, ObservedDays: c.ObservedDays,
		CheckedAt: conv.PtrEmpty(c.CheckedAt), RegistryEntryID: conv.PtrEmpty(c.RegistryEntryID), DocsStatus: conv.PtrEmpty(c.DocsStatus), DocsURL: conv.PtrEmpty(c.DocsURL), DeliveryError: conv.PtrEmpty(c.DeliveryError), DocsRevision: c.DocsRevision, DocsRetryAllowed: c.DocsRetryAllowed, DocsAttempts: c.DocsAttempts,
		DocsLastAttemptAt: conv.PtrEmpty(c.DocsLastAttemptAt), DocsNextAttemptAt: conv.PtrEmpty(c.DocsNextAttemptAt), DocsQueuedAt: conv.PtrEmpty(c.DocsQueuedAt), PromotedAt: conv.PtrEmpty(c.PromotedAt), PublicTitle: conv.PtrEmpty(c.PublicTitle)}
}

func promotionReviewView(review *mcpapproval.StaffReview) *gen.AdminRegistryPromotionReview {
	if review == nil {
		return nil
	}
	view := &gen.AdminRegistryPromotionReview{RequestID: review.RequestID, Evidence: nil, EvidenceCollectedAt: conv.PtrEmpty(review.EvidenceCollectedAt), Research: nil}
	if len(review.Evidence) > 0 {
		view.Evidence = review.Evidence
	}
	if run := review.Research; run != nil {
		view.Research = &gen.AdminRegistryPromotionResearch{Status: run.Status, Report: nil, Error: conv.PtrEmpty(run.Error), StartedAt: conv.PtrEmpty(run.StartedAt), CompletedAt: conv.PtrEmpty(run.CompletedAt)}
		if len(run.Report) > 0 {
			view.Research.Report = run.Report
		}
	}
	return view
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
	case errors.Is(err, catalogpromotion.ErrReviewNotAllowed):
		return oops.E(oops.CodeBadRequest, nil, "%s", catalogpromotion.ErrReviewNotAllowed.Error())
	case errors.Is(err, catalogpromotion.ErrReviewDisabled):
		return oops.E(oops.CodeForbidden, nil, "%s", catalogpromotion.ErrReviewDisabled.Error())
	case errors.Is(err, catalogpromotion.ErrNotFound):
		return oops.C(oops.CodeNotFound)
	case errors.Is(err, catalogpromotion.ErrStale), errors.Is(err, catalogpromotion.ErrConflict):
		return oops.E(oops.CodeConflict, nil, "promotion source changed or was already promoted; refresh before retrying")
	case errors.Is(err, catalogpromotion.ErrPublicTitleTooLong):
		return oops.E(oops.CodeBadRequest, nil, "%s", catalogpromotion.ErrPublicTitleTooLong.Error())
	case errors.Is(err, catalogpromotion.ErrPublicDescriptionTooLong):
		return oops.E(oops.CodeBadRequest, nil, "%s", catalogpromotion.ErrPublicDescriptionTooLong.Error())
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
	rows, next, err := s.catalogPromotion.ListCandidates(ctx, conv.PtrValOr(p.Cursor, ""), p.Limit, p.Status)
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
	result, err := s.catalogPromotion.InspectCandidate(ctx, target)
	if err != nil {
		return nil, promotionError(err)
	}
	return promotionCandidateView(result), nil
}

func (s *Service) GetRegistryPromotionReview(ctx context.Context, p *gen.GetRegistryPromotionReviewPayload) (*gen.AdminRegistryPromotionReviewResult, error) {
	if s.catalogPromotion == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	target, err := promotionTarget(p.OrganizationID, p.ProjectID, p.McpServerID)
	if err != nil {
		return nil, err
	}
	review, err := s.catalogPromotion.GetReview(ctx, target)
	if err != nil {
		return nil, promotionError(err)
	}
	return &gen.AdminRegistryPromotionReviewResult{Review: promotionReviewView(review)}, nil
}

func (s *Service) StartRegistryPromotionReview(ctx context.Context, p *gen.StartRegistryPromotionReviewPayload) (*gen.AdminRegistryPromotionReviewResult, error) {
	if s.catalogPromotion == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	target, err := promotionTarget(p.OrganizationID, p.ProjectID, p.McpServerID)
	if err != nil {
		return nil, err
	}
	actor, _, operatorEmail := adminActor(ctx)
	review, err := s.catalogPromotion.StartReview(ctx, target, actor.ID, operatorEmail)
	if err != nil {
		return nil, promotionError(err)
	}
	return &gen.AdminRegistryPromotionReviewResult{Review: promotionReviewView(review)}, nil
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
	request, err := s.catalogPromotion.PreparePublication(ctx, catalogpromotion.Request{Target: target, SourceRevision: p.SourceRevision, CurationRevision: p.CurationRevision,
		Public:        catalogpromotion.PublicMetadata{Name: p.PublicName, Title: p.PublicTitle, Description: p.PublicDescription, URL: p.PublicURL, DocumentationURL: p.DocumentationURL, DocsSlug: p.DocsSlug, AuthMode: p.AuthMode},
		AuthDiscovery: catalogauth.Discovery{SupportsDCR: false, SupportsCIMD: false, AuthSetup: "", Status: ""}, PublicMetadataConfirmed: p.PublicMetadataConfirmed, OverrideEligibility: p.OverrideEligibility, RetryKey: p.RetryKey})
	if err != nil {
		return nil, promotionError(err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, promotionError(err)
	}
	defer o11y.NoLogDefer(func() error {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return tx.Rollback(cleanupCtx)
	})
	result, err := s.catalogPromotion.PromoteTx(ctx, tx, request, actor.String())
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

func (s *Service) CurateRegistryPromotionCandidate(ctx context.Context, p *gen.CurateRegistryPromotionCandidatePayload) (*gen.AdminRegistryPromotionCandidate, error) {
	if s.catalogPromotion == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	id, err := uuid.Parse(p.PromotionID)
	if err != nil || id == uuid.Nil || p.OrganizationID == "" {
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
	result, err := s.catalogPromotion.CurateTx(ctx, tx, catalogpromotion.CurationRequest{OrganizationID: p.OrganizationID, PromotionID: id, CurationRevision: p.CurationRevision, Action: p.Action, RetryKey: p.RetryKey}, actor.String())
	if err != nil {
		return nil, promotionError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, promotionError(err)
	}
	return promotionCandidateView(result), nil
}
