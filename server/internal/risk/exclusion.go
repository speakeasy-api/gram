package risk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	exclusionRegexMaxLength      = 512
	exclusionMaxRegexPerScope    = 50
	exclusionDisplayNameMaxRunes = 80
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
func nullableText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}

func validateExclusionMatchValue(matchType, matchValue string) error {
	if matchValue == "" {
		return oops.E(oops.CodeInvalid, nil, "match_value must not be empty")
	}
	if matchType == "regex" {
		if len(matchValue) > exclusionRegexMaxLength {
			return oops.E(oops.CodeInvalid, nil, "regex pattern too long (max %d characters)", exclusionRegexMaxLength)
		}
		if _, err := regexp.Compile(matchValue); err != nil {
			return oops.E(oops.CodeInvalid, err, "invalid regex pattern")
		}
	}
	return nil
}

// redactExclusionValue replaces a raw exclusion pattern/filter with a stable,
// non-reversible fingerprint. An exact match_value can be the literal sensitive
// string the author wants suppressed (an email, a secret), so it must never
// reach the audit log or the outbound webhook payload verbatim. The fingerprint
// still lets update snapshots show that a value changed without revealing it.
func redactExclusionValue(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "redacted:sha256:" + hex.EncodeToString(sum[:])[:12]
}

func exclusionDisplayName(matchType, matchValue string) string {
	name := matchType + ":" + redactExclusionValue(matchValue)
	if len([]rune(name)) > exclusionDisplayNameMaxRunes {
		return string([]rune(name)[:exclusionDisplayNameMaxRunes])
	}
	return name
}

