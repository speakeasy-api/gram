package usage

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type openRouterKeyRefreshScheduler interface {
	SetOpenRouterSpendCap(context.Context, string, string, int, urn.Principal, *string) error
}

func (s *Service) SetSpendCap(ctx context.Context, payload *gen.SetSpendCapPayload) (*gen.SpendCap, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}
	if payload.MonthlyCredits < constants.MinimumPaygSpendCapUSD || payload.MonthlyCredits > constants.MaximumPaygSpendCapUSD {
		return nil, oops.E(oops.CodeInvalid, nil, "monthly_credits must be between %d and %d", constants.MinimumPaygSpendCapUSD, constants.MaximumPaygSpendCapUSD).LogWarn(ctx, s.logger)
	}

	_, err := trialsrepo.New(s.db).GetActiveTrial(ctx, authCtx.ActiveOrganizationID)
	switch {
	case err == nil:
		return nil, oops.E(oops.CodeConflict, nil, "the chat spend cap cannot be changed during an active trial")
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeUnexpected, err, "check active trial before setting chat spend cap").LogError(ctx, s.logger)
	}
	if err := s.requirePaygOrganization(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}
	_, subscription, err := s.getStripeBillingState(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	switch subscription.Status {
	case "trialing":
		return nil, oops.E(oops.CodeConflict, nil, "the chat spend cap cannot be changed before pay as you go billing starts")
	case "active", "past_due":
		// Both states represent a live paid subscription. A past-due invoice is
		// recoverable through Stripe retries and must not prevent an admin from
		// changing the cap while service remains active.
	default:
		return nil, oops.E(oops.CodeConflict, nil, "the chat spend cap cannot be changed without an active pay as you go subscription")
	}

	requestedLimit := payload.MonthlyCredits
	if err := s.keyRefresher.SetOpenRouterSpendCap(
		ctx,
		uuid.NewString(),
		authCtx.ActiveOrganizationID,
		requestedLimit,
		urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		authCtx.Email,
	); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set chat spend cap").LogError(ctx, s.logger)
	}

	return &gen.SpendCap{MonthlyCredits: requestedLimit}, nil
}
