package usage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func (s *Service) GetTokensUnderManagement(ctx context.Context, payload *gen.GetTokensUnderManagementPayload) (*gen.TokensUnderManagement, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	meta, err := s.repo.GetBillingMetadata(ctx, authCtx.ActiveOrganizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get billing metadata").Log(ctx, s.logger)
	}
	// On pgx.ErrNoRows the zero-value row stands in for an unconfigured
	// contract: no token limit, no alert email, and an anchor day that
	// CurrentBillingCycle defaults to 1.

	return s.buildTokensUnderManagement(ctx, authCtx, meta)
}

func (s *Service) SetBillingMetadata(ctx context.Context, payload *gen.SetBillingMetadataPayload) (*gen.TokensUnderManagement, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if !authCtx.IsAdmin {
		return nil, oops.E(oops.CodeForbidden, nil, "platform admin required").Log(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	tokenLimit := pgtype.Int8{Int64: 0, Valid: false}
	if payload.MonthlyTokenLimit != nil {
		tokenLimit = pgtype.Int8{Int64: *payload.MonthlyTokenLimit, Valid: true}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update billing metadata").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	qtx := repo.New(dbtx)

	var snapshotBefore *audit.BillingMetadataSnapshot
	before, err := qtx.GetBillingMetadata(ctx, authCtx.ActiveOrganizationID)
	switch {
	case err == nil:
		snapshotBefore = billingMetadataSnapshot(before)
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get billing metadata").Log(ctx, s.logger)
	}

	row, err := qtx.UpsertBillingMetadata(ctx, repo.UpsertBillingMetadataParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		TumMonthlyTokenLimit:  tokenLimit,
		AlertEmail:            conv.PtrToPGText(payload.AlertEmail),
		BillingCycleAnchorDay: conv.SafeInt32(payload.BillingCycleAnchorDay),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to upsert billing metadata").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to record billing metadata audit event").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update billing metadata").Log(ctx, s.logger)
	}

	return s.buildTokensUnderManagement(ctx, authCtx, row)
}

// buildTokensUnderManagement computes TUM consumption for the active billing
// cycle and combines it with the organization's contract terms. A zero-value
// meta row represents an unconfigured contract.
func (s *Service) buildTokensUnderManagement(ctx context.Context, authCtx *contextvalues.AuthContext, meta repo.BillingMetadatum) (*gen.TokensUnderManagement, error) {
	start, end := CurrentBillingCycle(time.Now(), int(meta.BillingCycleAnchorDay))

	projectIDs, err := s.repo.ListProjectIDsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list organization projects").Log(ctx, s.logger)
	}

	ids := make([]string, 0, len(projectIDs))
	for _, id := range projectIDs {
		ids = append(ids, id.String())
	}

	tokens, err := s.telemetryRepo.GetTokensUnderManagement(ctx, telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:    ids,
		StartUnixNano: start.UnixNano(),
		EndUnixNano:   end.UnixNano(),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to compute tokens under management").Log(ctx, s.logger)
	}

	var tokenLimit *int64
	if meta.TumMonthlyTokenLimit.Valid {
		tokenLimit = &meta.TumMonthlyTokenLimit.Int64
	}

	// The alert email is internal contract configuration; only expose it to
	// platform admins.
	var alertEmail *string
	if authCtx.IsAdmin {
		alertEmail = conv.FromPGText[string](meta.AlertEmail)
	}

	return &gen.TokensUnderManagement{
		PeriodStart:           start.Format(time.RFC3339),
		PeriodEnd:             end.Format(time.RFC3339),
		Tokens:                tokens,
		MonthlyTokenLimit:     tokenLimit,
		BillingCycleAnchorDay: int(max(meta.BillingCycleAnchorDay, 1)),
		AlertEmail:            alertEmail,
	}, nil
}

// billingMetadataSnapshot converts a billing metadata row into its audit
// snapshot form.
func billingMetadataSnapshot(row repo.BillingMetadatum) *audit.BillingMetadataSnapshot {
	var tokenLimit *int64
	if row.TumMonthlyTokenLimit.Valid {
		tokenLimit = &row.TumMonthlyTokenLimit.Int64
	}

	return &audit.BillingMetadataSnapshot{
		TumMonthlyTokenLimit:  tokenLimit,
		AlertEmail:            conv.FromPGText[string](row.AlertEmail),
		BillingCycleAnchorDay: int(row.BillingCycleAnchorDay),
	}
}
