package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func seedRiskOverviewResult(
	t *testing.T,
	ti *testInstance,
	projectID uuid.UUID,
	orgID string,
	policyID uuid.UUID,
	msgID uuid.UUID,
	messageTime time.Time,
	source string,
	ruleID string,
	found bool,
) {
	t.Helper()

	ctx := t.Context()
	err := testrepo.New(ti.conn).UpdateChatMessageCreatedAt(ctx, testrepo.UpdateChatMessageCreatedAtParams{
		ID:        msgID,
		CreatedAt: pgtype.Timestamptz{Time: messageTime, Valid: true},
	})
	require.NoError(t, err)

	resultID, err := uuid.NewV7()
	require.NoError(t, err)

	ruleIDValue := pgtype.Text{}
	if ruleID != "" {
		ruleIDValue = pgtype.Text{String: ruleID, Valid: true}
	}

	_, err = riskrepo.New(ti.conn).InsertRiskResults(ctx, []riskrepo.InsertRiskResultsParams{{
		ID:                resultID,
		ProjectID:         projectID,
		OrganizationID:    orgID,
		RiskPolicyID:      policyID,
		RiskPolicyVersion: 1,
		ChatMessageID:     msgID,
		Source:            source,
		Found:             found,
		RuleID:            ruleIDValue,
		Description:       pgtype.Text{},
		Match:             pgtype.Text{},
		StartPos:          pgtype.Int4{},
		EndPos:            pgtype.Int4{},
		Confidence:        pgtype.Float8{},
		Tags:              nil,
	}})
	require.NoError(t, err)

	err = testrepo.New(ti.conn).UpdateRiskResultCreatedAt(ctx, testrepo.UpdateRiskResultCreatedAtParams{
		ID:        resultID,
		CreatedAt: pgtype.Timestamptz{Time: messageTime, Valid: true},
	})
	require.NoError(t, err)
}

func TestGetRiskOverview_CustomWindowAggregates(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Overview Active")})
	require.NoError(t, err)
	disabledPolicy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Overview Disabled"), Enabled: new(false)})
	require.NoError(t, err)

	policyID, err := uuid.Parse(policy.ID)
	require.NoError(t, err)
	disabledPolicyID, err := uuid.Parse(disabledPolicy.ID)
	require.NoError(t, err)

	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)

	_, aliceSecret1 := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "alice@example.com")
	_, aliceSecret2 := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "alice@example.com")
	_, alicePII := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "alice@example.com")
	_, bobShadow := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "bob@example.com")
	_, opaqueNoFinding := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "opaque-user-id")
	_, opaqueFinding := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "opaque-user-id")
	_, outsideWindow := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "alice@example.com")
	_, disabledPolicyFinding := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "alice@example.com")

	seedRiskOverviewResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, aliceSecret1, from.Add(36*time.Hour), "gitleaks", "secret.github_pat", true)
	seedRiskOverviewResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, aliceSecret2, from.Add(38*time.Hour), "gitleaks", "secret.aws_access_token", true)
	seedRiskOverviewResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, alicePII, from.Add(60*time.Hour), "presidio", "pii.email_address", true)
	seedRiskOverviewResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, bobShadow, from.Add(84*time.Hour), "shadow_mcp", "", true)
	seedRiskOverviewResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, opaqueNoFinding, from.Add(108*time.Hour), "gitleaks", "", false)
	seedRiskOverviewResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, opaqueFinding, from.Add(109*time.Hour), "gitleaks", "secret.github_pat", true)
	seedRiskOverviewResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, outsideWindow, to.Add(24*time.Hour), "gitleaks", "secret.github_pat", true)
	seedRiskOverviewResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, disabledPolicyID, disabledPolicyFinding, from.Add(110*time.Hour), "gitleaks", "secret.github_pat", true)

	result, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{
		From: new(from.Format(time.RFC3339)),
		To:   new(to.Format(time.RFC3339)),
	})
	require.NoError(t, err)

	require.Equal(t, int64(7), result.MessagesScanned)
	require.Equal(t, int64(6), result.Findings)
	require.Equal(t, int64(6), result.FlaggedSessions)
	require.Equal(t, int64(1), result.ActivePolicies)

	categories := map[string]int64{}
	for _, category := range result.TopCategories {
		categories[category.Category] = category.Findings
	}
	require.Equal(t, int64(4), categories["secrets"])
	require.Equal(t, int64(1), categories["pii"])
	require.Equal(t, int64(1), categories["shadow_mcp"])

	users := map[string]int64{}
	for _, user := range result.TopUsers {
		users[user.Email] = user.Findings
	}
	require.Equal(t, int64(4), users["alice@example.com"])
	require.Equal(t, int64(1), users["bob@example.com"])
	require.Equal(t, int64(1), users["Unknown user"])
	require.NotContains(t, users, "opaque-user-id")

	require.Len(t, result.TimeSeriesFindings, 504)
	timeSeries := map[string]int64{}
	for _, point := range result.TimeSeriesFindings {
		timeSeries[point.Category+"|"+point.BucketStart] = point.Findings
	}
	require.Equal(t, int64(1), timeSeries["secrets|2026-05-02T12:00:00Z"])
	require.Equal(t, int64(1), timeSeries["secrets|2026-05-02T14:00:00Z"])
	require.Equal(t, int64(1), timeSeries["pii|2026-05-03T12:00:00Z"])
	require.Equal(t, int64(1), timeSeries["shadow_mcp|2026-05-04T12:00:00Z"])
	require.Equal(t, int64(1), timeSeries["secrets|2026-05-05T13:00:00Z"])
	require.Equal(t, int64(1), timeSeries["secrets|2026-05-05T14:00:00Z"])
	require.Equal(t, int64(0), timeSeries["pii|2026-05-05T13:00:00Z"])
}

func TestGetRiskOverview_DefaultWindow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Overview Default Window")})
	require.NoError(t, err)

	policyID, err := uuid.Parse(policy.ID)
	require.NoError(t, err)

	_, recent := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "recent@example.com")
	_, old := seedChatWithUser(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "old@example.com")

	seedRiskOverviewResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, recent, time.Now().UTC().Add(-time.Hour), "gitleaks", "secret.github_pat", true)
	seedRiskOverviewResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, old, time.Now().UTC().AddDate(0, 0, -10), "gitleaks", "secret.github_pat", true)

	result, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.MessagesScanned)
	require.Equal(t, int64(1), result.Findings)
	require.NotEmpty(t, result.TimeSeriesFindings)
	require.Equal(t, "secrets", result.TimeSeriesFindings[0].Category)
}

func TestGetRiskOverview_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestGetRiskOverview_RejectsLargeWindow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 32)
	_, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{
		From: new(from.Format(time.RFC3339)),
		To:   new(to.Format(time.RFC3339)),
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}