func exclusionToType(row repo.RiskExclusion) *types.RiskExclusion {
	var policyID *string
	if row.RiskPolicyID.Valid {
		s := row.RiskPolicyID.UUID.String()
		policyID = &s
	}
	return &types.RiskExclusion{
		ID:           row.ID.String(),
		ProjectID:    row.ProjectID.String(),
		RiskPolicyID: policyID,
		MatchType:    row.MatchType,
		MatchValue:   row.MatchValue,
		RuleIDFilter: row.RuleIDFilter.String,
		SourceFilter: row.SourceFilter.String,
		Enabled:      row.Enabled,
		CreatedAt:    row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:    row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// exclusionAuditSnapshot is exclusionToType with the sensitive pattern/filter
// fields redacted, for use in audit before/after snapshots that are emitted to
// webhook consumers.
func exclusionAuditSnapshot(row repo.RiskExclusion) *types.RiskExclusion {
	t := exclusionToType(row)
	t.MatchValue = redactExclusionValue(t.MatchValue)
	t.RuleIDFilter = redactExclusionValue(t.RuleIDFilter)
	t.SourceFilter = redactExclusionValue(t.SourceFilter)
	return t
}

// reconcileExclusion triggers the retroactive sweep best-effort. A failed
// trigger is logged, not fatal: the exclusion config is already committed and
// the reconcile is idempotent, so a later trigger (or manual re-run) converges.
func (s *Service) reconcileExclusion(ctx context.Context, projectID, exclusionID uuid.UUID) {
	if s.reconciler == nil {
		return
	}
	if err := s.reconciler.Reconcile(ctx, projectID, exclusionID); err != nil {
		s.logger.ErrorContext(ctx, "trigger risk exclusion reconcile",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
		)
	}
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

	rows, err := s.repo.ListRiskExclusionsByProject(ctx, repo.ListRiskExclusionsByProjectParams{
		ProjectID:    *authCtx.ProjectID,
		RiskPolicyID: policyID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk exclusions").LogError(ctx, s.logger)
	}

	exclusions := make([]*types.RiskExclusion, 0, len(rows))
	for _, row := range rows {
		exclusions = append(exclusions, exclusionToType(row))
	}
	return &gen.ListRiskExclusionsResult{Exclusions: exclusions}, nil
}

func (s *Service) CreateRiskExclusion(ctx context.Context, payload *gen.CreateRiskExclusionPayload) (*types.RiskExclusion, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if err := validateExclusionMatchValue(payload.MatchType, payload.MatchValue); err != nil {
		return nil, err
	}

	policyID, err := parseExclusionPolicyID(payload.RiskPolicyID)
	if err != nil {
		return nil, err
	}

	// Confirm the parent policy exists in this project (policy-bound only).
	if policyID.Valid {
		if _, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{ID: policyID.UUID, ProjectID: *authCtx.ProjectID}); err != nil {
			return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
		}
	}

	// Enforce the per-scope regex cap. The cap only bounds *enabled* regex
	// exclusions (they drive matching load), so a disabled regex draft is
	// always allowed even when the scope is already at the limit.
	if payload.MatchType == "regex" && payload.Enabled {
		count, err := s.repo.CountEnabledRegexExclusionsInScope(ctx, repo.CountEnabledRegexExclusionsInScopeParams{
			ProjectID:    *authCtx.ProjectID,
			RiskPolicyID: policyID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "count regex exclusions").LogError(ctx, s.logger)
		}
		if count >= exclusionMaxRegexPerScope {
			return nil, oops.E(oops.CodeInvalid, nil, "too many regex exclusions in scope (max %d)", exclusionMaxRegexPerScope)
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateRiskExclusion(ctx, repo.CreateRiskExclusionParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		RiskPolicyID:   policyID,
		MatchType:      payload.MatchType,
		MatchValue:     payload.MatchValue,
		RuleIDFilter:   nullableText(payload.RuleIDFilter),
		SourceFilter:   nullableText(payload.SourceFilter),
		Enabled:        payload.Enabled,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create risk exclusion").LogError(ctx, s.logger)
	}

	if err := s.audit.LogRiskExclusionCreate(ctx, dbtx, audit.LogRiskExclusionCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskExclusionID:  row.ID,
		DisplayName:      exclusionDisplayName(row.MatchType, row.MatchValue),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk exclusion create").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk exclusion create").LogError(ctx, s.logger)
	}

	s.reconcileExclusion(ctx, row.ProjectID, row.ID)

	return exclusionToType(row), nil
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
	if err := validateExclusionMatchValue(payload.MatchType, payload.MatchValue); err != nil {
		return nil, err
	}
	policyID, err := parseExclusionPolicyID(payload.RiskPolicyID)
	if err != nil {
		return nil, err
	}
	if policyID.Valid {
		if _, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{ID: policyID.UUID, ProjectID: *authCtx.ProjectID}); err != nil {
			return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
		}
	}

	before, err := s.repo.GetRiskExclusion(ctx, repo.GetRiskExclusionParams{ID: id, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk exclusion not found").LogError(ctx, s.logger)
	}

	// An omitted `enabled` leaves the current state untouched rather than
	// silently re-enabling a disabled exclusion.
	enabled := before.Enabled
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	// Enforce the per-scope regex cap when this update moves the exclusion into
	// the enabled-regex set of a scope it wasn't already counted in (newly
	// enabling, switching match_type to regex, or moving to another scope).
	if payload.MatchType == "regex" && enabled {
		wasCountedInScope := before.MatchType == "regex" && before.Enabled &&
			before.RiskPolicyID == policyID
		if !wasCountedInScope {
			count, err := s.repo.CountEnabledRegexExclusionsInScope(ctx, repo.CountEnabledRegexExclusionsInScopeParams{
				ProjectID:    *authCtx.ProjectID,
				RiskPolicyID: policyID,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "count regex exclusions").LogError(ctx, s.logger)
			}
			if count >= exclusionMaxRegexPerScope {
				return nil, oops.E(oops.CodeInvalid, nil, "too many regex exclusions in scope (max %d)", exclusionMaxRegexPerScope)
			}
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).UpdateRiskExclusion(ctx, repo.UpdateRiskExclusionParams{
		ID:           id,
		ProjectID:    *authCtx.ProjectID,
		RiskPolicyID: policyID,
		MatchType:    payload.MatchType,
		MatchValue:   payload.MatchValue,
		RuleIDFilter: nullableText(payload.RuleIDFilter),
		SourceFilter: nullableText(payload.SourceFilter),
		Enabled:      enabled,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update risk exclusion").LogError(ctx, s.logger)
	}

	if err := s.audit.LogRiskExclusionUpdate(ctx, dbtx, audit.LogRiskExclusionUpdateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskExclusionID:  row.ID,
		DisplayName:      exclusionDisplayName(row.MatchType, row.MatchValue),
		SnapshotBefore:   exclusionAuditSnapshot(before),
		SnapshotAfter:    exclusionAuditSnapshot(row),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk exclusion update").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk exclusion update").LogError(ctx, s.logger)
	}

	s.reconcileExclusion(ctx, row.ProjectID, row.ID)

	return exclusionToType(row), nil
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

	before, err := s.repo.GetRiskExclusion(ctx, repo.GetRiskExclusionParams{ID: id, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "risk exclusion not found").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := repo.New(dbtx).DeleteRiskExclusion(ctx, repo.DeleteRiskExclusionParams{ID: id, ProjectID: *authCtx.ProjectID}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete risk exclusion").LogError(ctx, s.logger)
	}

	if err := s.audit.LogRiskExclusionDelete(ctx, dbtx, audit.LogRiskExclusionDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskExclusionID:  before.ID,
		DisplayName:      exclusionDisplayName(before.MatchType, before.MatchValue),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log risk exclusion delete").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit risk exclusion delete").LogError(ctx, s.logger)
	}

	// Restore findings previously suppressed by this exclusion.
	s.reconcileExclusion(ctx, *authCtx.ProjectID, id)

	return nil
}
