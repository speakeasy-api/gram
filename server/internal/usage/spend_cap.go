package usage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type openRouterKeyRefreshScheduler interface {
	SetOpenRouterSpendCap(context.Context, string, string, openrouter.KeyType, int, urn.Principal, *string) error
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
	var keyType openrouter.KeyType
	if payload.KeyType != nil {
		keyType = openrouter.KeyType(*payload.KeyType)
	}
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid inference key type").LogWarn(ctx, s.logger)
	}
	if payload.MonthlyCredits < constants.MinimumPaygSpendCapUSD || payload.MonthlyCredits > constants.MaximumPaygSpendCapUSD {
		return nil, oops.E(oops.CodeInvalid, nil, "monthly_credits must be between %d and %d", constants.MinimumPaygSpendCapUSD, constants.MaximumPaygSpendCapUSD).LogWarn(ctx, s.logger)
	}
	_, err := trialsrepo.New(s.db).GetActiveTrial(ctx, authCtx.ActiveOrganizationID)
	switch {
	case err == nil:
		return nil, oops.E(oops.CodeConflict, nil, "inference caps cannot be changed during an active trial")
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeUnexpected, err, "check active trial before setting inference cap").LogError(ctx, s.logger)
	}
	if err := s.requirePaygOrganization(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}
	key, err := s.repo.GetMaterializedOpenRouterInferenceKey(ctx, repo.GetMaterializedOpenRouterInferenceKeyParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		KeyType:        string(keyType),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "inference key is not available")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load inference key before setting inference cap").LogError(ctx, s.logger)
	}
	if key.Disabled {
		return nil, oops.E(oops.CodeConflict, nil, "the inference key is disabled")
	}

	requestedLimit := payload.MonthlyCredits
	if err := s.keyRefresher.SetOpenRouterSpendCap(
		ctx,
		uuid.NewString(),
		authCtx.ActiveOrganizationID,
		keyType,
		requestedLimit,
		urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		authCtx.Email,
	); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set inference cap").LogError(ctx, s.logger)
	}

	return &gen.SpendCap{KeyType: string(keyType), MonthlyCredits: requestedLimit}, nil
}

func (s *Service) GetInferenceSpendCaps(ctx context.Context, _ *gen.GetInferenceSpendCapsPayload) ([]*gen.InferenceSpendCap, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgRead,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}

	keyTypes := make([]string, len(openrouter.AllKeyTypes))
	for index, keyType := range openrouter.AllKeyTypes {
		keyTypes[index] = string(keyType)
	}
	keys, err := s.repo.ListMaterializedOpenRouterInferenceKeys(ctx, repo.ListMaterializedOpenRouterInferenceKeysParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		KeyTypes:       keyTypes,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list platform-managed inference keys").LogError(ctx, s.logger)
	}

	result := make([]*gen.InferenceSpendCap, len(keys))
	group, groupCtx := errgroup.WithContext(ctx)
	for index, key := range keys {
		keyType := openrouter.KeyType(key.KeyType)
		if err := keyType.Validate(); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "validate stored inference key type").LogError(ctx, s.logger)
		}
		group.Go(func() error {
			creditsUsed, monthlyCredits, err := s.openRouter.GetCreditsUsed(groupCtx, authCtx.ActiveOrganizationID, keyType)
			if err != nil {
				return fmt.Errorf("read %s inference key usage: %w", keyType, err)
			}
			result[index] = &gen.InferenceSpendCap{
				KeyType:        key.KeyType,
				CreditsUsed:    creditsUsed,
				MonthlyCredits: monthlyCredits,
				Disabled:       key.Disabled,
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read inference key usage").LogError(ctx, s.logger)
	}

	return result, nil
}
