package usage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func (s *Service) GetBillingEmail(ctx context.Context, _ *gen.GetBillingEmailPayload) (*gen.BillingEmail, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requirePaygOrganization(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	metadata, err := repo.New(s.db).GetBillingMetadata(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return &gen.BillingEmail{Email: nil}, nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get billing email").LogError(ctx, s.logger)
	default:
		return &gen.BillingEmail{Email: conv.FromPGText[string](metadata.AlertEmail)}, nil
	}
}

func (s *Service) SetBillingEmail(ctx context.Context, payload *gen.SetBillingEmailPayload) (*gen.BillingEmail, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requirePaygOrganization(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set billing email").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	if err := queries.LockBillingMetadataOrganization(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock billing metadata organization").LogError(ctx, s.logger)
	}
	var snapshotBefore *audit.BillingMetadataSnapshot
	before, err := queries.LockBillingMetadata(ctx, authCtx.ActiveOrganizationID)
	switch {
	case err == nil:
		snapshotBefore = billingMetadataSnapshot(before)
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeUnexpected, err, "lock billing metadata").LogError(ctx, s.logger)
	}

	row, err := queries.UpsertBillingEmail(ctx, repo.UpsertBillingEmailParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		AlertEmail:     conv.PtrToPGTextTrimmed(payload.Email),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set billing email").LogError(ctx, s.logger)
	}

	if err := s.auditLogger.LogBillingMetadataUpdate(ctx, dbtx, audit.LogBillingMetadataUpdateEvent{
		OrganizationID:                authCtx.ActiveOrganizationID,
		Actor:                         urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:              authCtx.Email,
		ActorSlug:                     nil,
		BillingMetadataURN:            urn.NewBillingMetadata(row.ID),
		BillingMetadataSnapshotBefore: snapshotBefore,
		BillingMetadataSnapshotAfter:  billingMetadataSnapshot(row),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record billing email audit event").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set billing email").LogError(ctx, s.logger)
	}

	return &gen.BillingEmail{Email: conv.FromPGText[string](row.AlertEmail)}, nil
}

func (s *Service) requirePaygOrganization(ctx context.Context, organizationID string) error {
	organization, err := s.orgRepo.GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get organization billing type").LogError(ctx, s.logger)
	}
	if organization.GramAccountType != string(billing.TierPayg) {
		return oops.E(oops.CodeForbidden, nil, "PAYG organization required")
	}
	return nil
}
