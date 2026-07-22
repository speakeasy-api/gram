package risk_analysis

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// BackfillDetectionScopes is the one-shot migration of the legacy policy-wide
// scoping fields (message_types, scope_include, scope_exempt) into per-category
// detection scopes. For each affected policy it composes the legacy scope with
// every relevant category's effective scope (specified if present, else the
// registry recommendation), writes the result into
// analyzer_config.detection_scopes, and clears the legacy columns. Effective
// scan behavior is unchanged, so the policy version is not bumped.
type BackfillDetectionScopes struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
	celEng *celenv.Engine
}

func NewBackfillDetectionScopes(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, celEng *celenv.Engine) *BackfillDetectionScopes {
	return &BackfillDetectionScopes{
		logger: logger.With(attr.SlogComponent("risk-detection-scope-backfill")),
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		db:     db,
		celEng: celEng,
	}
}

type BackfillDetectionScopesResult struct {
	Migrated int
	Skipped  int
}

func (a *BackfillDetectionScopes) Do(ctx context.Context) (_ *BackfillDetectionScopesResult, err error) {
	ctx, span := a.tracer.Start(ctx, "risk.backfillDetectionScopes")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	queries := repo.New(a.db)
	rows, err := queries.ListRiskPoliciesWithLegacyScope(ctx)
	if err != nil {
		return nil, fmt.Errorf("list policies with legacy scope: %w", err)
	}

	result := &BackfillDetectionScopesResult{Migrated: 0, Skipped: 0}
	for _, row := range rows {
		config, err := composeLegacyScope(a.celEng, row)
		if err != nil {
			// Leave the policy untouched: its legacy columns keep enforcing under
			// old code, and a human inspects it before the columns are dropped.
			a.logger.ErrorContext(ctx, "compose legacy scope failed; policy skipped",
				attr.SlogError(err),
				attr.SlogRiskPolicyID(row.ID.String()),
			)
			result.Skipped++
			continue
		}
		if err := queries.MigrateRiskPolicyLegacyScope(ctx, repo.MigrateRiskPolicyLegacyScopeParams{
			AnalyzerConfig: config,
			ID:             row.ID,
			ProjectID:      row.ProjectID,
		}); err != nil {
			return nil, fmt.Errorf("write migrated detection scopes for policy %s: %w", row.ID, err)
		}
		result.Migrated++
	}

	span.SetAttributes(
		attribute.Int("risk.policies_migrated", result.Migrated),
		attribute.Int("risk.policies_skipped", result.Skipped),
	)
	return result, nil
}

// composeLegacyScope folds a policy's legacy message_types + scope predicates
// into its per-category detection scopes: include predicates intersect, exempt
// predicates union, and a policy-specified scope stays the composition base
// over the registry recommendation. Only categories the policy can emit are
// written so unrelated categories keep tracking the registry.
func composeLegacyScope(eng *celenv.Engine, row repo.RiskPolicy) ([]byte, error) {
	legacyInclude := IntersectScopeExprs(strings.TrimSpace(row.ScopeInclude.String), messageTypesExpr(row.MessageTypes))
	legacyExempt := strings.TrimSpace(row.ScopeExempt.String)

	specified := map[categories.Category]DetectionScopeConfig{}
	order := []categories.Category{}
	for _, spec := range DetectionScopesFromConfig(row.AnalyzerConfig) {
		cat := categories.Category(spec.Category)
		specified[cat] = spec
		order = append(order, cat)
	}
	for _, cat := range policyCategories(row) {
		if _, ok := specified[cat]; !ok {
			order = append(order, cat)
		}
	}

	out := make([]DetectionScopeConfig, 0, len(order))
	for _, cat := range order {
		base, hasBase := specified[cat]
		if !hasBase {
			if rec, ok := recommendedscopes.For(cat); ok && rec.Applicable {
				base = DetectionScopeConfig{Category: string(cat), ScopeInclude: rec.ScopeInclude, ScopeExempt: rec.ScopeExempt}
			} else {
				// Session-scoped categories are never message-scoped; the legacy
				// message filters did not apply to them either.
				continue
			}
		}
		composed := DetectionScopeConfig{
			Category:     string(cat),
			ScopeInclude: IntersectScopeExprs(base.ScopeInclude, legacyInclude),
			ScopeExempt:  UnionScopeExprs(base.ScopeExempt, legacyExempt),
		}
		if _, err := CompileScope(eng, composed.ScopeInclude, composed.ScopeExempt); err != nil {
			return nil, fmt.Errorf("composed scope for %s does not compile: %w", cat, err)
		}
		out = append(out, composed)
	}

	config, err := WithDetectionScopes(row.AnalyzerConfig, out)
	if err != nil {
		return nil, fmt.Errorf("write detection scopes into analyzer config: %w", err)
	}
	return config, nil
}

// policyCategories returns the applicable categories a policy can emit
// findings for: those of its detection sources, custom when custom rules are
// attached, and prompt_policy for prompt-based policies.
func policyCategories(row repo.RiskPolicy) []categories.Category {
	seen := map[categories.Category]bool{}
	out := []categories.Category{}
	add := func(cats ...categories.Category) {
		for _, cat := range cats {
			if !seen[cat] {
				seen[cat] = true
				out = append(out, cat)
			}
		}
	}
	for _, source := range row.Sources {
		add(sourceCategories[source]...)
	}
	if len(row.CustomRuleIds) > 0 {
		add(categories.CategoryCustom)
	}
	if row.PolicyType == PolicyTypePromptBased {
		add(categories.CategoryPromptPolicy)
	}
	return out
}

// messageTypesExpr renders a legacy message_types selection as the equivalent
// CEL surface predicate. Empty (= all types) renders as no restriction.
func messageTypesExpr(messageTypes []string) string {
	if len(messageTypes) == 0 {
		return ""
	}
	terms := make([]string, 0, len(messageTypes))
	for _, t := range messageTypes {
		terms = append(terms, fmt.Sprintf("kind == %q", t))
	}
	return strings.Join(terms, " || ")
}

// IntersectScopeExprs conjoins two CEL include predicates; either being empty
// (no restriction) yields the other.
func IntersectScopeExprs(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return fmt.Sprintf("(%s) && (%s)", a, b)
	}
}

// UnionScopeExprs disjoins two CEL exempt predicates; either being empty (no
// exemption) yields the other.
func UnionScopeExprs(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return fmt.Sprintf("(%s) || (%s)", a, b)
	}
}
