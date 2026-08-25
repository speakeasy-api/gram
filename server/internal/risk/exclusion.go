package risk

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/exclusioncore"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// parseExclusionPolicyID converts the optional risk_policy_id payload field to
// a NullUUID. A nil/empty value means a global exclusion.
func parseExclusionPolicyID(raw *string) (uuid.NullUUID, error) {
	if raw == nil || *raw == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, oops.E(oops.CodeInvalid, err, "invalid risk_policy_id")
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// nullableText maps an optional filter string to a nullable column value:
// empty string ("any") becomes SQL NULL.
func nullableText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

// validateExclusionMatchValue retains the existing Goa/suggestion error shape
// while sharing validation with transport-neutral exclusion administration.
func validateExclusionMatchValue(matchType, matchValue string) error {
	if err := exclusioncore.ValidateMatchValue(matchType, matchValue); err != nil {
		var validation *exclusioncore.ValidationError
		if errors.As(err, &validation) {
			return oops.E(oops.CodeInvalid, validation.Cause, "%s", validation.Message)
		}
		return oops.E(oops.CodeInvalid, err, "%s", err)
	}
	return nil
}

func (s *Service) ListRiskExclusions(ctx context.Context, payload *gen.ListRiskExclusionsPayload) (*gen.ListRiskExclusionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	policyID, err := parseExclusionPolicyID(payload.RiskPolicyID)
	if err != nil {
		return nil, err
	}
	exclusions, err := s.exclusions.List(ctx, *authCtx.ProjectID, policyID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk exclusions").LogError(ctx, s.logger)
	}
	result := make([]*types.RiskExclusion, 0, len(exclusions))
	for _, exclusion := range exclusions {
		result = append(result, exclusionToType(exclusion))
	}
	return &gen.ListRiskExclusionsResult{Exclusions: result}, nil
}

func (s *Service) CreateRiskExclusion(ctx context.Context, payload *gen.CreateRiskExclusionPayload) (*types.RiskExclusion, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	policyID, err := parseExclusionPolicyID(payload.RiskPolicyID)
	if err != nil {
		return nil, err
	}

	exclusion, err := s.exclusions.Create(ctx, exclusioncore.CreateMutation{
		Params: repo.CreateRiskExclusionParams{
			ProjectID:      *authCtx.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
			RiskPolicyID:   policyID,
			MatchType:      payload.MatchType,
			MatchValue:     payload.MatchValue,
			RuleIDFilter:   nullableText(payload.RuleIDFilter),
			SourceFilter:   nullableText(payload.SourceFilter),
			Enabled:        payload.Enabled,
		},
		Actor: exclusioncore.Actor{
			Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			DisplayName: authCtx.Email,
			Slug:        nil,
		},
	})
	if err != nil {
		return nil, s.exclusionError(ctx, err)
	}
	return exclusionToType(exclusion), nil
}

func (s *Service) UpdateRiskExclusion(ctx context.Context, payload *gen.UpdateRiskExclusionPayload) (*types.RiskExclusion, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid exclusion id")
	}
	policyID, err := parseExclusionPolicyID(payload.RiskPolicyID)
	if err != nil {
		return nil, err
	}

	exclusion, err := s.exclusions.Update(ctx, exclusioncore.UpdateMutation{
		ID:           id,
		ProjectID:    *authCtx.ProjectID,
		RiskPolicyID: policyID,
		MatchType:    payload.MatchType,
		MatchValue:   payload.MatchValue,
		RuleIDFilter: nullableText(payload.RuleIDFilter),
		SourceFilter: nullableText(payload.SourceFilter),
		Enabled:      payload.Enabled,
		Actor: exclusioncore.Actor{
			Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			DisplayName: authCtx.Email,
			Slug:        nil,
		},
	})
	if err != nil {
		return nil, s.exclusionError(ctx, err)
	}
	return exclusionToType(exclusion), nil
}

func (s *Service) DeleteRiskExclusion(ctx context.Context, payload *gen.DeleteRiskExclusionPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid exclusion id")
	}
	if err := s.exclusions.Delete(ctx, exclusioncore.DeleteMutation{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
		Actor: exclusioncore.Actor{
			Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			DisplayName: authCtx.Email,
			Slug:        nil,
		},
	}); err != nil {
		return s.exclusionError(ctx, err)
	}
	return nil
}
