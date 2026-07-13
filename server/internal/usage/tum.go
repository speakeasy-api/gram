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
	"github.com/speakeasy-api/gram/server/internal/billing"
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get billing metadata").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeForbidden, nil, "platform admin required").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	tokenLimit := pgtype.Int8{Int64: 0, Valid: false}
	if payload.MonthlyTokenLimit != nil {
		tokenLimit = pgtype.Int8{Int64: *payload.MonthlyTokenLimit, Valid: true}
	}
	tunneledMcpServerLimit := pgtype.Int4{Int32: 0, Valid: false}
	if payload.TunneledMcpServerLimit != nil {
		if *payload.TunneledMcpServerLimit < 0 {
			return nil, oops.E(oops.CodeInvalid, nil, "tunneled_mcp_server_limit must be at least 0").LogWarn(ctx, s.logger)
		}
		if *payload.TunneledMcpServerLimit > maxTunneledMcpServerLimit {
			return nil, oops.E(oops.CodeInvalid, nil, "tunneled_mcp_server_limit must be at most %d", maxTunneledMcpServerLimit).LogWarn(ctx, s.logger)
		}
		tunneledMcpServerLimit = pgtype.Int4{Int32: int32(*payload.TunneledMcpServerLimit), Valid: true}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update billing metadata").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	qtx := repo.New(dbtx)

	var snapshotBefore *audit.BillingMetadataSnapshot
	before, err := qtx.GetBillingMetadata(ctx, authCtx.ActiveOrganizationID)
	switch {
	case err == nil:
		snapshotBefore = billingMetadataSnapshot(before)
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get billing metadata").LogError(ctx, s.logger)
	}

	row, err := qtx.UpsertBillingMetadata(ctx, repo.UpsertBillingMetadataParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		TumMonthlyTokenLimit:   tokenLimit,
		AlertEmail:             conv.PtrToPGText(payload.AlertEmail),
		BillingCycleAnchorDay:  conv.SafeInt32(payload.BillingCycleAnchorDay),
		TunneledMcpServerLimit: tunneledMcpServerLimit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to upsert billing metadata").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to record billing metadata audit event").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update billing metadata").LogError(ctx, s.logger)
	}

	return s.buildTokensUnderManagement(ctx, authCtx, row)
}

// tumHistoryCycles is how many trailing billing cycles (including the active
// one) the TUM endpoint reports. The 2-year retention of
// attribute_metrics_summaries comfortably covers the window, but actual
// COVERAGE starts where the aggregate's data does (its backfill was bounded
// by the raw telemetry TTL) — cycles predating coverage recompute to zero
// unless a finalized snapshot protects them.
const tumHistoryCycles = 12
const maxTunneledMcpServerLimit = 1<<31 - 1

// buildTokensUnderManagement computes TUM consumption per billing cycle for
// the trailing cycles and combines it with the organization's contract terms.
// A zero-value meta row represents an unconfigured contract.
func (s *Service) buildTokensUnderManagement(ctx context.Context, authCtx *contextvalues.AuthContext, meta repo.BillingMetadatum) (*gen.TokensUnderManagement, error) {
	cycles := BillingCycles(time.Now(), int(meta.BillingCycleAnchorDay), tumHistoryCycles)

	projectIDs, err := s.repo.ListBillingProjectIDsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list organization projects").LogError(ctx, s.logger)
	}

	ids := make([]string, 0, len(projectIDs))
	for _, id := range projectIDs {
		ids = append(ids, id.String())
	}

	// Finalized cycle snapshots are the immutable billing record: once a
	// cycle is sealed (billingCycleFinalizeGrace after it closes), its total
	// is served from Postgres instead of being recomputed from ClickHouse, so
	// the reported number always matches what was invoiced — even if the
	// telemetry aggregates change or expire afterwards. Open and
	// not-yet-finalized cycles keep computing live.
	snapshots, err := s.repo.ListBillingCycleUsage(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list billing cycle snapshots").LogError(ctx, s.logger)
	}
	finalized := make(map[int64]repo.BillingCycleUsage, len(snapshots))
	for _, snap := range snapshots {
		if snap.FinalizedAt.Valid {
			finalized[snap.CycleStart.Time.UTC().Unix()] = snap
		}
	}

	// Chat qualification (stored non-metrics evidence) is evaluated per cycle,
	// so each cycle's daily points sum exactly to that cycle's TUM.
	history := make([]*gen.TUMPeriod, 0, len(cycles))
	for _, cycle := range cycles {
		days, err := s.telemetryRepo.GetTokensUnderManagementByDay(ctx, telemetryrepo.GetTokensUnderManagementParams{
			ProjectIDs:          ids,
			StartUnixNano:       cycle.Start.UnixNano(),
			EndUnixNano:         cycle.End.UnixNano(),
			ExcludedHookSources: billing.GramHostedHookSourceStrings(),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to compute tokens under management").LogError(ctx, s.logger)
		}

		var cycleTokens int64
		dayItems := make([]*gen.TUMPeriodDay, 0, len(days))
		for _, day := range days {
			cycleTokens += day.Tokens
			dayItems = append(dayItems, &gen.TUMPeriodDay{
				Date:   day.Day.UTC().Format(time.DateOnly),
				Tokens: day.Tokens,
			})
		}

		// The finalized snapshot wins over the live recompute; the daily
		// points remain advisory (they can drift or expire — the sealed total
		// is the billed number). Matched on BOTH boundaries: if the contract's
		// anchor day changed since a cycle was sealed, a stale-boundary
		// snapshot no longer describes this period, so the cycle recomputes
		// live (the aggregate's retention covers the full window) and the
		// hourly snapshot job re-finalizes it on the new boundaries. The
		// old-boundary rows stay untouched as the invoiced record.
		if snap, ok := finalized[cycle.Start.UTC().Unix()]; ok && snap.CycleEnd.Time.UTC().Equal(cycle.End.UTC()) {
			cycleTokens = snap.TumTokens
		}

		history = append(history, &gen.TUMPeriod{
			PeriodStart: cycle.Start.Format(time.RFC3339),
			PeriodEnd:   cycle.End.Format(time.RFC3339),
			Tokens:      cycleTokens,
			Days:        dayItems,
		})
	}

	current := history[len(history)-1]

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
		PeriodStart:            current.PeriodStart,
		PeriodEnd:              current.PeriodEnd,
		Tokens:                 current.Tokens,
		MonthlyTokenLimit:      tokenLimit,
		TunneledMcpServerLimit: conv.PtrInt32ToInt(conv.FromPGInt4(meta.TunneledMcpServerLimit)),
		BillingCycleAnchorDay:  int(max(meta.BillingCycleAnchorDay, 1)),
		AlertEmail:             alertEmail,
		History:                history,
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
		TumMonthlyTokenLimit:   tokenLimit,
		TunneledMcpServerLimit: conv.PtrInt32ToInt(conv.FromPGInt4(row.TunneledMcpServerLimit)),
		AlertEmail:             conv.FromPGText[string](row.AlertEmail),
		BillingCycleAnchorDay:  int(row.BillingCycleAnchorDay),
	}
}
