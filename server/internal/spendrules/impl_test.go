package spendrules_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/spend_rules"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	spendrepo "github.com/speakeasy-api/gram/server/internal/spendrules/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func createTestRulePayload() *gen.CreateSpendRulePayload {
	return &gen.CreateSpendRulePayload{
		Name:        "Engineering cap",
		Description: "Per-person budget for engineering",
		Target: &types.SpendRuleTargetCondition{
			Attribute: "department_name",
			Operator:  "equals",
			Value:     "Engineering",
		},
		LimitUsd:   500,
		WindowKind: "monthly",
		WarnAtPct:  80,
		Action:     "flag",
		Enabled:    true,
	}
}

func TestCreateSpendRule_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSpendRuleCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateSpendRule(ctx, createTestRulePayload())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Engineering cap", result.Name)
	require.Equal(t, createTestRulePayload().Target, result.Target)
	require.Equal(t, `department_name == "Engineering"`, result.TargetExpr)
	require.Equal(t, spendrules.DefaultRuleExpr, result.RuleExpr)
	require.InDelta(t, 500.0, result.LimitUsd, 0.001)
	require.Equal(t, "monthly", result.WindowKind)
	require.Equal(t, 80, result.WarnAtPct)
	require.Equal(t, "flag", result.Action)
	require.True(t, result.Enabled)
	require.Equal(t, int64(1), result.Version)

	require.Equal(t, "engineering-cap", result.Slug, "slug derives from the name")
	require.Equal(t, urn.NewSpendRule("engineering-cap", 1).String(), result.Urn)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSpendRuleCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	require.Len(t, ti.signaler.calls, 1)
}

func TestCreateSpendRule_DuplicateNameGetsSuffixedSlug(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	first, err := ti.service.CreateSpendRule(ctx, createTestRulePayload())
	require.NoError(t, err)
	require.Equal(t, "engineering-cap", first.Slug)

	second, err := ti.service.CreateSpendRule(ctx, createTestRulePayload())
	require.NoError(t, err)
	require.NotEqual(t, first.Slug, second.Slug)
	require.True(t, strings.HasPrefix(second.Slug, "engineering-cap-"), "collision slug %q keeps the name prefix", second.Slug)
	require.Equal(t, urn.NewSpendRule(second.Slug, 1).String(), second.Urn)
}

func TestCreateSpendRule_RejectsInvalidExpression(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	payload := createTestRulePayload()
	payload.Target = &types.SpendRuleTargetCondition{
		Attribute: "favorite_color",
		Operator:  "equals",
		Value:     "blue",
	}
	_, err := ti.service.CreateSpendRule(ctx, payload)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)

	payload = createTestRulePayload()
	payload.Target = &types.SpendRuleTargetCondition{
		Attribute: "groups",
		Operator:  "contains",
		Value:     "engineering",
	}
	_, err = ti.service.CreateSpendRule(ctx, payload)
	require.Error(t, err)
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestCreateSpendRule_RejectsEmptyNameAndZeroLimit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	payload := createTestRulePayload()
	payload.Name = ""
	_, err := ti.service.CreateSpendRule(ctx, payload)
	require.Error(t, err)

	payload = createTestRulePayload()
	payload.LimitUsd = 0
	_, err = ti.service.CreateSpendRule(ctx, payload)
	require.Error(t, err)
}

func TestCreateSpendRule_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)

	// Enterprise account with zero grants — RBAC should deny.
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateSpendRule(ctx, createTestRulePayload())
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestListAndGetSpendRules(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	created, err := ti.service.CreateSpendRule(ctx, createTestRulePayload())
	require.NoError(t, err)

	list, err := ti.service.ListSpendRules(ctx, &gen.ListSpendRulesPayload{})
	require.NoError(t, err)
	require.Len(t, list.Rules, 1)
	require.Equal(t, created.ID, list.Rules[0].ID)

	got, err := ti.service.GetSpendRule(ctx, &gen.GetSpendRulePayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, created.Urn, got.Urn)

	_, err = ti.service.GetSpendRule(ctx, &gen.GetSpendRulePayload{ID: uuid.NewString()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestUpdateSpendRule_NonMaterialKeepsVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	created, err := ti.service.CreateSpendRule(ctx, createTestRulePayload())
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSpendRuleUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateSpendRule(ctx, &gen.UpdateSpendRulePayload{
		ID:          created.ID,
		Name:        new("Engineering cap (renamed)"),
		Description: new("Updated description"),
		Enabled:     new(false),
	})
	require.NoError(t, err)
	require.Equal(t, "Engineering cap (renamed)", updated.Name)
	require.False(t, updated.Enabled)
	require.Equal(t, int64(1), updated.Version, "name/description/enabled edits must not bump the version")
	require.Equal(t, created.Urn, updated.Urn)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSpendRuleUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestUpdateSpendRule_MaterialBumpsVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	created, err := ti.service.CreateSpendRule(ctx, createTestRulePayload())
	require.NoError(t, err)

	updated, err := ti.service.UpdateSpendRule(ctx, &gen.UpdateSpendRulePayload{
		ID: created.ID,
		Target: &types.SpendRuleTargetCondition{
			Attribute: "groups",
			Operator:  "includes",
			Value:     "eng-frontier",
		},
	})
	require.NoError(t, err)
	require.Equal(t, `"eng-frontier" in groups`, updated.TargetExpr)
	require.Equal(t, int64(2), updated.Version, "target changes are material")
	require.NotEqual(t, created.Urn, updated.Urn)

	require.Equal(t, created.Slug, updated.Slug, "material edits keep the slug")
	require.Equal(t, urn.NewSpendRule(created.Slug, 2).String(), updated.Urn)
}

func TestUpdateSpendRule_RejectsInvalidExpression(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	created, err := ti.service.CreateSpendRule(ctx, createTestRulePayload())
	require.NoError(t, err)

	_, err = ti.service.UpdateSpendRule(ctx, &gen.UpdateSpendRulePayload{
		ID: created.ID,
		Target: &types.SpendRuleTargetCondition{
			Attribute: "favorite_color",
			Operator:  "equals",
			Value:     "blue",
		},
	})
	require.Error(t, err)

	_, err = ti.service.UpdateSpendRule(ctx, &gen.UpdateSpendRulePayload{
		ID: created.ID,
		Target: &types.SpendRuleTargetCondition{
			Attribute: "email",
			Operator:  "includes",
			Value:     "blue",
		},
	})
	require.Error(t, err)
}

func TestDeleteSpendRule(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	created, err := ti.service.CreateSpendRule(ctx, createTestRulePayload())
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSpendRuleDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteSpendRule(ctx, &gen.DeleteSpendRulePayload{ID: created.ID}))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSpendRuleDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	list, err := ti.service.ListSpendRules(ctx, &gen.ListSpendRulesPayload{})
	require.NoError(t, err)
	require.Empty(t, list.Rules)

	// Re-deleting a tombstoned rule reports not found.
	err = ti.service.DeleteSpendRule(ctx, &gen.DeleteSpendRulePayload{ID: created.ID})
	require.Error(t, err)
}

func TestSpendRuleEventsListAndDedupe(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	created, err := ti.service.CreateSpendRule(ctx, createTestRulePayload())
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ruleID := uuid.MustParse(created.ID)
	ruleURN := urn.NewSpendRule(created.Slug, 1).String()
	windowStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	params := spendrepo.InsertSpendRuleEventParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		SpendRuleID:    ruleID,
		RuleVersion:    1,
		RuleUrn:        ruleURN,
		EventType:      spendrules.EventTypeBreach,
		UserID:         conv.ToPGTextEmpty("user_ada"),
		Email:          "ada@acme.com",
		DisplayName:    conv.ToPGTextEmpty("Ada"),
		SpendUsd:       520,
		LimitUsd:       500,
		WindowStart:    conv.ToPGTimestamptz(windowStart),
		WindowEnd:      conv.ToPGTimestamptz(windowEnd),
	}

	inserted, err := spendrepo.New(ti.conn).InsertSpendRuleEvent(ctx, params)
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)

	// Same (rule version, actor, window, type) dedupes to a no-op.
	inserted, err = spendrepo.New(ti.conn).InsertSpendRuleEvent(ctx, params)
	require.NoError(t, err)
	require.Equal(t, int64(0), inserted)

	// A new rule version is a fresh evaluation: the same actor/window records
	// a new event under the bumped URN.
	params.RuleUrn = urn.NewSpendRule(created.Slug, 2).String()
	inserted, err = spendrepo.New(ti.conn).InsertSpendRuleEvent(ctx, params)
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)

	result, err := ti.service.ListSpendRuleEvents(ctx, &gen.ListSpendRuleEventsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Events, 2)
	require.Equal(t, "Engineering cap", result.Events[0].RuleName)
	require.Equal(t, "breach", result.Events[0].EventType)
	require.Equal(t, "ada@acme.com", result.Events[0].Email)

	// Event-type filtering.
	warnings, err := ti.service.ListSpendRuleEvents(ctx, &gen.ListSpendRuleEventsPayload{
		EventType: new("warning"),
	})
	require.NoError(t, err)
	require.Empty(t, warnings.Events)
}

func TestPreviewSpendRuleWithNoMatchingMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	result, err := ti.service.PreviewSpendRule(ctx, &gen.PreviewSpendRulePayload{
		Target:     createTestRulePayload().Target,
		LimitUsd:   500,
		WarnAtPct:  80,
		WindowKind: "monthly",
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.MatchedCount)
	require.Empty(t, result.Actors)
}

func TestGetSpendRulesOverviewWithNoRules(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	result, err := ti.service.GetSpendRulesOverview(ctx, &gen.GetSpendRulesOverviewPayload{})
	require.NoError(t, err)
	require.Equal(t, 0, result.RulesTotal)
	require.Empty(t, result.Rules)
	require.InDelta(t, 0.0, result.TotalSpendUsd, 0.001)
}
